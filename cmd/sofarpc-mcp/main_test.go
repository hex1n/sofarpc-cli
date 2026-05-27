package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvConfig_IgnoresProjectScopedTargetEnv(t *testing.T) {
	clearTargetEnv(t)
	t.Setenv("SOFARPC_DIRECT_URL", "bolt://host:12200")
	t.Setenv("SOFARPC_REGISTRY_ADDRESS", "zookeeper://host:2181")
	t.Setenv("SOFARPC_PROTOCOL", "bolt")
	t.Setenv("SOFARPC_TIMEOUT_MS", "2500")

	cfg := envConfig()
	if cfg.Mode != "" || cfg.DirectURL != "" || cfg.RegistryAddress != "" || cfg.Protocol != "" || cfg.TimeoutMS != 0 {
		t.Fatalf("envConfig should ignore project-scoped target env, got %+v", cfg)
	}
}

func TestProjectRootFromEnv_IgnoresLegacyExplicitEnv(t *testing.T) {
	t.Setenv("SOFARPC_PROJECT_ROOT", "/custom/root")
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if got := projectRootFromEnv(); got != wd {
		t.Fatalf("got %q want %q", got, wd)
	}
}

func TestLoadContractStore_LoadsJavaSources(t *testing.T) {
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

	store, err := loadContractStore(root)
	if err != nil {
		t.Fatalf("loadContractStore: %v", err)
	}
	if store == nil {
		t.Fatal("loadContractStore returned nil")
	}
	if store.Size() != 1 {
		t.Fatalf("store.Size: got %d want 1", store.Size())
	}
	if _, ok := store.Class("com.foo.Svc"); !ok {
		t.Fatal("Svc not found")
	}
}

func TestLoadContractStore_EmptyWorkspaceReturnsNil(t *testing.T) {
	got, err := loadContractStore(t.TempDir())
	if err != nil {
		t.Fatalf("loadContractStore(empty): %v", err)
	}
	if got != nil {
		t.Fatalf("loadContractStore(empty) = %#v, want nil", got)
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
