package facadeindex

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/facadeconfig"
	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
)

func TestWriteIndexFilesCreatesServiceAndSummaryFiles(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, ".sofarpc", "index")
	cfg := facadeconfig.DefaultConfig()
	cfg.InterfaceSuffixes = []string{"Facade"}
	cfg.RequiredMarkers = []string{"必传", "required"}
	cfg.FacadeModules = []facadeconfig.FacadeModule{
		{Name: "fixture-facade", SourceRoot: "src/main/java"},
	}
	if err := os.MkdirAll(filepath.Join(root, "src", "main", "java"), 0o755); err != nil {
		t.Fatalf("MkdirAll source root: %v", err)
	}

	var stdout bytes.Buffer
	if err := WriteIndexFiles(indexDir, root, cfg, fixtureRegistry(), &stdout); err != nil {
		t.Fatalf("WriteIndexFiles error = %v", err)
	}

	serviceFile := filepath.Join(indexDir, "com.example.UserFacade.json")
	body, err := os.ReadFile(serviceFile)
	if err != nil {
		t.Fatalf("ReadFile(service) error = %v", err)
	}
	var servicePayload ServiceIndexFile
	if err := json.Unmarshal(body, &servicePayload); err != nil {
		t.Fatalf("Unmarshal(service) error = %v", err)
	}
	if servicePayload.Service != "com.example.UserFacade" {
		t.Fatalf("Service = %q", servicePayload.Service)
	}
	if len(servicePayload.Methods) != 1 {
		t.Fatalf("Methods len = %d", len(servicePayload.Methods))
	}
	if servicePayload.Methods[0].Name != "getUser" {
		t.Fatalf("Method name = %q", servicePayload.Methods[0].Name)
	}

	summaryFile := filepath.Join(indexDir, "_index.json")
	summaryBody, err := os.ReadFile(summaryFile)
	if err != nil {
		t.Fatalf("ReadFile(summary) error = %v", err)
	}
	var summary IndexSummary
	if err := json.Unmarshal(summaryBody, &summary); err != nil {
		t.Fatalf("Unmarshal(summary) error = %v", err)
	}
	if len(summary.Services) != 1 || summary.Services[0].Service != "com.example.UserFacade" {
		t.Fatalf("Summary services = %+v", summary.Services)
	}
	if len(summary.SourceRoots) != 1 || summary.SourceRoots[0] != "src/main/java" {
		t.Fatalf("SourceRoots = %v", summary.SourceRoots)
	}
	out := stdout.String()
	if !strings.Contains(out, "com.example.UserFacade") || !strings.Contains(out, "[index] wrote 1 services") {
		t.Fatalf("stdout = %s", out)
	}
}

