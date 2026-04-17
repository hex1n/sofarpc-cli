package projectscan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverArtifactsFindsTargetJarAndFacadeDeps(t *testing.T) {
	root := t.TempDir()
	module := FacadeModule{
		Name:            "svc-facade",
		MavenModulePath: "svc-facade",
		JarGlob:         "svc-facade/target/svc-facade-*.jar",
		DepsDir:         "svc-facade/target/facade-deps",
	}
	moduleRoot := filepath.Join(root, "svc-facade")
	targetDir := filepath.Join(moduleRoot, "target")
	depsDir := filepath.Join(targetDir, "facade-deps")
	if err := os.MkdirAll(depsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll depsDir: %v", err)
	}
	jarPath := filepath.Join(targetDir, "svc-facade-1.0.0.jar")
	depPath := filepath.Join(depsDir, "dep-1.0.0.jar")
	if err := os.WriteFile(jarPath, []byte("jar"), 0o644); err != nil {
		t.Fatalf("WriteFile jarPath: %v", err)
	}
	if err := os.WriteFile(depPath, []byte("jar"), 0o644); err != nil {
		t.Fatalf("WriteFile depPath: %v", err)
	}

	artifacts, err := DiscoverArtifacts(root, module)
	if err != nil {
		t.Fatalf("DiscoverArtifacts() error = %v", err)
	}
	if len(artifacts.PrimaryJars) != 1 || artifacts.PrimaryJars[0] != jarPath {
		t.Fatalf("PrimaryJars = %v", artifacts.PrimaryJars)
	}
	if len(artifacts.DependencyJars) != 1 || artifacts.DependencyJars[0] != depPath {
		t.Fatalf("DependencyJars = %v", artifacts.DependencyJars)
	}
}

func TestDiscoverArtifactsFallsBackToTargetDependencyAndGradleLibs(t *testing.T) {
	root := t.TempDir()
	module := FacadeModule{
		Name:            "svc-facade",
		MavenModulePath: "svc-facade",
	}
	moduleRoot := filepath.Join(root, "svc-facade")
	gradleDir := filepath.Join(moduleRoot, "build", "libs")
	dependencyDir := filepath.Join(moduleRoot, "target", "dependency")
	if err := os.MkdirAll(gradleDir, 0o755); err != nil {
		t.Fatalf("MkdirAll gradleDir: %v", err)
	}
	if err := os.MkdirAll(dependencyDir, 0o755); err != nil {
		t.Fatalf("MkdirAll dependencyDir: %v", err)
	}
	primaryJar := filepath.Join(gradleDir, "svc-facade.jar")
	dependencyJar := filepath.Join(dependencyDir, "dep.jar")
	if err := os.WriteFile(primaryJar, []byte("jar"), 0o644); err != nil {
		t.Fatalf("WriteFile primaryJar: %v", err)
	}
	if err := os.WriteFile(dependencyJar, []byte("jar"), 0o644); err != nil {
		t.Fatalf("WriteFile dependencyJar: %v", err)
	}

	artifacts, err := DiscoverArtifacts(root, module)
	if err != nil {
		t.Fatalf("DiscoverArtifacts() error = %v", err)
	}
	if len(artifacts.PrimaryJars) != 1 || artifacts.PrimaryJars[0] != primaryJar {
		t.Fatalf("PrimaryJars = %v", artifacts.PrimaryJars)
	}
	if len(artifacts.DependencyJars) != 1 || artifacts.DependencyJars[0] != dependencyJar {
		t.Fatalf("DependencyJars = %v", artifacts.DependencyJars)
	}
}

func TestSortAndDedupe(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "b.jar")
	second := filepath.Join(root, "a.jar")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("jar"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", path, err)
		}
	}

	got := sortAndDedupe([]string{first, second, first, ""})
	want := []string{second, first}
	if len(got) != len(want) {
		t.Fatalf("sortAndDedupe() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("sortAndDedupe()[%d] = %q, want %q", idx, got[idx], want[idx])
		}
	}
}
