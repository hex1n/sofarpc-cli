package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/model"
)

type DescribeOptions struct {
	Refresh bool
	NoCache bool
}

var describeWorker = func(m *Manager, ctx context.Context, spec Spec, service string) (model.ServiceSchema, error) {
	return m.describeViaWorker(ctx, spec, service)
}

var describeViaDaemonRequest = func(ctx context.Context, m *Manager, spec Spec, service string, opts DescribeOptions) (model.ServiceSchema, error) {
	return m.describeViaDaemon(ctx, spec, service, opts)
}

func (m *Manager) DescribeService(ctx context.Context, spec Spec, service string, opts DescribeOptions) (model.ServiceSchema, error) {
	if strings.TrimSpace(service) == "" {
		return model.ServiceSchema{}, errors.New("service is required")
	}

	schema, err := describeViaDaemonRequest(ctx, m, spec, service, opts)
	if err == nil {
		return schema, nil
	}

	// Keep direct worker call as a compatibility fallback.
	return describeWorker(m, ctx, spec, service)
}

func (m *Manager) describeViaDaemon(ctx context.Context, spec Spec, service string, opts DescribeOptions) (model.ServiceSchema, error) {
	metadata, err := m.EnsureDaemon(ctx, spec)
	if err != nil {
		return model.ServiceSchema{}, err
	}
	request := model.InvocationRequest{
		RequestID:   requestID(),
		Action:      "describe",
		Service:     service,
		PayloadMode: model.PayloadRaw,
		Refresh:     opts.Refresh || opts.NoCache,
	}
	response, err := m.Invoke(ctx, metadata, request)
	if err != nil {
		return model.ServiceSchema{}, err
	}
	if !response.OK {
		if response.Error != nil {
			if strings.TrimSpace(response.Error.Message) != "" {
				return model.ServiceSchema{}, errors.New(response.Error.Message)
			}
			if response.Error.Code != "" {
				return model.ServiceSchema{}, errors.New(response.Error.Code)
			}
		}
		return model.ServiceSchema{}, errors.New("worker describe failed")
	}
	var schema model.ServiceSchema
	if err := json.Unmarshal(bytes.TrimSpace(response.Result), &schema); err != nil {
		return model.ServiceSchema{}, fmt.Errorf("parse describe response: %w", err)
	}
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

func buildClasspath(runtimeJar string, stubs []string) string {
	entries := make([]string, 0, 1+len(stubs))
	if runtimeJar != "" {
		entries = append(entries, runtimeJar)
	}
	entries = append(entries, stubs...)
	return strings.Join(entries, string(os.PathListSeparator))
}

func requestID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}
