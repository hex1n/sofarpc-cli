package projectscan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFirstArtifactIDSkipsParentBlock(t *testing.T) {
	pom := `
<project>
  <parent>
    <artifactId>demo-parent</artifactId>
  </parent>
  <artifactId>user-facade</artifactId>
</project>`
	if got := FirstArtifactID(pom); got != "user-facade" {
		t.Fatalf("FirstArtifactID() = %q", got)
	}
}

func TestDiscoverProjectUsesGitRootBeforeNestedPom(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "svc-facade")
	nested := filepath.Join(moduleDir, "src", "main", "java")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll nested: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "pom.xml"), []byte(`<project><artifactId>repo-parent</artifactId></project>`), 0o644); err != nil {
		t.Fatalf("WriteFile root pom: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "pom.xml"), []byte(`<project><artifactId>svc-facade</artifactId></project>`), 0o644); err != nil {
		t.Fatalf("WriteFile module pom: %v", err)
	}

	layout, err := DiscoverProject(filepath.Join(moduleDir, "src"))
	if err != nil {
		t.Fatalf("DiscoverProject() error = %v", err)
	}
	if layout.Root != root {
		t.Fatalf("Root = %q, want %q", layout.Root, root)
	}
	if layout.BuildTool != "maven" {
		t.Fatalf("BuildTool = %q", layout.BuildTool)
	}
}

func TestDiscoverProjectFallsBackToNearestPom(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "svc-facade")
	sourceRoot := filepath.Join(moduleDir, "src", "main", "java")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll sourceRoot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "pom.xml"), []byte(`<project><artifactId>svc-facade</artifactId></project>`), 0o644); err != nil {
		t.Fatalf("WriteFile pom: %v", err)
	}

	layout, err := DiscoverProject(sourceRoot)
	if err != nil {
		t.Fatalf("DiscoverProject() error = %v", err)
	}
	if layout.Root != moduleDir {
		t.Fatalf("Root = %q, want %q", layout.Root, moduleDir)
	}
}

func TestDetectFacadeModulesFindsFacadeArtifact(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "svc-facade")
	sourceRoot := filepath.Join(moduleDir, "src", "main", "java")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll sourceRoot: %v", err)
	}
	pom := `<project><artifactId>svc-facade</artifactId></project>`
	if err := os.WriteFile(filepath.Join(moduleDir, "pom.xml"), []byte(pom), 0o644); err != nil {
		t.Fatalf("WriteFile pom: %v", err)
	}

	modules := DetectFacadeModules(root)
	if len(modules) != 1 {
		t.Fatalf("DetectFacadeModules() len = %d", len(modules))
	}
	if modules[0].Name != "svc-facade" || modules[0].SourceRoot != "svc-facade/src/main/java" {
		t.Fatalf("module = %+v", modules[0])
	}
}

func TestDetectFacadeModulesIgnoresNonFacadeArtifact(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "svc-core")
	sourceRoot := filepath.Join(moduleDir, "src", "main", "java")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll sourceRoot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "pom.xml"), []byte(`<project><artifactId>svc-core</artifactId></project>`), 0o644); err != nil {
		t.Fatalf("WriteFile pom: %v", err)
	}

	modules := DetectFacadeModules(root)
	if len(modules) != 0 {
		t.Fatalf("DetectFacadeModules() len = %d, want 0", len(modules))
	}
}

func TestDetectFacadeModulesRequiresSourceRoot(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "svc-facade")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("MkdirAll moduleDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "pom.xml"), []byte(`<project><artifactId>svc-facade</artifactId></project>`), 0o644); err != nil {
		t.Fatalf("WriteFile pom: %v", err)
	}

	modules := DetectFacadeModules(root)
	if len(modules) != 0 {
		t.Fatalf("DetectFacadeModules() len = %d, want 0", len(modules))
	}
}

func TestDetectFacadeModulesSkipsTargetAndHiddenDirs(t *testing.T) {
	root := t.TempDir()
	targetModuleDir := filepath.Join(root, "target", "ignored-facade")
	hiddenModuleDir := filepath.Join(root, ".idea", "hidden-facade")
	for _, moduleDir := range []string{targetModuleDir, hiddenModuleDir} {
		sourceRoot := filepath.Join(moduleDir, "src", "main", "java")
		if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", moduleDir, err)
		}
		if err := os.WriteFile(filepath.Join(moduleDir, "pom.xml"), []byte(`<project><artifactId>ignored-facade</artifactId></project>`), 0o644); err != nil {
			t.Fatalf("WriteFile pom in %s: %v", moduleDir, err)
		}
	}

	modules := DetectFacadeModules(root)
	if len(modules) != 0 {
		t.Fatalf("DetectFacadeModules() len = %d, want 0", len(modules))
	}
}
