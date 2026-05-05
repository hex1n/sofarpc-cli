package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMCPProjectFile(t *testing.T, root, relative, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func assertConfigDiagnostics(t *testing.T, diagnostics map[string]any, projectRoot string) {
	t.Helper()
	if diagnostics == nil {
		t.Fatal("diagnostics should include config errors")
	}
	if got, _ := diagnostics["projectRoot"].(string); got != projectRoot {
		t.Fatalf("diagnostics projectRoot: got %q want %q", got, projectRoot)
	}
	rawErrors, ok := diagnostics["configErrors"].([]any)
	if !ok || len(rawErrors) != 1 {
		t.Fatalf("diagnostics configErrors: got %#v", diagnostics["configErrors"])
	}
	first, ok := rawErrors[0].(map[string]any)
	if !ok {
		t.Fatalf("configErrors[0] type = %T", rawErrors[0])
	}
	path, _ := first["path"].(string)
	msg, _ := first["error"].(string)
	if !strings.Contains(path, projectRoot) {
		t.Fatalf("config error path %q should contain project root %q", path, projectRoot)
	}
	if !strings.Contains(msg, "mode") {
		t.Fatalf("config error should mention mode, got %q", msg)
	}
}
