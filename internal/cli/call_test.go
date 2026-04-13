package cli

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

func newCallTestApp(t *testing.T) *App {
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
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Cwd:     cwd,
		Paths:   paths,
		Runtime: runtime.NewManager(paths, cwd),
	}
}

func TestRunCallRejectsMalformedPositionalServiceMethod(t *testing.T) {
	app := newCallTestApp(t)
	err := app.runCall([]string{"not-a-service", "[]"})
	if err == nil {
		t.Fatal("expected error for positional without service/method slash")
	}
	if !strings.Contains(err.Error(), "service/method") {
		t.Fatalf("expected parseServiceMethod error, got %v", err)
	}
}

func TestRunCallRejectsInvalidArgsJSON(t *testing.T) {
	app := newCallTestApp(t)
	err := app.runCall([]string{
		"--direct-url", "bolt://127.0.0.1:12200",
		"--service", "com.example.Svc",
		"--method", "ping",
		"--args", "not-json",
	})
	if err == nil || !strings.Contains(err.Error(), "--args must be valid JSON") {
		t.Fatalf("expected invalid args JSON error, got %v", err)
	}
}

func TestRunCallRequiresResolvableTarget(t *testing.T) {
	app := newCallTestApp(t)
	err := app.runCall([]string{
		"--service", "com.example.Svc",
		"--method", "ping",
	})
	if err == nil || !strings.Contains(err.Error(), "direct target or registry target") {
		t.Fatalf("expected missing target error, got %v", err)
	}
}

func TestRunCallUsesPositionalArgsJSON(t *testing.T) {
	app := newCallTestApp(t)
	err := app.runCall([]string{
		"--direct-url", "bolt://127.0.0.1:12200",
		"com.example.Svc/ping",
		"still-not-json",
	})
	if err == nil || !strings.Contains(err.Error(), "--args must be valid JSON") {
		t.Fatalf("expected positional args JSON to flow through validation, got %v", err)
	}
}
