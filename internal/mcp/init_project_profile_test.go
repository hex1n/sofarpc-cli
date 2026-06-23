package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/projectconfig"
)

// TestInitProject_ProfileModeMergesIntoExistingFile verifies profile mode adds
// profiles[name] without disturbing the base config and without requiring force
// for a brand-new profile.
func TestInitProject_ProfileModeMergesIntoExistingFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".sofarpc", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{
  "directUrl": "bolt://base:12200",
  "allowedServices": ["com.foo.Svc"]
}`), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	out, result := callInitProject(t, Options{}, map[string]any{
		"project":    root,
		"config":     "shared",
		"profile":    "test",
		"setDefault": true,
		"directUrl":  "bolt://test-host:12200",
	})
	if result.IsError || out.Error != nil {
		t.Fatalf("profile init failed: result=%+v out=%+v", result, out)
	}
	if !out.Ok || !out.Wrote {
		t.Fatalf("expected write success: %+v", out)
	}

	read, err := projectconfig.Read(root, projectconfig.KindShared)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if read.Config.DirectURL != "bolt://base:12200" {
		t.Fatalf("base directUrl should be preserved: %+v", read.Config)
	}
	if len(read.Config.AllowedServices) != 1 {
		t.Fatalf("base allowedServices should be preserved: %+v", read.Config.AllowedServices)
	}
	if read.Config.Profiles["test"].DirectURL != "bolt://test-host:12200" {
		t.Fatalf("profile target not written: %+v", read.Config.Profiles)
	}
	if read.Config.DefaultProfile != "test" {
		t.Fatalf("setDefault should record defaultProfile: %+v", read.Config)
	}
}

func TestInitProject_ProfileModeRejectsServiceFlags(t *testing.T) {
	root := t.TempDir()
	out, result := callInitProject(t, Options{}, map[string]any{
		"project":   root,
		"profile":   "test",
		"directUrl": "bolt://test-host:12200",
		"services":  []any{"com.foo.Svc"},
	})
	if !result.IsError || out.Error == nil {
		t.Fatalf("profile mode should reject service flags: %+v", out)
	}
	if !strings.Contains(out.Error.Message, "allowedServices") {
		t.Fatalf("error should explain allowlist stays a base setting: %+v", out.Error)
	}
}

func TestInitProject_ProfileModeExistingProfileNeedsForce(t *testing.T) {
	root := t.TempDir()
	args := map[string]any{
		"project":   root,
		"config":    "local",
		"profile":   "test",
		"directUrl": "bolt://one:12200",
	}
	if out, result := callInitProject(t, Options{}, args); result.IsError || out.Error != nil {
		t.Fatalf("first profile write failed: %+v", out)
	}

	args["directUrl"] = "bolt://two:12200"
	out, result := callInitProject(t, Options{}, args)
	if !result.IsError || out.Error == nil {
		t.Fatalf("overwriting an existing profile without force should fail: %+v", out)
	}
	if !strings.Contains(out.Error.Message, "force=true") {
		t.Fatalf("error should point at force=true: %+v", out.Error)
	}

	args["force"] = true
	if out, result := callInitProject(t, Options{}, args); result.IsError || out.Error != nil {
		t.Fatalf("forced overwrite failed: %+v", out)
	}
	read, _ := projectconfig.Read(root, projectconfig.KindLocal)
	if read.Config.Profiles["test"].DirectURL != "bolt://two:12200" {
		t.Fatalf("forced overwrite did not replace the profile: %+v", read.Config.Profiles)
	}
}

func TestInitProject_ProfileModeDryRunLeavesFileUntouched(t *testing.T) {
	root := t.TempDir()
	out, result := callInitProject(t, Options{}, map[string]any{
		"project":   root,
		"config":    "local",
		"profile":   "test",
		"directUrl": "bolt://test-host:12200",
		"dryRun":    true,
	})
	if result.IsError || out.Error != nil {
		t.Fatalf("dry-run failed: %+v", out)
	}
	if out.Wrote {
		t.Fatalf("dry-run must not write: %+v", out)
	}
	if _, err := os.Stat(filepath.Join(root, ".sofarpc", "config.local.json")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not create the config file: %v", err)
	}
}
