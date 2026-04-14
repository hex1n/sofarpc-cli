package rpctest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteIndexFilesCreatesServiceAndSummaryFiles(t *testing.T) {
	root := t.TempDir()
	indexDir := filepath.Join(root, ".sofarpc", "index")
	cfg := DefaultConfig()
	cfg.InterfaceSuffixes = []string{"Facade"}
	cfg.RequiredMarkers = []string{"必传", "required"}
	cfg.FacadeModules = []FacadeModule{
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
	if !strings.Contains(out, "com.example.UserFacade") || !strings.Contains(out, "[build_index] wrote 1 services") {
		t.Fatalf("stdout = %s", out)
	}
}
