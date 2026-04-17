package cli

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

func newDoctorTestApp(t *testing.T) *App {
	t.Helper()
	cwd := t.TempDir()
	configDir := t.TempDir()
	paths := config.Paths{
		ConfigDir:          configDir,
		CacheDir:           t.TempDir(),
		ContextsFile:       filepath.Join(configDir, "contexts.json"),
		RuntimeSourcesFile: filepath.Join(configDir, "runtime-sources.json"),
	}
	return &App{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Cwd:     cwd,
		Paths:   paths,
		Runtime: runtime.NewManager(paths, cwd),
	}
}

func TestPrepareDoctorInvocationUsesRuntimeOnlyForDefaultProbe(t *testing.T) {
	app := newDoctorTestApp(t)
	resolved := resolvedInvocation{
		Request: model.InvocationRequest{
			Service: doctorProbeService,
			Method:  doctorProbeMethod,
		},
		StubPaths: []string{"a.jar", "b.jar"},
	}

	source, cacheHit, notes, err := app.prepareDoctorInvocation(context.Background(), &resolved, invocationInputs{
		Service: doctorProbeService,
		Method:  doctorProbeMethod,
	})
	if err != nil {
		t.Fatalf("prepareDoctorInvocation() error = %v", err)
	}
	if source != "" || cacheHit || len(notes) != 0 {
		t.Fatalf("got source=%q cacheHit=%t notes=%v, want empty/false/nil", source, cacheHit, notes)
	}
	if len(resolved.StubPaths) != 0 {
		t.Fatalf("StubPaths = %v, want runtime-only", resolved.StubPaths)
	}
}

func TestSummarizeInvokeProbeTransportError(t *testing.T) {
	probe := summarizeInvokeProbe(model.InvocationResponse{}, errors.New("dial tcp timeout"))
	if !probe.Attempted || probe.Reachable {
		t.Fatalf("expected failed transport probe, got %+v", probe)
	}
	if probe.TransportError == "" {
		t.Fatalf("expected transport error to be populated, got %+v", probe)
	}
}

func TestSummarizeInvokeProbeTreatsProviderNotFoundAsReachable(t *testing.T) {
	probe := summarizeInvokeProbe(model.InvocationResponse{
		OK: false,
		Error: &model.RuntimeError{
			Code: "PROVIDER_NOT_FOUND",
		},
	}, nil)
	if !probe.Reachable {
		t.Fatalf("expected provider-not-found probe to confirm rpc path, got %+v", probe)
	}
}

func TestSummarizeInvokeProbeTreatsProviderUnreachableAsNotReachable(t *testing.T) {
	probe := summarizeInvokeProbe(model.InvocationResponse{
		OK: false,
		Error: &model.RuntimeError{
			Code: "PROVIDER_UNREACHABLE",
		},
	}, nil)
	if probe.Reachable {
		t.Fatalf("expected provider-unreachable probe to fail reachability, got %+v", probe)
	}
}
