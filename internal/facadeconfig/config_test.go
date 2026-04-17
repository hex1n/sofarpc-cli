package facadeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigMergesDefaults(t *testing.T) {
	root := mkProject(t, t.TempDir(), true)

	cfg, err := LoadConfig(root, false)
	if err != nil {
		t.Fatalf("LoadConfig error = %v", err)
	}
	if cfg.DefaultContext != "test-direct" {
		t.Fatalf("DefaultContext = %q", cfg.DefaultContext)
	}
	if strings.Join(cfg.RequiredMarkers, ",") != strings.Join(DefaultConfig().RequiredMarkers, ",") {
		t.Fatalf("RequiredMarkers = %v, want %v", cfg.RequiredMarkers, DefaultConfig().RequiredMarkers)
	}
	if len(cfg.FacadeModules) != 1 || cfg.FacadeModules[0].Name != "x-facade" {
		t.Fatalf("FacadeModules = %+v", cfg.FacadeModules)
	}
}

func TestLoadConfigStripsCommentKeys(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	stateDir := StateDir(root)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	body, err := json.Marshal(map[string]any{
		"_comment": "ignore me",
		"$schema":  "ignore me too",
		"facadeModules": []map[string]any{
			{"name": "y", "sourceRoot": "mod/java", "_note": "dropped"},
		},
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "config.json"), body, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(root, false)
	if err != nil {
		t.Fatalf("LoadConfig error = %v", err)
	}
	if len(cfg.FacadeModules) != 1 {
		t.Fatalf("FacadeModules len = %d", len(cfg.FacadeModules))
	}
	if cfg.FacadeModules[0].SourceRoot != "mod/java" {
		t.Fatalf("SourceRoot = %q", cfg.FacadeModules[0].SourceRoot)
	}
}

func TestLoadConfigOptionalReturnsDefaults(t *testing.T) {
	root := filepath.Join(t.TempDir(), "empty")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	cfg, err := LoadConfig(root, true)
	if err != nil {
		t.Fatalf("LoadConfig error = %v", err)
	}
	want := DefaultConfig()
	if !equalConfig(cfg, want) {
		t.Fatalf("LoadConfig = %+v, want %+v", cfg, want)
	}
}

func TestLoadConfigOrDiscoverUsesDetectedModulesWhenConfigMissing(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pom.xml"), []byte("<project><modelVersion>4.0.0</modelVersion><artifactId>demo-root</artifactId></project>"), 0o644); err != nil {
		t.Fatalf("write root pom: %v", err)
	}
	moduleDir := filepath.Join(root, "user-facade")
	if err := os.MkdirAll(filepath.Join(moduleDir, "src", "main", "java"), 0o755); err != nil {
		t.Fatalf("mkdir source root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "pom.xml"), []byte("<project><modelVersion>4.0.0</modelVersion><artifactId>user-facade</artifactId></project>"), 0o644); err != nil {
		t.Fatalf("write module pom: %v", err)
	}

	cfg, err := LoadConfigOrDiscover(root)
	if err != nil {
		t.Fatalf("LoadConfigOrDiscover error = %v", err)
	}
	if len(cfg.FacadeModules) != 1 || cfg.FacadeModules[0].Name != "user-facade" {
		t.Fatalf("FacadeModules = %+v", cfg.FacadeModules)
	}
}

func TestLoadConfigMissingErrors(t *testing.T) {
	root := filepath.Join(t.TempDir(), "empty")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	_, err := LoadConfig(root, false)
	if err == nil {
		t.Fatal("LoadConfig error = nil, want error")
	}
	if !strings.Contains(err.Error(), "no config found") {
		t.Fatalf("LoadConfig error = %v", err)
	}
}

func TestIterSourceRootsResolvesRelative(t *testing.T) {
	root := mkProject(t, t.TempDir(), true)
	cfg, err := LoadConfig(root, false)
	if err != nil {
		t.Fatalf("LoadConfig error = %v", err)
	}
	roots := IterSourceRoots(cfg, root)
	want := filepath.Join(root, "mod", "src", "main", "java")
	if len(roots) != 1 || roots[0] != want {
		t.Fatalf("IterSourceRoots = %v, want [%s]", roots, want)
	}
}

func TestResolveRepoPathAbsolutePassthrough(t *testing.T) {
	absPath := filepath.Join(t.TempDir(), "somewhere")
	got := ResolveRepoPath(absPath, t.TempDir())
	if got != absPath {
		t.Fatalf("ResolveRepoPath = %s, want %s", got, absPath)
	}
}

func TestSaveJSONCreatesParentDirs(t *testing.T) {
	target := filepath.Join(t.TempDir(), "deep", "nested", "out.json")
	if err := SaveJSON(target, map[string]string{"hello": "世界"}); err != nil {
		t.Fatalf("SaveJSON error = %v", err)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if got["hello"] != "世界" {
		t.Fatalf("SaveJSON payload = %v", got)
	}
}

func equalConfig(left, right Config) bool {
	if left.MvnCommand != right.MvnCommand ||
		left.SofaRPCBin != right.SofaRPCBin ||
		left.DefaultContext != right.DefaultContext ||
		left.ManifestPath != right.ManifestPath {
		return false
	}
	if strings.Join(left.InterfaceSuffixes, ",") != strings.Join(right.InterfaceSuffixes, ",") {
		return false
	}
	if strings.Join(left.RequiredMarkers, ",") != strings.Join(right.RequiredMarkers, ",") {
		return false
	}
	return len(left.FacadeModules) == len(right.FacadeModules)
}
