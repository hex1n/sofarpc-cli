package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/rpctest"
)

func TestSplitRPCTestProjectArg(t *testing.T) {
	project, rest, err := splitRPCTestProjectArg([]string{
		"--project", "C:/work/demo",
		"--filter", "Deal",
		"--save",
	})
	if err != nil {
		t.Fatalf("splitRPCTestProjectArg() error = %v", err)
	}
	if project != "C:/work/demo" {
		t.Fatalf("expected project override, got %q", project)
	}
	if got := strings.Join(rest, " "); got != "--filter Deal --save" {
		t.Fatalf("unexpected passthrough args: %q", got)
	}
}

func TestSplitRPCTestProjectArgRejectsMissingValue(t *testing.T) {
	if _, _, err := splitRPCTestProjectArg([]string{"--project"}); err == nil {
		t.Fatal("expected missing project value to be rejected")
	}
}

func TestInspectRPCTestStatePrefersExistingConfigLayout(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".sofarpc")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "config.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	state := rpctest.InspectState(root)
	if state.Layout.Label() != "primary (.sofarpc)" {
		t.Fatalf("expected primary layout, got %q", state.Layout.Label())
	}
	if !strings.Contains(state.ConfigPath, filepath.Join(".sofarpc", "config.json")) {
		t.Fatalf("unexpected config path %q", state.ConfigPath)
	}
}

func TestResolveRPCTestProjectRootWalksUpFromNestedDir(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pom.xml"), []byte("<project><modelVersion>4.0.0</modelVersion></project>"), 0o644); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	nested := filepath.Join(root, "svc", "impl")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll(nested) error = %v", err)
	}
	app := &App{Cwd: nested}
	got, err := app.resolveRPCTestProjectRoot("")
	if err != nil {
		t.Fatalf("resolveRPCTestProjectRoot() error = %v", err)
	}
	if got != root {
		t.Fatalf("expected walk-up root %q, got %q", root, got)
	}
}

func TestRunRPCTestWherePrintsResolvedProjectState(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".sofarpc")
	if err := os.MkdirAll(filepath.Join(stateDir, "index"), 0o755); err != nil {
		t.Fatalf("MkdirAll(index) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "cases"), 0o755); err != nil {
		t.Fatalf("MkdirAll(cases) error = %v", err)
	}
	body, err := json.Marshal(map[string]any{
		"sofarpcBin":     "C:/Users/demo/bin/sofarpc.exe",
		"defaultContext": "test-direct",
		"manifestPath":   "sofarpc.manifest.json",
		"facadeModules":  []map[string]string{{"name": "demo", "sourceRoot": "svc/src/main/java"}},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "config.json"), append(body, '\n'), 0o644); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	moduleRoot, err := filepath.Abs(filepath.Join(cwd, "..", ".."))
	if err != nil {
		t.Fatalf("Abs(module root) error = %v", err)
	}
	t.Setenv("SOFARPC_HOME", moduleRoot)

	var stdout bytes.Buffer
	app := &App{
		Stdout: &stdout,
		Stderr: io.Discard,
		Cwd:    root,
	}
	if err := app.runRPCTestWhere(nil); err != nil {
		t.Fatalf("runRPCTestWhere() error = %v", err)
	}

	out := stdout.String()
	for _, want := range []string{
		"state layout:   primary (.sofarpc)",
		"config path:    " + filepath.Join(root, ".sofarpc", "config.json"),
		"index dir:      " + filepath.Join(root, ".sofarpc", "index"),
		"cases dir:      " + filepath.Join(root, ".sofarpc", "cases"),
		"sofarpcBin:     C:/Users/demo/bin/sofarpc.exe",
		"defaultContext: test-direct",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}
