package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

func newDaemonTestApp(t *testing.T, stdout io.Writer) *App {
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
		Stdout:  stdout,
		Stderr:  io.Discard,
		Cwd:     cwd,
		Paths:   paths,
		Runtime: runtime.NewManager(paths, cwd),
	}
}

func TestRunDaemonRequiresSubcommand(t *testing.T) {
	app := newDaemonTestApp(t, io.Discard)
	err := app.runDaemon(nil)
	if err == nil || !strings.Contains(err.Error(), "daemon subcommand required") {
		t.Fatalf("expected subcommand-required error, got %v", err)
	}
}

func TestRunDaemonRejectsUnknownSubcommand(t *testing.T) {
	app := newDaemonTestApp(t, io.Discard)
	err := app.runDaemon([]string{"bogus"})
	if err == nil || !strings.Contains(err.Error(), "unknown daemon subcommand") {
		t.Fatalf("expected unknown-subcommand error, got %v", err)
	}
}

func TestRunDaemonShowRequiresSingleKey(t *testing.T) {
	app := newDaemonTestApp(t, io.Discard)
	if err := app.runDaemon([]string{"show"}); err == nil {
		t.Fatal("expected arity error with no key")
	}
	if err := app.runDaemon([]string{"show", "a", "b"}); err == nil {
		t.Fatal("expected arity error with two keys")
	}
}

func TestRunDaemonShowReportsMissingDaemon(t *testing.T) {
	app := newDaemonTestApp(t, io.Discard)
	err := app.runDaemon([]string{"show", "no-such-key"})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected does-not-exist error, got %v", err)
	}
}

func TestRunDaemonListEmptyCache(t *testing.T) {
	var buf bytes.Buffer
	app := newDaemonTestApp(t, &buf)
	if err := app.runDaemon([]string{"list"}); err != nil {
		t.Fatalf("runDaemon list error = %v", err)
	}
	var payload struct {
		CacheDir string          `json:"cacheDir"`
		Daemons  json.RawMessage `json:"daemons"`
	}
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if payload.CacheDir == "" {
		t.Fatalf("expected cacheDir to be populated, got %q", payload.CacheDir)
	}
	if !bytes.Equal(bytes.TrimSpace(payload.Daemons), []byte("[]")) {
		t.Fatalf("expected empty daemons array, got %s", payload.Daemons)
	}
}

func TestRunDaemonPruneEmptyCache(t *testing.T) {
	var buf bytes.Buffer
	app := newDaemonTestApp(t, &buf)
	if err := app.runDaemon([]string{"prune"}); err != nil {
		t.Fatalf("runDaemon prune error = %v", err)
	}
	var payload struct {
		Removed json.RawMessage `json:"removed"`
	}
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(payload.Removed), []byte("[]")) {
		t.Fatalf("expected empty removed array, got %s", payload.Removed)
	}
}
