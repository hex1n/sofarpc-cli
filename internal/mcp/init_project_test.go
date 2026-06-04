package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/projectconfig"
	"github.com/hex1n/sofarpc-cli/internal/sourcecontract"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestInitProject_AutoDiscoversFacadeAndWritesLocalConfig(t *testing.T) {
	root := t.TempDir()
	writeInitProjectJava(t, root, "src/main/java/com/foo/UserFacade.java", `
package com.foo;
public interface UserFacade {
    String query(String id);
}
`)
	writeInitProjectJava(t, root, "src/main/java/com/foo/InternalService.java", `
package com.foo;
public interface InternalService {
    String run(String id);
}
`)
	writeInitProjectJava(t, root, "src/main/java/com/foo/UserDTO.java", `
package com.foo;
public class UserDTO {
    private String id;
}
`)

	out, result := callInitProject(t, Options{ProjectContractLoader: sourceContractLoader(t)}, map[string]any{
		"project":   root,
		"directUrl": "bolt://dev-rpc.example.com:12200",
	})
	if result.IsError || out.Error != nil {
		t.Fatalf("init project failed: result=%+v out=%+v", result, out)
	}
	if !out.Ok || !out.Wrote {
		t.Fatalf("expected write success: %+v", out)
	}
	if out.ConfigFile != string(projectconfig.KindLocal) {
		t.Fatalf("configFile: got %q", out.ConfigFile)
	}
	if out.ProjectConfig.DirectURL != "bolt://dev-rpc.example.com:12200" {
		t.Fatalf("directUrl: %+v", out.ProjectConfig)
	}
	if len(out.ProjectConfig.AllowedServices) != 1 || out.ProjectConfig.AllowedServices[0] != "com.foo.UserFacade" {
		t.Fatalf("allowedServices: %+v", out.ProjectConfig.AllowedServices)
	}
	if len(out.Discovery.CandidateServices) != 1 || out.Discovery.CandidateServices[0] != "com.foo.UserFacade" {
		t.Fatalf("candidateServices: %+v", out.Discovery.CandidateServices)
	}
	body, err := os.ReadFile(filepath.Join(root, ".sofarpc", "config.local.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(body), `"allowedServices": [`) || !strings.Contains(string(body), `"com.foo.UserFacade"`) {
		t.Fatalf("config did not contain discovered service:\n%s", body)
	}
	ignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	if !strings.Contains(string(ignore), projectconfig.LocalGitignoreEntry) {
		t.Fatalf("gitignore missing local config entry:\n%s", ignore)
	}
}

func TestInitProject_NoScopeHighConfidenceDiscoveryRequiresExplicitProjectForWrite(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "pom.xml"), []byte("<project/>"), 0o644); err != nil {
		t.Fatalf("write pom: %v", err)
	}
	writeInitProjectJava(t, root, "src/main/java/com/foo/UserFacade.java", `
package com.foo;
public interface UserFacade {
    String query(String id);
}
`)
	restoreCwd := chdirForInitProject(t, root)
	defer restoreCwd()

	out, result := callInitProject(t, Options{ProjectContractLoader: sourceContractLoader(t)}, map[string]any{
		"directUrl": "bolt://dev-rpc.example.com:12200",
	})
	if !result.IsError || out.Error == nil {
		t.Fatalf("expected explicit project requirement: result=%+v out=%+v", result, out)
	}
	if !strings.Contains(out.Error.Message, "project scope must be explicit") {
		t.Fatalf("error should require explicit project: %+v", out.Error)
	}
	if out.ProjectResolution == nil || out.ProjectResolution.Confidence != "high" || out.ProjectResolution.Root != root {
		t.Fatalf("projectResolution: %+v", out.ProjectResolution)
	}
	if _, err := os.Stat(filepath.Join(root, ".sofarpc", "config.local.json")); !os.IsNotExist(err) {
		t.Fatalf("no-scope init should not write config: %v", err)
	}
}