func TestSwitchIndexDirReplacesIndexDirectory(t *testing.T) {
	root := t.TempDir()
	currentDir := filepath.Join(root, "index")
	nextDir := filepath.Join(root, "next")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(current): %v", err)
	}
	if err := os.WriteFile(filepath.Join(currentDir, "stale.json"), []byte(`{"stale":true}`), 0o644); err != nil {
		t.Fatalf("write stale index: %v", err)
	}
	if err := os.MkdirAll(nextDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(next): %v", err)
	}
	if err := os.WriteFile(filepath.Join(nextDir, "new.json"), []byte(`{"new":true}`), 0o644); err != nil {
		t.Fatalf("write new index: %v", err)
	}

	if err := switchIndexDir(nextDir, currentDir); err != nil {
		t.Fatalf("switchIndexDir() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(currentDir, "new.json")); err != nil {
		t.Fatalf("current does not have new.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(currentDir, "stale.json")); err == nil {
		t.Fatalf("expected stale.json to be replaced")
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected stat error for stale.json: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(currentDir))
	if err != nil {
		t.Fatalf("ReadDir(parent): %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".sofarpc-index-old-") {
			t.Fatalf("found stale backup directory %q after switch", entry.Name())
		}
	}
}

func TestLoadServiceSummaryUsesCachedIndexWhenCompatible(t *testing.T) {
	root := t.TempDir()
	cfg := facadeconfig.DefaultConfig()
	cfg.InterfaceSuffixes = []string{"Facade"}
	cfg.FacadeModules = []facadeconfig.FacadeModule{
		{Name: "fixture-facade", SourceRoot: "src/main/java"},
	}
	if err := facadeconfig.SaveJSON(facadeconfig.ConfigPath(root), cfg); err != nil {
		t.Fatalf("SaveJSON(config) error = %v", err)
	}
	cached := IndexSummary{
		SourceRoots:       []string{"src/main/java"},
		InterfaceSuffixes: []string{"Facade"},
		Services: []IndexSummaryService{
			{Service: "com.example.UserFacade", Methods: []string{"getUser"}},
		},
	}
	if err := facadeconfig.SaveJSON(filepath.Join(facadeconfig.EffectiveIndexDir(root), "_index.json"), cached); err != nil {
		t.Fatalf("SaveJSON(index) error = %v", err)
	}

	summary, err := LoadServiceSummary(root)
	if err != nil {
		t.Fatalf("LoadServiceSummary error = %v", err)
	}
	if len(summary.Services) != 1 || summary.Services[0].Service != "com.example.UserFacade" {
		t.Fatalf("summary.Services = %+v", summary.Services)
	}
}

func TestIndexSummaryCompatibleRejectsConfigDrift(t *testing.T) {
	root := t.TempDir()
	cfg := facadeconfig.DefaultConfig()
	cfg.InterfaceSuffixes = []string{"Facade"}
	cfg.FacadeModules = []facadeconfig.FacadeModule{
		{Name: "fixture-facade", SourceRoot: "src/main/java"},
	}
	summary := IndexSummary{
		SourceRoots:       []string{"src/main/java"},
		InterfaceSuffixes: []string{"Api"},
		Services:          []IndexSummaryService{{Service: "com.example.UserFacade"}},
	}
	if indexSummaryCompatible(summary, cfg, root) {
		t.Fatal("expected summary compatibility check to fail when interface suffixes drift")
	}
}

func fixtureRegistry() facadesemantic.Registry {
	return facadesemantic.Registry{
		"com.example.UserFacade": {
			FQN:        "com.example.UserFacade",
			SimpleName: "UserFacade",
			File:       "src/main/java/com/example/UserFacade.java",
			Kind:       "interface",
			Methods: []facadesemantic.SemanticMethodInfo{
				{
					Name:       "getUser",
					Javadoc:    "@param request\n\t\t必传 用户请求",
					ReturnType: "com.example.ResponseEnvelope",
					Parameters: []facadesemantic.SemanticParameterInfo{{Name: "request", Type: "com.example.UserRequest"}},
				},
			},
		},
		"com.example.BaseRequest": {
			FQN:        "com.example.BaseRequest",
			SimpleName: "BaseRequest",
			Kind:       "class",
			Fields: []facadesemantic.SemanticFieldInfo{
				{Name: "tenantId", JavaType: "java.lang.String", Comment: "tenant id 必传", Required: true},
			},
		},
		"com.example.UserRequest": {
			FQN:        "com.example.UserRequest",
			SimpleName: "UserRequest",
			Kind:       "class",
			Superclass: "com.example.BaseRequest",
			Fields: []facadesemantic.SemanticFieldInfo{
				{Name: "items", JavaType: "java.util.List<com.example.Item>"},
				{Name: "status", JavaType: "com.example.Status"},
			},
		},
		"com.example.Item": {
			FQN:        "com.example.Item",
			SimpleName: "Item",
			Kind:       "class",
			Fields: []facadesemantic.SemanticFieldInfo{
				{Name: "code", JavaType: "java.lang.String"},
			},
		},
		"com.example.Status": {
			FQN:           "com.example.Status",
			SimpleName:    "Status",
			Kind:          "enum",
			EnumConstants: []string{"ACTIVE", "INACTIVE"},
		},
		"com.example.ResponseEnvelope": {
			FQN:           "com.example.ResponseEnvelope",
			SimpleName:    "ResponseEnvelope",
			Kind:          "class",
			MethodReturns: []string{"java.util.Optional<java.lang.String>"},
		},
	}
}
