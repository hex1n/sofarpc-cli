package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/target"
)

func TestAtoiOrZero(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"0", 0},
		{"42", 42},
		{"-7", -7},
		{"not-a-number", 0},
		{"12x", 0},
	}
	for _, tc := range cases {
		if got := atoiOrZero(tc.in); got != tc.want {
			t.Errorf("atoiOrZero(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestEnvConfig_DirectURLImpliesModeDirect(t *testing.T) {
	clearTargetEnv(t)
	t.Setenv("SOFARPC_DIRECT_URL", "bolt://host:12200")
	t.Setenv("SOFARPC_TIMEOUT_MS", "2500")

	cfg := envConfig()
	if cfg.Mode != target.ModeDirect {
		t.Fatalf("mode: got %q want %q", cfg.Mode, target.ModeDirect)
	}
	if cfg.DirectURL != "bolt://host:12200" {
		t.Fatalf("directUrl: got %q", cfg.DirectURL)
	}
	if cfg.TimeoutMS != 2500 {
		t.Fatalf("timeoutMs: got %d want 2500", cfg.TimeoutMS)
	}
}

func TestEnvConfig_RegistryWithoutDirectURLImpliesModeRegistry(t *testing.T) {
	clearTargetEnv(t)
	t.Setenv("SOFARPC_REGISTRY_ADDRESS", "zookeeper://host:2181")

	cfg := envConfig()
	if cfg.Mode != target.ModeRegistry {
		t.Fatalf("mode: got %q want %q", cfg.Mode, target.ModeRegistry)
	}
}

func TestEnvConfig_EmptyEnvYieldsEmptyMode(t *testing.T) {
	clearTargetEnv(t)
	cfg := envConfig()
	if cfg.Mode != "" {
		t.Fatalf("mode should be empty without env, got %q", cfg.Mode)
	}
}

func TestEnvConfig_DirectURLWinsOverRegistry(t *testing.T) {
	clearTargetEnv(t)
	t.Setenv("SOFARPC_DIRECT_URL", "bolt://host:1")
	t.Setenv("SOFARPC_REGISTRY_ADDRESS", "zk://host:2")

	cfg := envConfig()
	if cfg.Mode != target.ModeDirect {
		t.Fatalf("mode: got %q want direct", cfg.Mode)
	}
}

func TestProjectRootFromEnv_PrefersExplicitOverCWD(t *testing.T) {
	t.Setenv("SOFARPC_PROJECT_ROOT", "/custom/root")
	if got := projectRootFromEnv(); got != "/custom/root" {
		t.Fatalf("got %q want /custom/root", got)
	}
}

func TestProjectRootFromEnv_FallsBackToCWD(t *testing.T) {
	t.Setenv("SOFARPC_PROJECT_ROOT", "")
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if got := projectRootFromEnv(); got != wd {
		t.Fatalf("got %q want %q", got, wd)
	}
}

func TestLoadFacade_LoadsJavaSources(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "src", "main", "java", "com", "foo", "Svc.java")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(`
package com.foo;

public interface Svc {
    String ping(String input);
}
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	store, err := loadFacade(root)
	if err != nil {
		t.Fatalf("loadFacade: %v", err)
	}
	if store == nil {
		t.Fatal("loadFacade returned nil")
	}
	if store.Size() != 1 {
		t.Fatalf("store.Size: got %d want 1", store.Size())
	}
	if _, ok := store.Class("com.foo.Svc"); !ok {
		t.Fatal("Svc not found")
	}
}

func TestLoadFacade_EmptyWorkspaceReturnsNil(t *testing.T) {
	got, err := loadFacade(t.TempDir())
	if err != nil {
		t.Fatalf("loadFacade(empty): %v", err)
	}
	if got != nil {
		t.Fatalf("loadFacade(empty) = %#v, want nil", got)
	}
}

func clearTargetEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"SOFARPC_DIRECT_URL",
		"SOFARPC_REGISTRY_ADDRESS",
		"SOFARPC_REGISTRY_PROTOCOL",
		"SOFARPC_PROTOCOL",
		"SOFARPC_SERIALIZATION",
		"SOFARPC_UNIQUE_ID",
		"SOFARPC_TIMEOUT_MS",
		"SOFARPC_CONNECT_TIMEOUT_MS",
	} {
		t.Setenv(key, "")
	}
}