func TestInitProject_DryRunLeavesFilesUntouched(t *testing.T) {
	root := t.TempDir()
	out, result := callInitProject(t, Options{}, map[string]any{
		"project":  root,
		"dryRun":   true,
		"services": []string{"com.foo.UserFacade"},
	})
	if result.IsError || out.Error != nil {
		t.Fatalf("dry-run failed: result=%+v out=%+v", result, out)
	}
	if !out.Ok || !out.DryRun || out.Wrote {
		t.Fatalf("unexpected dry-run output: %+v", out)
	}
	if out.Gitignore == nil || !out.Gitignore.WouldChange {
		t.Fatalf("dry-run should report gitignore would change: %+v", out.Gitignore)
	}
	if _, err := os.Stat(filepath.Join(root, ".sofarpc")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not create .sofarpc: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not create .gitignore: %v", err)
	}
}

func TestInitProject_TargetWithoutAllowedServicesIsRejected(t *testing.T) {
	root := t.TempDir()
	out, result := callInitProject(t, Options{}, map[string]any{
		"project":   root,
		"directUrl": "bolt://dev-rpc.example.com:12200",
	})
	if !result.IsError || out.Error == nil {
		t.Fatalf("expected missing allowedServices rejection: result=%+v out=%+v", result, out)
	}
	if !strings.Contains(out.Error.Message, "allowedServices is required") {
		t.Fatalf("error should explain allowedServices requirement: %+v", out.Error)
	}
	if _, err := os.Stat(filepath.Join(root, ".sofarpc")); !os.IsNotExist(err) {
		t.Fatalf("rejected init should not create .sofarpc: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("rejected init should not create .gitignore: %v", err)
	}
}

func TestInitProject_AllowAllServicesWritesExplicitWildcard(t *testing.T) {
	root := t.TempDir()
	out, result := callInitProject(t, Options{}, map[string]any{
		"project":          root,
		"directUrl":        "bolt://dev-rpc.example.com:12200",
		"allowAllServices": true,
	})
	if result.IsError || out.Error != nil {
		t.Fatalf("init project failed: result=%+v out=%+v", result, out)
	}
	if len(out.ProjectConfig.AllowedServices) != 1 || out.ProjectConfig.AllowedServices[0] != "*" {
		t.Fatalf("allowedServices: %+v", out.ProjectConfig.AllowedServices)
	}
	body, err := os.ReadFile(filepath.Join(root, ".sofarpc", "config.local.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(body), `"*"`) {
		t.Fatalf("config should contain explicit wildcard allowlist:\n%s", body)
	}
}

func TestInitProject_NoScopeLowConfidenceRejectsWrite(t *testing.T) {
	root := t.TempDir()
	restoreCwd := chdirForInitProject(t, root)
	defer restoreCwd()

	out, result := callInitProject(t, Options{}, map[string]any{
		"services": []string{"com.foo.UserFacade"},
	})
	if !result.IsError || out.Error == nil {
		t.Fatalf("expected auto-discovery rejection: result=%+v out=%+v", result, out)
	}
	if !strings.Contains(out.Error.Message, "project scope must be explicit") {
		t.Fatalf("error should explain auto-discovery failure: %+v", out.Error)
	}
	if out.ProjectResolution == nil || out.ProjectResolution.Confidence != "low" {
		t.Fatalf("projectResolution should explain low confidence: %+v", out.ProjectResolution)
	}
}

func TestInitProject_NoScopeLowConfidenceDryRunReturnsCandidates(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pom.xml"), []byte("<project/>"), 0o644); err != nil {
		t.Fatalf("write pom: %v", err)
	}
	restoreCwd := chdirForInitProject(t, root)
	defer restoreCwd()

	out, result := callInitProject(t, Options{}, map[string]any{
		"dryRun": true,
	})
	if result.IsError || out.Error != nil {
		t.Fatalf("dry-run discovery should not fail: result=%+v out=%+v", result, out)
	}
	if !out.Ok || out.ProjectResolution == nil {
		t.Fatalf("dry-run should return low-confidence candidate: %+v", out)
	}
	foundRoot := false
	for _, candidate := range out.ProjectResolution.Candidates {
		if candidate.Root == root {
			foundRoot = true
			break
		}
	}
	if !foundRoot {
		t.Fatalf("dry-run candidates should include current project root %s: %+v", root, out.ProjectResolution.Candidates)
	}
}

func TestInitProject_CustomSuffixDiscoversServiceInterface(t *testing.T) {
	root := t.TempDir()
	writeInitProjectJava(t, root, "src/main/java/com/foo/InternalService.java", `
package com.foo;
public interface InternalService {
    String run(String id);
}
`)

	out, result := callInitProject(t, Options{ProjectContractLoader: sourceContractLoader(t)}, map[string]any{
		"project":             root,
		"dryRun":              true,
		"serviceNameSuffixes": []string{"Service"},
	})
	if result.IsError || out.Error != nil {
		t.Fatalf("init project failed: result=%+v out=%+v", result, out)
	}
	if len(out.ProjectConfig.AllowedServices) != 1 || out.ProjectConfig.AllowedServices[0] != "com.foo.InternalService" {
		t.Fatalf("allowedServices: %+v", out.ProjectConfig.AllowedServices)
	}
}

func TestInitProject_ExistingConfigRequiresForce(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".sofarpc", "config.local.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{\"allowedServices\":[\"old\"]}\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	out, result := callInitProject(t, Options{}, map[string]any{
		"project":  root,
		"services": []string{"com.foo.NewFacade"},
	})
	if !result.IsError || out.Error == nil {
		t.Fatalf("expected overwrite rejection: result=%+v out=%+v", result, out)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(body), `"old"`) {
		t.Fatalf("existing config was changed:\n%s", body)
	}
}

