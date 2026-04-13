package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/model"
)

type DescribeOptions struct {
	Refresh bool
}

func (m *Manager) DescribeService(ctx context.Context, spec Spec, service string, opts DescribeOptions) (model.ServiceSchema, error) {
	if strings.TrimSpace(service) == "" {
		return model.ServiceSchema{}, errors.New("service is required")
	}
	key, err := classpathContentKey(spec.StubPaths)
	if err != nil {
		return model.ServiceSchema{}, err
	}
	cachePath := m.schemaCachePath(key, service)
	if !opts.Refresh {
		if schema, ok, err := readSchemaCache(cachePath); err != nil {
			return model.ServiceSchema{}, err
		} else if ok {
			return schema, nil
		}
	}
	schema, err := m.describeViaWorker(ctx, spec, service)
	if err != nil {
		return model.ServiceSchema{}, err
	}
	if writeErr := writeJSONFile(cachePath, schema); writeErr != nil {
		return schema, fmt.Errorf("write schema cache %s: %w", cachePath, writeErr)
	}
	return schema, nil
}

func (m *Manager) schemaCachePath(classpathKey, service string) string {
	return filepath.Join(m.SchemaDir(), classpathKey, service+".json")
}

func (m *Manager) describeViaWorker(ctx context.Context, spec Spec, service string) (model.ServiceSchema, error) {
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

func readSchemaCache(path string) (model.ServiceSchema, bool, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return model.ServiceSchema{}, false, nil
		}
		return model.ServiceSchema{}, false, err
	}
	var schema model.ServiceSchema
	if err := json.Unmarshal(body, &schema); err != nil {
		return model.ServiceSchema{}, false, fmt.Errorf("read schema cache %s: %w", path, err)
	}
	return schema, true, nil
}

func classpathContentKey(stubs []string) (string, error) {
	if len(stubs) == 0 {
		return hashStrings(nil), nil
	}
	sorted := append([]string{}, stubs...)
	sort.Strings(sorted)
	parts := make([]string, 0, len(sorted)*2)
	for _, path := range sorted {
		digest, err := fileDigest(path)
		if err != nil {
			return "", fmt.Errorf("digest stub %s: %w", path, err)
		}
		parts = append(parts, path, digest)
	}
	return hashStrings(parts), nil
}

func buildClasspath(runtimeJar string, stubs []string) string {
	entries := make([]string, 0, 1+len(stubs))
	if runtimeJar != "" {
		entries = append(entries, runtimeJar)
	}
	entries = append(entries, stubs...)
	return strings.Join(entries, string(os.PathListSeparator))
}
