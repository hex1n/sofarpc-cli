package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertCodexTOML_FreshFile(t *testing.T) {
	got := upsertCodexTOML("", "/bin/sofa", map[string]string{"SOFARPC_DIRECT_URL": "bolt://h:1"})
	want := "[mcp_servers.sofarpc]\n" +
		"command = \"/bin/sofa\"\n" +
		"\n" +
		"[mcp_servers.sofarpc.env]\n" +
		"SOFARPC_DIRECT_URL = \"bolt://h:1\"\n"
	if got != want {
		t.Fatalf("mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestUpsertCodexTOML_PreservesOtherServers(t *testing.T) {
	existing := "[mcp_servers.other]\n" +
		"command = \"other-bin\"\n" +
		"\n" +
		"[mcp_servers.other.env]\n" +
		"FOO = \"bar\"\n"
	got := upsertCodexTOML(existing, "/bin/sofa", nil)
	if !strings.Contains(got, "[mcp_servers.other]") {
		t.Fatalf("sibling server dropped:\n%s", got)
	}
	if !strings.Contains(got, "FOO = \"bar\"") {
		t.Fatalf("sibling env dropped:\n%s", got)
	}
	if !strings.Contains(got, "[mcp_servers.sofarpc]") {
		t.Fatalf("sofarpc block missing:\n%s", got)
	}
}

func TestUpsertCodexTOML_ReplacesExistingSofaBlocks(t *testing.T) {
	existing := "[mcp_servers.sofarpc]\n" +
		"command = \"/old/path\"\n" +
		"\n" +
		"[mcp_servers.sofarpc.env]\n" +
		"SOFARPC_DIRECT_URL = \"bolt://old:1\"\n" +
		"\n" +
		"[mcp_servers.other]\n" +
		"command = \"other-bin\"\n"
	got := upsertCodexTOML(existing, "/new/path", map[string]string{"SOFARPC_DIRECT_URL": "bolt://new:2"})
	if strings.Contains(got, "/old/path") {
		t.Fatalf("old command not replaced:\n%s", got)
	}
	if strings.Contains(got, "bolt://old") {
		t.Fatalf("old env not replaced:\n%s", got)
	}
	if !strings.Contains(got, "/new/path") {
		t.Fatalf("new command missing:\n%s", got)
	}
	if !strings.Contains(got, "bolt://new") {
		t.Fatalf("new env missing:\n%s", got)
	}
	if !strings.Contains(got, "[mcp_servers.other]") {
		t.Fatalf("sibling server dropped:\n%s", got)
	}
}

func TestUpsertCodexTOML_DoubleBracketTablesUntouched(t *testing.T) {
	// [[foo]] is an array-of-tables header; our stripper must not treat
	// it as a regular section and must not recurse into it.
	existing := "[[mcp_servers.sofarpc]]\n" +
		"command = \"keep-me\"\n"
	got := upsertCodexTOML(existing, "/new/path", nil)
	if !strings.Contains(got, "[[mcp_servers.sofarpc]]") {
		t.Fatalf("array-of-tables header removed:\n%s", got)
	}
	if !strings.Contains(got, "keep-me") {
		t.Fatalf("array-of-tables body removed:\n%s", got)
	}
}

func TestSetupClaude_UpsertsPreservingOtherKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	seed := map[string]any{
		"someUnrelatedKey": "keep-me",
		"mcpServers": map[string]any{
			"other": map[string]any{"command": "other"},
		},
	}
	body, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	env := map[string]string{"SOFARPC_DIRECT_URL": "bolt://h:1"}
	if err := setupClaudeAt(path, "/bin/sofa", env, false); err != nil {
		t.Fatalf("setupClaudeAt: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc["someUnrelatedKey"] != "keep-me" {
		t.Fatalf("unrelated top-level key dropped: %#v", doc)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatalf("sibling server dropped")
	}
	sofa, ok := servers["sofarpc"].(map[string]any)
	if !ok {
		t.Fatalf("sofarpc entry missing: %#v", servers)
	}
	if sofa["command"] != "/bin/sofa" {
		t.Fatalf("command: %#v", sofa["command"])
	}
	gotEnv, _ := sofa["env"].(map[string]any)
	if gotEnv["SOFARPC_DIRECT_URL"] != "bolt://h:1" {
		t.Fatalf("env: %#v", gotEnv)
	}
}

func TestSetupClaude_CreatesFileWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	if err := setupClaudeAt(path, "/bin/sofa", nil, false); err != nil {
		t.Fatalf("setupClaudeAt: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if _, ok := servers["sofarpc"]; !ok {
		t.Fatalf("sofarpc not added: %#v", doc)
	}
}

func TestSetupCodex_CreatesDirAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.toml")
	env := map[string]string{"SOFARPC_DIRECT_URL": "bolt://h:1"}
	if err := setupCodexAt(path, "/bin/sofa", env, false); err != nil {
		t.Fatalf("setupCodexAt: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, "[mcp_servers.sofarpc]") {
		t.Fatalf("sofarpc block missing:\n%s", got)
	}
	if !strings.Contains(got, "bolt://h:1") {
		t.Fatalf("env missing:\n%s", got)
	}
}

func TestSetupCodex_DryRunLeavesFileUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := "[mcp_servers.other]\ncommand = \"other-bin\"\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := setupCodexAt(path, "/bin/sofa", nil, true); err != nil {
		t.Fatalf("setupCodexAt: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != original {
		t.Fatalf("dry-run mutated file:\ngot:\n%s\nwant:\n%s", body, original)
	}
}

func TestBuildSetupEnv_OnlyIncludesProvidedKeys(t *testing.T) {
	got := buildSetupEnv(" /root ", "", "zk://h:1")
	if got["SOFARPC_PROJECT_ROOT"] != "/root" {
		t.Fatalf("project root: %#v", got)
	}
	if _, ok := got["SOFARPC_DIRECT_URL"]; ok {
		t.Fatalf("direct url should be absent: %#v", got)
	}
	if got["SOFARPC_REGISTRY_ADDRESS"] != "zk://h:1" {
		t.Fatalf("registry: %#v", got)
	}
}