func TestInitProject_ValidationErrorReportsExistingConfig(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".sofarpc", "config.local.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{\"allowedServices\":[\"old\"]}\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	out, result := callInitProject(t, Options{}, map[string]any{
		"project":   root,
		"directUrl": "bolt://dev-rpc.example.com:12200",
	})
	if !result.IsError || out.Error == nil {
		t.Fatalf("expected validation rejection: result=%+v out=%+v", result, out)
	}
	if !out.Existing {
		t.Fatalf("expected existing config to be reported: %+v", out)
	}
	if !strings.Contains(out.Error.Message, "allowedServices is required") {
		t.Fatalf("error should explain allowedServices requirement: %+v", out.Error)
	}
}

func TestInitProject_SharedConfigDoesNotTouchGitignore(t *testing.T) {
	root := t.TempDir()
	out, result := callInitProject(t, Options{}, map[string]any{
		"project":  root,
		"config":   "shared",
		"services": []string{"com.foo.UserFacade"},
	})
	if result.IsError || out.Error != nil {
		t.Fatalf("init shared failed: result=%+v out=%+v", result, out)
	}
	if out.ConfigFile != string(projectconfig.KindShared) || out.Gitignore != nil {
		t.Fatalf("shared config should not touch gitignore: %+v", out)
	}
	if _, err := os.Stat(filepath.Join(root, ".sofarpc", "config.json")); err != nil {
		t.Fatalf("shared config missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("shared config should not create gitignore: %v", err)
	}
}

func sourceContractLoader(t *testing.T) func(string) (contract.Store, error) {
	t.Helper()
	return func(projectRoot string) (contract.Store, error) {
		return sourcecontract.Load(projectRoot)
	}
}

func callInitProject(t *testing.T, opts Options, args map[string]any) (InitProjectOutput, *sdkmcp.CallToolResult) {
	t.Helper()
	server := New(opts)
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	result, err := client.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "sofarpc_init_project",
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("call init_project: %v", err)
	}
	var out InitProjectOutput
	body, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal structured: %v", err)
	}
	return out, result
}

func writeInitProjectJava(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write java: %v", err)
	}
}

func chdirForInitProject(t *testing.T, dir string) func() {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	return func() {
		if err := os.Chdir(prev); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}
}
