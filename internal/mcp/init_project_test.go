package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/projectconfig"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestInitProject_TargetWritesLocalConfig(t *testing.T) {
	root := t.TempDir()
	out, result := callInitProject(t, Options{}, map[string]any{
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
	body, err := os.ReadFile(filepath.Join(root, ".sofarpc", "config.local.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(body), `"allowedServices"`) {
		t.Fatalf("config contains retired allowedServices: %s", body)
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
	root := isolatedInitProjectTempDir(t)
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

	out, result := callInitProject(t, Options{}, map[string]any{
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
		"project":   root,
		"dryRun":    true,
		"directUrl": "bolt://dev-rpc.example.com:12200",
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

func TestInitProject_IgnoresLegacyServiceOptions(t *testing.T) {
	root := t.TempDir()
	out, result := callInitProject(t, Options{}, map[string]any{
		"project":             root,
		"directUrl":           "bolt://dev-rpc.example.com:12200",
		"allowAllServices":    true,
		"services":            []string{"com.foo.UserFacade"},
		"serviceNameSuffixes": []string{"Facade"},
	})
	if result.IsError || out.Error != nil {
		t.Fatalf("init project failed: result=%+v out=%+v", result, out)
	}
	body, err := os.ReadFile(filepath.Join(root, ".sofarpc", "config.local.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(body), `"allowedServices"`) {
		t.Fatalf("config contains retired allowedServices:\n%s", body)
	}
}

func TestInitProject_NoScopeLowConfidenceRejectsWrite(t *testing.T) {
	root := isolatedInitProjectTempDir(t)
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git: %v", err)
	}
	restoreCwd := chdirForInitProject(t, root)
	defer restoreCwd()

	out, result := callInitProject(t, Options{}, map[string]any{
		"directUrl": "bolt://dev-rpc.example.com:12200",
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
	root := isolatedInitProjectTempDir(t)
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git: %v", err)
	}
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
		"project":   root,
		"directUrl": "bolt://dev-rpc.example.com:12200",
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
		"project": root,
	})
	if !result.IsError || out.Error == nil {
		t.Fatalf("expected validation rejection: result=%+v out=%+v", result, out)
	}
	if !out.Existing {
		t.Fatalf("expected existing config to be reported: %+v", out)
	}
	if !strings.Contains(out.Error.Message, "no project config fields resolved") {
		t.Fatalf("error should explain missing config fields: %+v", out.Error)
	}
}

func TestInitProject_SharedConfigDoesNotTouchGitignore(t *testing.T) {
	root := t.TempDir()
	out, result := callInitProject(t, Options{}, map[string]any{
		"project":   root,
		"config":    "shared",
		"directUrl": "bolt://dev-rpc.example.com:12200",
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

func TestInitProject_RejectsRetiredRegistryField(t *testing.T) {
	for _, key := range []string{"registryAddress", "registryProtocol"} {
		t.Run(key, func(t *testing.T) {
			root := t.TempDir()
			out, result := callInitProject(t, Options{}, map[string]any{
				"project":   root,
				"directUrl": "bolt://dev-rpc.example.com:12200",
				key:         "zookeeper://host:2181",
			})
			if !result.IsError || out.Error == nil || out.Error.Code != errcode.ArgsInvalid {
				t.Fatalf("a retired registry field must be rejected: result=%+v out=%+v", result, out)
			}
			if !strings.Contains(out.Error.Message, key) {
				t.Fatalf("error should name the retired field %q: %+v", key, out.Error)
			}
			if _, err := os.Stat(filepath.Join(root, ".sofarpc")); !os.IsNotExist(err) {
				t.Fatalf("rejected init must not write config: %v", err)
			}
		})
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

func isolatedInitProjectTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		base := `C:\tmp`
		if err := os.MkdirAll(base, 0o755); err == nil {
			if d, err := os.MkdirTemp(base, "sofarpc-init-project-*"); err == nil {
				t.Cleanup(func() { _ = os.RemoveAll(d) })
				dir = d
			}
		}
	}
	// Resolve symlinks so the root compares equal with discovery output, which
	// canonicalizes its start dir (/var vs /private/var on macOS).
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
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
