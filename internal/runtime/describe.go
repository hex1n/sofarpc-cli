package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/hex1n/sofarpc-cli/internal/model"
)

type DescribeOptions struct {
	Refresh bool
	NoCache bool
}

type schemaCacheKey struct {
	Profile string
	Service string
}

var (
	schemaCacheMu    sync.RWMutex
	schemaCacheStore = map[schemaCacheKey]model.ServiceSchema{}
)

var describeWorker = func(m *Manager, ctx context.Context, spec Spec, service string) (model.ServiceSchema, error) {
	return m.describeViaWorker(ctx, spec, service)
}

func (m *Manager) DescribeService(ctx context.Context, spec Spec, service string, opts DescribeOptions) (model.ServiceSchema, error) {
	if strings.TrimSpace(service) == "" {
		return model.ServiceSchema{}, errors.New("service is required")
	}
	classpathKey, err := classpathContentKey(spec.StubPaths)
	if err != nil {
		return model.ServiceSchema{}, err
	}
	cacheKey := schemaCacheKey{Profile: classpathKey, Service: service}
	if !opts.Refresh && !opts.NoCache {
		if schema, ok := loadSchema(cacheKey); ok {
			return schema, nil
		}
	}
	schema, err := describeWorker(m, ctx, spec, service)
	if err != nil {
		return model.ServiceSchema{}, err
	}
	if opts.NoCache {
		return schema, nil
	}
	storeSchema(cacheKey, schema)
	return schema, nil
}

func classpathContentKey(stubPaths []string) (string, error) {
	return classpathContentKeyWithPolicy(stubPaths, false)
}

func (m *Manager) describeViaWorker(ctx context.Context, spec Spec, service string) (model.ServiceSchema, error) {
	if strings.TrimSpace(service) == "" {
		return model.ServiceSchema{}, errors.New("service is required")
	}
	classpath := buildClasspath(spec.RuntimeJar, spec.StubPaths)
	cmd := exec.CommandContext(ctx, spec.JavaBin, "-cp", classpath, mainClass, "describe", "--service", service)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			return model.ServiceSchema{}, fmt.Errorf("worker describe failed: %w", err)
		}
		return model.ServiceSchema{}, fmt.Errorf("worker describe failed: %w: %s", err, detail)
	}
	var schema model.ServiceSchema
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &schema); err != nil {
		return model.ServiceSchema{}, fmt.Errorf("parse describe output: %w", err)
	}
	return schema, nil
}

func loadSchema(key schemaCacheKey) (model.ServiceSchema, bool) {
	schemaCacheMu.RLock()
	defer schemaCacheMu.RUnlock()
	schema, ok := schemaCacheStore[key]
	return schema, ok
}

func storeSchema(key schemaCacheKey, schema model.ServiceSchema) {
	schemaCacheMu.Lock()
	defer schemaCacheMu.Unlock()
	schemaCacheStore[key] = schema
}

func clearSchemaCache() {
	schemaCacheMu.Lock()
	defer schemaCacheMu.Unlock()
	for key := range schemaCacheStore {
		delete(schemaCacheStore, key)
	}
}

func buildClasspath(runtimeJar string, stubs []string) string {
	entries := make([]string, 0, 1+len(stubs))
	if runtimeJar != "" {
		entries = append(entries, runtimeJar)
	}
	entries = append(entries, stubs...)
	return strings.Join(entries, string(os.PathListSeparator))
}
