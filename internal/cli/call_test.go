package cli

import (
	"io"
	"os"
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
		Stdin:   strings.NewReader(""),
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

func TestLoadArgsInputInline(t *testing.T) {
	got, err := loadArgsInput("[1,2]", t.TempDir(), strings.NewReader(""))
	if err != nil {
		t.Fatalf("loadArgsInput error = %v", err)
	}
	if got != "[1,2]" {
		t.Fatalf("got %q, want [1,2]", got)
	}
}

func TestLoadArgsInputEmpty(t *testing.T) {
	got, err := loadArgsInput("", t.TempDir(), strings.NewReader(""))
	if err != nil {
		t.Fatalf("loadArgsInput error = %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestLoadArgsInputFile(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "payload.json")
	if err := os.WriteFile(path, []byte("  [42]\n"), 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	got, err := loadArgsInput("@payload.json", cwd, strings.NewReader(""))
	if err != nil {
		t.Fatalf("loadArgsInput error = %v", err)
	}
	if got != "[42]" {
		t.Fatalf("got %q, want [42]", got)
	}
}

func TestLoadArgsInputAbsoluteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.json")
	if err := os.WriteFile(path, []byte("[\"hello\"]"), 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	got, err := loadArgsInput("@"+path, t.TempDir(), strings.NewReader(""))
	if err != nil {
		t.Fatalf("loadArgsInput error = %v", err)
	}
	if got != "[\"hello\"]" {
		t.Fatalf("got %q, want [\"hello\"]", got)
	}
}

func TestLoadArgsInputMissingFile(t *testing.T) {
	_, err := loadArgsInput("@no-such.json", t.TempDir(), strings.NewReader(""))
	if err == nil || !strings.Contains(err.Error(), "read --args from") {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestLoadArgsInputEmptyAtPath(t *testing.T) {
	_, err := loadArgsInput("@", t.TempDir(), strings.NewReader(""))
	if err == nil || !strings.Contains(err.Error(), "requires a file path") {
		t.Fatalf("expected file-path error, got %v", err)
	}
}

func TestLoadArgsInputStdin(t *testing.T) {
	got, err := loadArgsInput("-", t.TempDir(), strings.NewReader("[7]\n"))
	if err != nil {
		t.Fatalf("loadArgsInput error = %v", err)
	}
	if got != "[7]" {
		t.Fatalf("got %q, want [7]", got)
	}
}

func TestRunCallReadsArgsFromDataAlias(t *testing.T) {
	app := newCallTestApp(t)
	err := app.runCall([]string{
		"--direct-url", "bolt://127.0.0.1:12200",
		"--service", "com.example.Svc",
		"--method", "ping",
		"--data", "not-json-via-data",
	})
	if err == nil || !strings.Contains(err.Error(), "--args must be valid JSON") {
		t.Fatalf("expected --data alias to flow through args validation, got %v", err)
	}
}

func TestRunCallReadsArgsFromShortDataAlias(t *testing.T) {
	app := newCallTestApp(t)
	err := app.runCall([]string{
		"--direct-url", "bolt://127.0.0.1:12200",
		"--service", "com.example.Svc",
		"--method", "ping",
		"-d", "not-json-via-d",
	})
	if err == nil || !strings.Contains(err.Error(), "--args must be valid JSON") {
		t.Fatalf("expected -d alias to flow through args validation, got %v", err)
	}
}

func TestRunCallReadsArgsFromAtFile(t *testing.T) {
	app := newCallTestApp(t)
	path := filepath.Join(app.Cwd, "payload.json")
	if err := os.WriteFile(path, []byte("not-json-from-file"), 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	err := app.runCall([]string{
		"--direct-url", "bolt://127.0.0.1:12200",
		"--service", "com.example.Svc",
		"--method", "ping",
		"-d", "@payload.json",
	})
	if err == nil || !strings.Contains(err.Error(), "--args must be valid JSON") {
		t.Fatalf("expected file-loaded args to flow through validation, got %v", err)
	}
}

func TestRunCallReadsArgsFromStdin(t *testing.T) {
	app := newCallTestApp(t)
	app.Stdin = strings.NewReader("not-json-from-stdin")
	err := app.runCall([]string{
		"--direct-url", "bolt://127.0.0.1:12200",
		"--service", "com.example.Svc",
		"--method", "ping",
		"-d", "-",
	})
	if err == nil || !strings.Contains(err.Error(), "--args must be valid JSON") {
		t.Fatalf("expected stdin-loaded args to flow through validation, got %v", err)
	}
}
