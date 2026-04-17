package facadeconfig

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
		t.Fatalf("FirstArtifactID = %q", got)
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

	modules := detectFacadeModules(root)
	if len(modules) != 1 {
		t.Fatalf("detectFacadeModules len = %d", len(modules))
	}
	if modules[0].Name != "svc-facade" || modules[0].SourceRoot != "svc-facade/src/main/java" {
		t.Fatalf("module = %+v", modules[0])
	}
}

func TestMergeDetectedConfigPreservesExistingValues(t *testing.T) {
	existing := Config{
		FacadeModules: []FacadeModule{{Name: "custom", SourceRoot: "custom/src/main/java"}},
		MvnCommand:    "mvn",
	}
	detected := DefaultConfig()
	detected.FacadeModules = []FacadeModule{{Name: "auto", SourceRoot: "auto/src/main/java"}}

	got := mergeDetectedConfig(existing, detected)
	if len(got.FacadeModules) != 1 || got.FacadeModules[0].Name != "custom" {
		t.Fatalf("FacadeModules = %+v", got.FacadeModules)
	}
	if got.MvnCommand != "mvn" {
		t.Fatalf("MvnCommand = %q", got.MvnCommand)
	}
	if got.SofaRPCBin != "sofarpc" {
		t.Fatalf("SofaRPCBin = %q", got.SofaRPCBin)
	}
}

func TestDetectConfigWritesPrimaryConfig(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "svc-facade")
	sourceRoot := filepath.Join(moduleDir, "src", "main", "java")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll sourceRoot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "pom.xml"), []byte(`<project><artifactId>svc-facade</artifactId></project>`), 0o644); err != nil {
		t.Fatalf("WriteFile pom: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := DetectConfig(root, true, &stdout, &stderr); err != nil {
		t.Fatalf("DetectConfig error = %v", err)
	}
	body, err := os.ReadFile(ConfigPath(root))
	if err != nil {
		t.Fatalf("ReadFile config: %v", err)
	}
	var cfg Config
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("Unmarshal config: %v", err)
	}
	if len(cfg.FacadeModules) != 1 || cfg.FacadeModules[0].Name != "svc-facade" {
		t.Fatalf("FacadeModules = %+v", cfg.FacadeModules)
	}
	if !strings.Contains(stdout.String(), "[detect] wrote") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestDetectConfigPreservesCustomKeysFromExistingConfig(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "svc-facade")
	sourceRoot := filepath.Join(moduleDir, "src", "main", "java")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll sourceRoot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "pom.xml"), []byte(`<project><artifactId>svc-facade</artifactId></project>`), 0o644); err != nil {
		t.Fatalf("WriteFile pom: %v", err)
	}
	existing := map[string]any{
		"customHint": "keep-me",
		"facadeModules": []map[string]any{
			{
				"name":            "manual-facade",
				"sourceRoot":      "manual/src/main/java",
				"mavenModulePath": "manual",
				"extraField":      "still-here",
			},
		},
	}
	body, err := json.Marshal(existing)
	if err != nil {
		t.Fatalf("Marshal existing: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(ConfigPath(root)), 0o755); err != nil {
		t.Fatalf("MkdirAll config path: %v", err)
	}
	if err := os.WriteFile(ConfigPath(root), append(body, '\n'), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := DetectConfig(root, true, &stdout, &stderr); err != nil {
		t.Fatalf("DetectConfig error = %v", err)
	}
	written, err := os.ReadFile(ConfigPath(root))
	if err != nil {
		t.Fatalf("ReadFile config: %v", err)
	}
	var final map[string]any
	if err := json.Unmarshal(written, &final); err != nil {
		t.Fatalf("Unmarshal final: %v", err)
	}
	if final["customHint"] != "keep-me" {
		t.Fatalf("customHint = %#v", final["customHint"])
	}
	modules, ok := final["facadeModules"].([]any)
	if !ok || len(modules) != 1 {
		t.Fatalf("facadeModules = %#v", final["facadeModules"])
	}
	module, ok := modules[0].(map[string]any)
	if !ok {
		t.Fatalf("facade module type = %T", modules[0])
	}
	if module["extraField"] != "still-here" {
		t.Fatalf("extraField = %#v", module["extraField"])
	}
}
