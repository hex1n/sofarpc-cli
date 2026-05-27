package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertCodexTOML_FreshFile(t *testing.T) {
	got := upsertCodexTOML("", "/bin/sofa", map[string]string{"SOFARPC_DIRECT_URL": "bolt://h:1"}, false)
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
	got := upsertCodexTOML(existing, "/bin/sofa", nil, false)
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
	got := upsertCodexTOML(existing, "/new/path", map[string]string{"SOFARPC_DIRECT_URL": "bolt://new:2"}, false)
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

func TestUpsertCodexTOML_MergesExistingEnvByDefault(t *testing.T) {
	existing := "[mcp_servers.sofarpc]\n" +
		"command = \"/old/path\"\n" +
		"\n" +
		"[mcp_servers.sofarpc.env]\n" +
		"SOFARPC_ALLOW_INVOKE = \"true\"\n" +
		"SOFARPC_ALLOWED_TARGET_HOSTS = \"127.0.0.1\"\n"
	got := upsertCodexTOML(existing, "/new/path", map[string]string{"SOFARPC_MAX_RESPONSE_BYTES": "1024"}, false)
	if !strings.Contains(got, "SOFARPC_ALLOW_INVOKE = \"true\"") {
		t.Fatalf("global existing env dropped:\n%s", got)
	}
	if !strings.Contains(got, "SOFARPC_ALLOWED_TARGET_HOSTS = \"127.0.0.1\"") {
		t.Fatalf("global target-host guardrail dropped:\n%s", got)
	}
	if !strings.Contains(got, "SOFARPC_MAX_RESPONSE_BYTES = \"1024\"") {
		t.Fatalf("new env missing:\n%s", got)
	}
}

func TestUpsertCodexTOML_ReplaceEnvDropsExistingEnv(t *testing.T) {
	existing := "[mcp_servers.sofarpc]\n" +
		"command = \"/old/path\"\n" +
		"\n" +
		"[mcp_servers.sofarpc.env]\n" +
		"SOFARPC_ALLOWED_SERVICES = \"com.foo.UserFacade\"\n"
	got := upsertCodexTOML(existing, "/new/path", map[string]string{"SOFARPC_DIRECT_URL": "bolt://new:2"}, true)
	if strings.Contains(got, "SOFARPC_ALLOWED_SERVICES") {
		t.Fatalf("replace-env kept old env:\n%s", got)
	}
	if !strings.Contains(got, "SOFARPC_DIRECT_URL = \"bolt://new:2\"") {
		t.Fatalf("new env missing:\n%s", got)
	}
}

func TestUpsertCodexTOML_ScrubsProjectScopedEnvByDefault(t *testing.T) {
	existing := "[mcp_servers.sofarpc]\n" +
		"command = \"/old/path\"\n" +
		"\n" +
		"[mcp_servers.sofarpc.env]\n" +
		"SOFARPC_PROJECT_ROOT = \"C:\\\\project\"\n" +
		"SOFARPC_DIRECT_URL = \"bolt://old:1\"\n" +
		"SOFARPC_ALLOWED_SERVICES = \"com.foo.UserFacade\"\n" +
		"SOFARPC_ALLOW_INVOKE = \"true\"\n"

	got := upsertCodexTOML(existing, "/new/path", nil, false)

	for _, removed := range []string{"SOFARPC_PROJECT_ROOT", "SOFARPC_DIRECT_URL", "SOFARPC_ALLOWED_SERVICES"} {
		if strings.Contains(got, removed) {
			t.Fatalf("%s should be scrubbed from user-scope env:\n%s", removed, got)
		}
	}
	if !strings.Contains(got, "SOFARPC_ALLOW_INVOKE = \"true\"") {
		t.Fatalf("global env should remain:\n%s", got)
	}
}

func TestUpsertCodexTOML_DoubleBracketTablesUntouched(t *testing.T) {
	// [[foo]] is an array-of-tables header; our stripper must not treat
	// it as a regular section and must not recurse into it.
	existing := "[[mcp_servers.sofarpc]]\n" +
		"command = \"keep-me\"\n"
	got := upsertCodexTOML(existing, "/new/path", nil, false)
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
	if err := setupClaudeAt(path, "/bin/sofa", env, false, false); err != nil {
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
	if err := setupClaudeAt(path, "/bin/sofa", nil, false, false); err != nil {
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

func TestSetupClaude_MergesExistingEnvByDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	seed := map[string]any{
		"mcpServers": map[string]any{
			"sofarpc": map[string]any{
				"command": "/old/path",
				"env": map[string]any{
					"SOFARPC_ALLOW_INVOKE":         "true",
					"SOFARPC_ALLOWED_TARGET_HOSTS": "127.0.0.1",
				},
			},
		},
	}
	body, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	if err := setupClaudeAt(path, "/bin/sofa", map[string]string{"SOFARPC_MAX_RESPONSE_BYTES": "1024"}, false, false); err != nil {
		t.Fatalf("setupClaudeAt: %v", err)
	}

	env := readClaudeEnvFromFile(t, path)
	if env["SOFARPC_ALLOW_INVOKE"] != "true" {
		t.Fatalf("global existing env dropped: %#v", env)
	}
	if env["SOFARPC_ALLOWED_TARGET_HOSTS"] != "127.0.0.1" {
		t.Fatalf("target host guardrail dropped: %#v", env)
	}
	if env["SOFARPC_MAX_RESPONSE_BYTES"] != "1024" {
		t.Fatalf("new env missing: %#v", env)
	}
}

func TestSetupClaude_ScrubsProjectScopedEnvByDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	seed := map[string]any{
		"mcpServers": map[string]any{
			"sofarpc": map[string]any{
				"command": "/old/path",
				"env": map[string]any{
					"SOFARPC_PROJECT_ROOT":     "C:\\project",
					"SOFARPC_DIRECT_URL":       "bolt://old:1",
					"SOFARPC_ALLOWED_SERVICES": "com.foo.UserFacade",
					"SOFARPC_ALLOW_INVOKE":     "true",
				},
			},
		},
	}
	body, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	if err := setupClaudeAt(path, "/bin/sofa", nil, false, false); err != nil {
		t.Fatalf("setupClaudeAt: %v", err)
	}

	env := readClaudeEnvFromFile(t, path)
	for _, removed := range []string{"SOFARPC_PROJECT_ROOT", "SOFARPC_DIRECT_URL", "SOFARPC_ALLOWED_SERVICES"} {
		if _, ok := env[removed]; ok {
			t.Fatalf("%s should be scrubbed from user-scope env: %#v", removed, env)
		}
	}
	if env["SOFARPC_ALLOW_INVOKE"] != "true" {
		t.Fatalf("global env should remain: %#v", env)
	}
}

func TestSetupClaude_ReplaceEnvDropsExistingEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	seed := map[string]any{
		"mcpServers": map[string]any{
			"sofarpc": map[string]any{
				"command": "/old/path",
				"env": map[string]any{
					"SOFARPC_ALLOWED_SERVICES": "com.foo.UserFacade",
				},
			},
		},
	}
	body, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	if err := setupClaudeAt(path, "/bin/sofa", map[string]string{"SOFARPC_DIRECT_URL": "bolt://new:2"}, true, false); err != nil {
		t.Fatalf("setupClaudeAt: %v", err)
	}

	env := readClaudeEnvFromFile(t, path)
	if _, ok := env["SOFARPC_ALLOWED_SERVICES"]; ok {
		t.Fatalf("replace-env kept old env: %#v", env)
	}
	if env["SOFARPC_DIRECT_URL"] != "bolt://new:2" {
		t.Fatalf("new env missing: %#v", env)
	}
}

func TestSetupCodex_CreatesDirAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.toml")
	env := map[string]string{"SOFARPC_DIRECT_URL": "bolt://h:1"}
	if err := setupCodexAt(path, "/bin/sofa", env, false, false); err != nil {
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
	if err := setupCodexAt(path, "/bin/sofa", nil, false, true); err != nil {
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

func TestInstallSkillAt_WritesEmbeddedContent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sofarpc-invoke")
	if err := installSkillAt(target, false); err != nil {
		t.Fatalf("installSkillAt: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// The frontmatter anchor is the most stable line to pin; the
	// embed directive failing at build time or the wrong file
	// getting baked in would both break this.
	if !strings.Contains(string(body), "name: sofarpc-invoke") {
		t.Fatalf("skill frontmatter missing:\n%s", body)
	}
	if len(body) < 1000 {
		t.Fatalf("skill body suspiciously small: %d bytes", len(body))
	}
}

func TestInstallSkillAt_Idempotent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sofarpc-invoke")
	for i := 0; i < 2; i++ {
		if err := installSkillAt(target, false); err != nil {
			t.Fatalf("installSkillAt #%d: %v", i, err)
		}
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "SKILL.md" {
		t.Fatalf("unexpected dir contents: %#v", entries)
	}
}

func TestInstallSkillAt_DryRunLeavesNoFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sofarpc-invoke")
	if err := installSkillAt(target, true); err != nil {
		t.Fatalf("installSkillAt dry-run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created file: err=%v", err)
	}
}

func TestBuildSetupEnv_OnlyIncludesProvidedKeys(t *testing.T) {
	got, err := buildSetupEnv(setupOptions{
		allowInvoke:         true,
		allowedTargetHosts:  "127.0.0.1,dev-rpc.example.com:12200",
		allowTargetOverride: true,
		argsFileRoot:        "/root/payloads",
		argsFileMaxBytes:    "1048576",
		sessionPlanMaxBytes: "2097152",
		maxResponseBytes:    "16777216",
		set: map[string]bool{
			"allow-invoke":           true,
			"allowed-target-hosts":   true,
			"allow-target-override":  true,
			"args-file-root":         true,
			"args-file-max-bytes":    true,
			"session-plan-max-bytes": true,
			"max-response-bytes":     true,
		},
	})
	if err != nil {
		t.Fatalf("buildSetupEnv: %v", err)
	}
	for _, absent := range []string{
		"SOFARPC_PROJECT_ROOT",
		"SOFARPC_DIRECT_URL",
		"SOFARPC_REGISTRY_ADDRESS",
		"SOFARPC_REGISTRY_PROTOCOL",
		"SOFARPC_PROTOCOL",
		"SOFARPC_SERIALIZATION",
		"SOFARPC_UNIQUE_ID",
		"SOFARPC_TIMEOUT_MS",
		"SOFARPC_CONNECT_TIMEOUT_MS",
		"SOFARPC_ALLOWED_SERVICES",
	} {
		if _, ok := got[absent]; ok {
			t.Fatalf("%s should be project-scoped and absent from user env: %#v", absent, got)
		}
	}
	if got["SOFARPC_ALLOW_INVOKE"] != "true" {
		t.Fatalf("allow invoke: %#v", got)
	}
	if got["SOFARPC_ALLOWED_TARGET_HOSTS"] != "127.0.0.1,dev-rpc.example.com:12200" {
		t.Fatalf("allowed target hosts: %#v", got)
	}
	if got["SOFARPC_ALLOW_TARGET_OVERRIDE"] != "true" {
		t.Fatalf("allow target override: %#v", got)
	}
	if got["SOFARPC_ARGS_FILE_ROOT"] != "/root/payloads" {
		t.Fatalf("args file root: %#v", got)
	}
	if got["SOFARPC_ARGS_FILE_MAX_BYTES"] != "1048576" {
		t.Fatalf("args file max: %#v", got)
	}
	if got["SOFARPC_SESSION_PLAN_MAX_BYTES"] != "2097152" {
		t.Fatalf("session plan max: %#v", got)
	}
	if got["SOFARPC_MAX_RESPONSE_BYTES"] != "16777216" {
		t.Fatalf("max response: %#v", got)
	}
}

func TestBuildSetupEnv_InvalidNumericFlag(t *testing.T) {
	_, err := buildSetupEnv(setupOptions{
		argsFileMaxBytes: "abc",
		set:              map[string]bool{"args-file-max-bytes": true},
	})
	if err == nil {
		t.Fatal("expected invalid numeric flag to fail")
	}
}

func TestRunSetup_UserScopeIsDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	command := setupCommandFixture(t)

	err := runSetup([]string{
		"--command", command,
		"--install-skill=false",
		"--allow-invoke",
	})
	if err != nil {
		t.Fatalf("runSetup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); err != nil {
		t.Fatalf("claude config missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); err != nil {
		t.Fatalf("codex config missing: %v", err)
	}
}

func TestRunSetup_UserScopeRejectsProjectSpecificFlags(t *testing.T) {
	command := setupCommandFixture(t)
	err := runSetup([]string{
		"--command", command,
		"--install-skill=false",
		"--direct-url", "bolt://host:12200",
	})
	if err == nil {
		t.Fatal("expected user setup to reject project-specific direct-url")
	}
}

func TestRunSetup_ProjectLocalWritesConfigAndGitignore(t *testing.T) {
	root := t.TempDir()
	err := runSetup([]string{
		"--scope=project",
		"--project-root", root,
		"--local",
		"--direct-url", "bolt://host:12200",
		"--protocol", "bolt",
		"--serialization", "hessian2",
		"--timeout-ms", "3000",
		"--allowed-services", "com.foo.UserFacade",
	})
	if err != nil {
		t.Fatalf("runSetup: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(root, ".sofarpc", "config.local.json"))
	if err != nil {
		t.Fatalf("read local config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg["directUrl"] != "bolt://host:12200" {
		t.Fatalf("directUrl: %#v", cfg)
	}
	if cfg["timeoutMs"] != float64(3000) {
		t.Fatalf("timeoutMs: %#v", cfg)
	}
	if _, ok := cfg["mode"]; ok {
		t.Fatalf("project setup wrote mode: %#v", cfg)
	}
	if got := cfg["allowedServices"].([]any); len(got) != 1 || got[0] != "com.foo.UserFacade" {
		t.Fatalf("allowedServices: %#v", cfg)
	}
	ignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	if !strings.Contains(string(ignore), ".sofarpc/config.local.json") {
		t.Fatalf("local config ignore entry missing:\n%s", ignore)
	}
}

func TestRunSetup_ProjectSharedWritesSharedConfigOnly(t *testing.T) {
	root := t.TempDir()
	err := runSetup([]string{
		"--scope=project",
		"--project-root", root,
		"--shared",
		"--registry-address", "zookeeper://host:2181",
		"--registry-protocol", "zookeeper",
		"--connect-timeout-ms", "1000",
		"--allowed-services", "com.foo.UserFacade",
	})
	if err != nil {
		t.Fatalf("runSetup: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(root, ".sofarpc", "config.json"))
	if err != nil {
		t.Fatalf("read shared config: %v", err)
	}
	if !strings.Contains(string(body), "\"registryAddress\": \"zookeeper://host:2181\"") {
		t.Fatalf("registryAddress missing:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("shared setup should not create gitignore: %v", err)
	}
}

func TestRunSetup_ProjectRejectsTargetWithoutAllowedServices(t *testing.T) {
	root := t.TempDir()
	err := runSetup([]string{
		"--scope=project",
		"--project-root", root,
		"--local",
		"--direct-url", "bolt://host:12200",
	})
	if err == nil {
		t.Fatal("expected project setup to require allowedServices")
	}
	if !strings.Contains(err.Error(), "--allowed-services") {
		t.Fatalf("error should mention --allowed-services, got %v", err)
	}
}

func TestRunSetup_ProjectWritesAllowedServices(t *testing.T) {
	root := t.TempDir()
	err := runSetup([]string{
		"--scope=project",
		"--project-root", root,
		"--shared",
		"--allowed-services", "com.foo.UserFacade, com.foo.OrderFacade",
	})
	if err != nil {
		t.Fatalf("runSetup project: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(root, ".sofarpc", "config.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg struct {
		AllowedServices []string `json:"allowedServices"`
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if len(cfg.AllowedServices) != 2 ||
		cfg.AllowedServices[0] != "com.foo.UserFacade" ||
		cfg.AllowedServices[1] != "com.foo.OrderFacade" {
		t.Fatalf("allowedServices: got %#v", cfg.AllowedServices)
	}
}

func TestRunSetup_ProjectDoesNotOverwriteWithoutForce(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".sofarpc", "config.local.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{\"directUrl\":\"bolt://old:1\"}\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	err := runSetup([]string{
		"--scope=project",
		"--project-root", root,
		"--local",
		"--direct-url", "bolt://new:2",
		"--allowed-services", "com.foo.UserFacade",
	})
	if err == nil {
		t.Fatal("expected overwrite without force to fail")
	}
	if err := runSetup([]string{
		"--scope=project",
		"--project-root", root,
		"--local",
		"--force",
		"--direct-url", "bolt://new:2",
		"--allowed-services", "com.foo.UserFacade",
	}); err != nil {
		t.Fatalf("force runSetup: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), "bolt://new:2") {
		t.Fatalf("force did not overwrite:\n%s", body)
	}
}

func TestRunSetup_ProjectLocalDoesNotWriteConfigWhenGitignoreFails(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".gitignore"), 0o755); err != nil {
		t.Fatalf("mkdir gitignore dir: %v", err)
	}
	err := runSetup([]string{
		"--scope=project",
		"--project-root", root,
		"--local",
		"--direct-url", "bolt://host:12200",
		"--allowed-services", "com.foo.UserFacade",
	})
	if err == nil {
		t.Fatal("expected gitignore failure")
	}
	if _, err := os.Stat(filepath.Join(root, ".sofarpc", "config.local.json")); !os.IsNotExist(err) {
		t.Fatalf("local config should not be written when gitignore fails: %v", err)
	}
}

func TestRunSetup_ProjectDryRunLeavesFilesUntouched(t *testing.T) {
	root := t.TempDir()
	err := runSetup([]string{
		"--scope=project",
		"--project-root", root,
		"--local",
		"--dry-run",
		"--direct-url", "bolt://host:12200",
		"--allowed-services", "com.foo.UserFacade",
	})
	if err != nil {
		t.Fatalf("runSetup dry-run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".sofarpc")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created .sofarpc: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created .gitignore: %v", err)
	}
}

func TestRunSetup_ProjectRejectsUserGuardrails(t *testing.T) {
	root := t.TempDir()
	err := runSetup([]string{
		"--scope=project",
		"--project-root", root,
		"--shared",
		"--direct-url", "bolt://host:12200",
		"--allow-invoke",
	})
	if err == nil {
		t.Fatal("expected project setup to reject user env guardrails")
	}
}

func TestRunSetup_ProjectRequiresSingleTargetFile(t *testing.T) {
	root := t.TempDir()
	err := runSetup([]string{
		"--scope=project",
		"--project-root", root,
		"--direct-url", "bolt://host:12200",
	})
	if err == nil {
		t.Fatal("expected missing --local/--shared to fail")
	}
	err = runSetup([]string{
		"--scope=project",
		"--project-root", root,
		"--local",
		"--shared",
		"--direct-url", "bolt://host:12200",
	})
	if err == nil {
		t.Fatal("expected both --local and --shared to fail")
	}
}

func TestResolveSetupCommandRejectsRelativeCommand(t *testing.T) {
	_, err := resolveSetupCommand("sofarpc-mcp")
	if err == nil {
		t.Fatal("expected relative command to fail")
	}
}

func TestIsLikelyGoRunBinary(t *testing.T) {
	if !isLikelyGoRunBinary(filepath.Join(os.TempDir(), "go-build123", "b001", "exe", "sofarpc-mcp")) {
		t.Fatal("expected go-build path to be treated as temporary")
	}
	if isLikelyGoRunBinary(filepath.Join(os.TempDir(), "sofarpc-mcp")) {
		t.Fatal("regular temp path should not be treated as go run")
	}
}

func readClaudeEnvFromFile(t *testing.T, path string) map[string]string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	return readClaudeEnv(servers["sofarpc"])
}

func setupCommandFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sofarpc-mcp-test-bin")
	if err := os.WriteFile(path, []byte("test binary"), 0o755); err != nil {
		t.Fatalf("write command fixture: %v", err)
	}
	return path
}
