package metadata

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestContractFingerprintChangesWhenArtifactsChangeEvenWithSourcePresent(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatalf("create .git: %v", err)
	}
	moduleDir := filepath.Join(project, "order-facade")
	sourceDir := filepath.Join(moduleDir, "src", "main", "java", "com", "example")
	depsDir := filepath.Join(moduleDir, "target", "facade-deps")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	if err := os.MkdirAll(depsDir, 0o755); err != nil {
		t.Fatalf("create deps dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "pom.xml"), []byte(`<project><artifactId>order-facade</artifactId></project>`), 0o644); err != nil {
		t.Fatalf("write pom: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "OrderFacade.java"), []byte(`package com.example; public interface OrderFacade {}`), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	primaryJar := filepath.Join(moduleDir, "target", "order-facade-1.0.0.jar")
	depJar := filepath.Join(depsDir, "dep-1.0.0.jar")
	if err := os.WriteFile(primaryJar, []byte("jar-v1"), 0o644); err != nil {
		t.Fatalf("write primary jar: %v", err)
	}
	if err := os.WriteFile(depJar, []byte("dep-v1"), 0o644); err != nil {
		t.Fatalf("write dep jar: %v", err)
	}

	first, err := contractFingerprint(project, "com.example.OrderFacade")
	if err != nil {
		t.Fatalf("contractFingerprint() error = %v", err)
	}

	if err := os.WriteFile(primaryJar, []byte("jar-v2-longer"), 0o644); err != nil {
		t.Fatalf("rewrite primary jar: %v", err)
	}
	modTime := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(primaryJar, modTime, modTime); err != nil {
		t.Fatalf("chtimes primary jar: %v", err)
	}

	second, err := contractFingerprint(project, "com.example.OrderFacade")
	if err != nil {
		t.Fatalf("contractFingerprint() second error = %v", err)
	}
	if first == second {
		t.Fatalf("expected fingerprint to change when artifacts change, first=%s second=%s", first, second)
	}
}

func TestContractFingerprintChangesWhenSourceChanges(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatalf("create .git: %v", err)
	}
	moduleDir := filepath.Join(project, "order-facade")
	sourceDir := filepath.Join(moduleDir, "src", "main", "java", "com", "example")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "pom.xml"), []byte(`<project><artifactId>order-facade</artifactId></project>`), 0o644); err != nil {
		t.Fatalf("write pom: %v", err)
	}
	sourceFile := filepath.Join(sourceDir, "OrderFacade.java")
	if err := os.WriteFile(sourceFile, []byte(`package com.example; public interface OrderFacade {}`), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	first, err := contractFingerprint(project, "com.example.OrderFacade")
	if err != nil {
		t.Fatalf("contractFingerprint() error = %v", err)
	}

	if err := os.WriteFile(sourceFile, []byte(`package com.example; public interface OrderFacade { void ping(); }`), 0o644); err != nil {
		t.Fatalf("rewrite source: %v", err)
	}
	modTime := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(sourceFile, modTime, modTime); err != nil {
		t.Fatalf("chtimes source: %v", err)
	}

	second, err := contractFingerprint(project, "com.example.OrderFacade")
	if err != nil {
		t.Fatalf("contractFingerprint() second error = %v", err)
	}
	if first == second {
		t.Fatalf("expected fingerprint to change when source changes, first=%s second=%s", first, second)
	}
}
