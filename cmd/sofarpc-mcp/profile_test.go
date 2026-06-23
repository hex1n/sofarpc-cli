package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/projectconfig"
)

func seedProjectConfig(t *testing.T, root string, kind projectconfig.Kind, body string) {
	t.Helper()
	path := projectconfig.ConfigPath(root, kind)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}
}

func TestRunSetup_ProjectProfileWritesWithoutAllowedServices(t *testing.T) {
	root := t.TempDir()
	if err := runSetup([]string{
		"--scope=project",
		"--project-root", root,
		"--shared",
		"--profile", "test",
		"--direct-url", "bolt://test-host:12200",
		"--set-default",
	}); err != nil {
		t.Fatalf("runSetup profile: %v", err)
	}
	read, err := projectconfig.Read(root, projectconfig.KindShared)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if read.Config.Profiles["test"].DirectURL != "bolt://test-host:12200" {
		t.Fatalf("profile not written: %+v", read.Config.Profiles)
	}
	if read.Config.DefaultProfile != "test" {
		t.Fatalf("--set-default should record defaultProfile: %+v", read.Config)
	}
}

func TestRunSetup_ProjectProfileRejectsAllowedServices(t *testing.T) {
	root := t.TempDir()
	err := runSetup([]string{
		"--scope=project",
		"--project-root", root,
		"--local",
		"--profile", "test",
		"--direct-url", "bolt://test-host:12200",
		"--allowed-services", "com.foo.Svc",
	})
	if err == nil || !strings.Contains(err.Error(), "--allowed-services") {
		t.Fatalf("profile mode should reject --allowed-services, got %v", err)
	}
}

func TestRunSetup_SetDefaultWithoutProfileErrors(t *testing.T) {
	root := t.TempDir()
	err := runSetup([]string{
		"--scope=project",
		"--project-root", root,
		"--local",
		"--set-default",
		"--direct-url", "bolt://host:12200",
		"--allowed-services", "com.foo.Svc",
	})
	if err == nil || !strings.Contains(err.Error(), "--set-default requires --profile") {
		t.Fatalf("--set-default without --profile should error, got %v", err)
	}
}

func TestRunSetup_ProfileRejectedAtUserScope(t *testing.T) {
	err := runSetup([]string{"--scope=user", "--profile", "test"})
	if err == nil || !strings.Contains(err.Error(), "--profile is project-specific") {
		t.Fatalf("--profile should be rejected at user scope, got %v", err)
	}
}

func TestRunProfileUse_SetsDefaultForDefinedProfile(t *testing.T) {
	root := t.TempDir()
	seedProjectConfig(t, root, projectconfig.KindShared, `{
  "profiles": { "test": { "directUrl": "bolt://test:12200" } }
}`)

	if err := runProfile([]string{"use", "test", "--project-root", root}); err != nil {
		t.Fatalf("profile use: %v", err)
	}
	local, err := projectconfig.Read(root, projectconfig.KindLocal)
	if err != nil {
		t.Fatalf("read local: %v", err)
	}
	if local.Config.DefaultProfile != "test" {
		t.Fatalf("defaultProfile not set: %+v", local.Config)
	}
}

func TestRunProfileUse_UndefinedProfileErrors(t *testing.T) {
	root := t.TempDir()
	seedProjectConfig(t, root, projectconfig.KindShared, `{
  "profiles": { "test": { "directUrl": "bolt://test:12200" } }
}`)

	err := runProfile([]string{"use", "prod", "--project-root", root})
	if err == nil || !strings.Contains(err.Error(), "not defined") {
		t.Fatalf("undefined profile should error, got %v", err)
	}
	if !strings.Contains(err.Error(), "test") {
		t.Fatalf("error should list the available profile, got %v", err)
	}
}

func TestRunProfileUse_RequiresNameFirst(t *testing.T) {
	if err := runProfile([]string{"use", "--project-root", "."}); err == nil {
		t.Fatal("profile use without a name should error")
	}
	if err := runProfile([]string{"unknown"}); err == nil || !strings.Contains(err.Error(), "unknown profile subcommand") {
		t.Fatalf("unknown subcommand should error, got %v", err)
	}
}
