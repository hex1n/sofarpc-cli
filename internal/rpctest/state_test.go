package rpctest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func mkProject(t *testing.T, tmp string, withConfig bool) string {
	t.Helper()
	root := filepath.Join(tmp, "proj")
	stateDir := StateDir(root)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if withConfig {
		body, err := json.Marshal(map[string]any{
			"facadeModules": []map[string]any{
				{"name": "x-facade", "sourceRoot": "mod/src/main/java"},
			},
			"defaultContext": "test-direct",
		})
		if err != nil {
			t.Fatalf("marshal config: %v", err)
		}
		if err := os.WriteFile(filepath.Join(stateDir, "config.json"), body, 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	return root
}

func TestResolveProjectRootEnvOverride(t *testing.T) {
	tmp := t.TempDir()
	root := mkProject(t, tmp, true)
	t.Setenv(EnvProjectRoot, root)

	got, err := ResolveProjectRoot("", nil)
	if err != nil {
		t.Fatalf("ResolveProjectRoot error = %v", err)
	}
	if got != root {
		t.Fatalf("ResolveProjectRoot = %s, want %s", got, root)
	}
}

func TestResolveProjectRootWalksUp(t *testing.T) {
	tmp := t.TempDir()
	root := mkProject(t, tmp, true)
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	t.Setenv(EnvProjectRoot, "")

	got, err := ResolveProjectRoot(nested, nil)
	if err != nil {
		t.Fatalf("ResolveProjectRoot error = %v", err)
	}
	if got != root {
		t.Fatalf("ResolveProjectRoot = %s, want %s", got, root)
	}
}

func TestResolveProjectRootWarnsOnInvalidEnv(t *testing.T) {
	tmp := t.TempDir()
	root := mkProject(t, tmp, true)
	var stderr bytes.Buffer
	t.Setenv(EnvProjectRoot, filepath.Join(tmp, "missing"))

	got, err := ResolveProjectRoot(root, &stderr)
	if err != nil {
		t.Fatalf("ResolveProjectRoot error = %v", err)
	}
	if got != root {
		t.Fatalf("ResolveProjectRoot = %s, want %s", got, root)
	}
	if stderr.Len() == 0 {
		t.Fatalf("expected warning for invalid env override")
	}
}

func TestEffectivePathsPrimaryLayout(t *testing.T) {
	tmp := t.TempDir()
	root := mkProject(t, tmp, true)

	cfgPath, layout := EffectiveConfigPath(root)
	if layout != LayoutPrimary {
		t.Fatalf("layout = %s, want %s", layout, LayoutPrimary)
	}
	if cfgPath != ConfigPath(root) {
		t.Fatalf("config path = %s, want %s", cfgPath, ConfigPath(root))
	}
	if got := EffectiveIndexDir(root); got != filepath.Join(root, ".sofarpc", "index") {
		t.Fatalf("EffectiveIndexDir = %s", got)
	}
	if got := EffectiveCasesDir(root); got != filepath.Join(root, ".sofarpc", "cases") {
		t.Fatalf("EffectiveCasesDir = %s", got)
	}
}

func TestEffectivePathsPreferPrimaryWhenAllPresent(t *testing.T) {
	tmp := t.TempDir()
	root := mkProject(t, tmp, true)
	cfgPath, layout := EffectiveConfigPath(root)
	if layout != LayoutPrimary {
		t.Fatalf("layout = %s, want %s", layout, LayoutPrimary)
	}
	if cfgPath != ConfigPath(root) {
		t.Fatalf("config path = %s, want %s", cfgPath, ConfigPath(root))
	}
}

func TestEffectivePathsNoConfigReturnsPrimaryTarget(t *testing.T) {
	root := filepath.Join(t.TempDir(), "empty")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	cfgPath, layout := EffectiveConfigPath(root)
	if layout != LayoutPrimary {
		t.Fatalf("layout = %s, want %s", layout, LayoutPrimary)
	}
	if cfgPath != ConfigPath(root) {
		t.Fatalf("config path = %s, want %s", cfgPath, ConfigPath(root))
	}
}
