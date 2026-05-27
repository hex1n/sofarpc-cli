package projectbootstrap

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/projectconfig"
)

func TestRun_LocalWritesConfigAndGitignore(t *testing.T) {
	root := t.TempDir()

	result, err := Run(Input{
		ProjectRoot:            root,
		Kind:                   projectconfig.KindLocal,
		Config:                 projectConfig("bolt://host:12200", "com.foo.UserFacade"),
		RequireConfigFields:    true,
		RequireAllowedServices: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Wrote || result.DryRun || result.ConfigPath == "" {
		t.Fatalf("result: %+v", result)
	}
	if result.Gitignore == nil || !result.Gitignore.Changed {
		t.Fatalf("gitignore: %+v", result.Gitignore)
	}
	body, err := os.ReadFile(filepath.Join(root, ".sofarpc", "config.local.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(body), `"directUrl": "bolt://host:12200"`) ||
		!strings.Contains(string(body), `"com.foo.UserFacade"`) {
		t.Fatalf("config body:\n%s", body)
	}
	ignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read gitignore: %v", err)
	}
	if !strings.Contains(string(ignore), projectconfig.LocalGitignoreEntry) {
		t.Fatalf("gitignore missing entry:\n%s", ignore)
	}
}

func TestRun_LocalDryRunLeavesFilesUntouched(t *testing.T) {
	root := t.TempDir()

	result, err := Run(Input{
		ProjectRoot:            root,
		Kind:                   projectconfig.KindLocal,
		Config:                 projectConfig("", "com.foo.UserFacade"),
		DryRun:                 true,
		RequireConfigFields:    true,
		RequireAllowedServices: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.DryRun || result.Wrote {
		t.Fatalf("result: %+v", result)
	}
	if result.Gitignore == nil || !result.Gitignore.WouldChange {
		t.Fatalf("gitignore: %+v", result.Gitignore)
	}
	if _, err := os.Stat(filepath.Join(root, ".sofarpc")); !os.IsNotExist(err) {
		t.Fatalf("dry run created .sofarpc: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("dry run created .gitignore: %v", err)
	}
}

func TestRun_RejectsMissingRequiredFields(t *testing.T) {
	_, err := Run(Input{
		ProjectRoot:            t.TempDir(),
		Kind:                   projectconfig.KindLocal,
		RequireConfigFields:    true,
		RequireAllowedServices: true,
	})
	if !errors.Is(err, ErrNoConfigFields) {
		t.Fatalf("err = %v, want ErrNoConfigFields", err)
	}

	_, err = Run(Input{
		ProjectRoot:            t.TempDir(),
		Kind:                   projectconfig.KindLocal,
		Config:                 projectConfig("bolt://host:12200", ""),
		RequireConfigFields:    true,
		RequireAllowedServices: true,
	})
	if !errors.Is(err, ErrAllowedServicesMissing) {
		t.Fatalf("err = %v, want ErrAllowedServicesMissing", err)
	}

	result, err := Run(Input{
		ProjectRoot:            t.TempDir(),
		Kind:                   projectconfig.KindLocal,
		Config:                 projectconfig.Config{DirectURL: " bolt://host:12200 ", AllowedServices: []string{" "}},
		RequireConfigFields:    true,
		RequireAllowedServices: true,
	})
	if !errors.Is(err, ErrAllowedServicesMissing) {
		t.Fatalf("err = %v, want ErrAllowedServicesMissing", err)
	}
	if result.Config.DirectURL != "bolt://host:12200" || len(result.Config.AllowedServices) != 0 {
		t.Fatalf("config should be normalized before validation: %+v", result.Config)
	}
}

func TestRun_ValidationErrorsStillReportExistingConfig(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".sofarpc", "config.local.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{\"allowedServices\":[\"old\"]}\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	result, err := Run(Input{
		ProjectRoot:            root,
		Kind:                   projectconfig.KindLocal,
		Config:                 projectConfig("bolt://host:12200", ""),
		RequireConfigFields:    true,
		RequireAllowedServices: true,
	})
	if !errors.Is(err, ErrAllowedServicesMissing) {
		t.Fatalf("err = %v, want ErrAllowedServicesMissing", err)
	}
	if !result.Existing {
		t.Fatalf("expected existing config to be reported: %+v", result)
	}
}

func TestRun_ExistingConfigRequiresForce(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".sofarpc", "config.local.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{\"allowedServices\":[\"old\"]}\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	result, err := Run(Input{
		ProjectRoot:            root,
		Kind:                   projectconfig.KindLocal,
		Config:                 projectConfig("bolt://new:12200", "com.foo.UserFacade"),
		RequireConfigFields:    true,
		RequireAllowedServices: true,
	})
	var existsErr ExistingConfigError
	if !errors.As(err, &existsErr) {
		t.Fatalf("err = %v, want ExistingConfigError", err)
	}
	if !result.Existing || result.Gitignore != nil {
		t.Fatalf("result: %+v", result)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), `"old"`) {
		t.Fatalf("config changed:\n%s", body)
	}
}

func TestRun_ForceOverwritesExistingConfig(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".sofarpc", "config.local.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{\"allowedServices\":[\"old\"]}\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	result, err := Run(Input{
		ProjectRoot:            root,
		Kind:                   projectconfig.KindLocal,
		Config:                 projectConfig("bolt://new:12200", "com.foo.UserFacade"),
		Force:                  true,
		RequireConfigFields:    true,
		RequireAllowedServices: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Existing || !result.Overwrote || !result.Wrote {
		t.Fatalf("result: %+v", result)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), `"bolt://new:12200"`) ||
		strings.Contains(string(body), `"old"`) {
		t.Fatalf("force overwrite body:\n%s", body)
	}
}

func TestRun_LocalDoesNotWriteConfigWhenGitignoreFails(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".gitignore"), 0o755); err != nil {
		t.Fatalf("mkdir gitignore dir: %v", err)
	}
	_, err := Run(Input{
		ProjectRoot:            root,
		Kind:                   projectconfig.KindLocal,
		Config:                 projectConfig("bolt://host:12200", "com.foo.UserFacade"),
		RequireConfigFields:    true,
		RequireAllowedServices: true,
	})
	if err == nil {
		t.Fatal("expected gitignore failure")
	}
	if _, err := os.Stat(filepath.Join(root, ".sofarpc", "config.local.json")); !os.IsNotExist(err) {
		t.Fatalf("config should not be written when gitignore fails: %v", err)
	}
}

func TestRun_SharedDoesNotTouchGitignore(t *testing.T) {
	root := t.TempDir()

	result, err := Run(Input{
		ProjectRoot:            root,
		Kind:                   projectconfig.KindShared,
		Config:                 projectConfig("", "com.foo.UserFacade"),
		RequireConfigFields:    true,
		RequireAllowedServices: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Gitignore != nil {
		t.Fatalf("shared result should not include gitignore: %+v", result.Gitignore)
	}
	if _, err := os.Stat(filepath.Join(root, ".sofarpc", "config.json")); err != nil {
		t.Fatalf("shared config missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("shared setup should not create gitignore: %v", err)
	}
}

func projectConfig(directURL, service string) projectconfig.Config {
	cfg := projectconfig.Config{DirectURL: directURL}
	if service != "" {
		cfg.AllowedServices = []string{service}
	}
	return cfg
}
