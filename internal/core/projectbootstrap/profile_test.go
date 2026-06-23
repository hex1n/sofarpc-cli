package projectbootstrap

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/projectconfig"
)

func writeConfigFile(t *testing.T, root string, kind projectconfig.Kind, body string) {
	t.Helper()
	path := projectconfig.ConfigPath(root, kind)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestWriteProfile_AddsNewProfilePreservingBase(t *testing.T) {
	root := t.TempDir()
	writeConfigFile(t, root, projectconfig.KindShared, `{
  "directUrl": "bolt://base:12200",
  "allowedServices": ["com.foo.Svc"]
}`)

	res, err := WriteProfile(ProfileInput{
		ProjectRoot: root,
		Kind:        projectconfig.KindShared,
		Name:        "test",
		Profile:     projectconfig.ProfileConfig{DirectURL: "bolt://test-host:12200"},
	})
	if err != nil {
		t.Fatalf("WriteProfile: %v", err)
	}
	if !res.Wrote || res.ProfileExisted || !res.FileExisted {
		t.Fatalf("result flags: %+v", res)
	}

	read, err := projectconfig.Read(root, projectconfig.KindShared)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if read.Config.DirectURL != "bolt://base:12200" {
		t.Fatalf("base directUrl should be preserved: %+v", read.Config)
	}
	if len(read.Config.AllowedServices) != 1 || read.Config.AllowedServices[0] != "com.foo.Svc" {
		t.Fatalf("base allowedServices should be preserved: %+v", read.Config.AllowedServices)
	}
	if read.Config.Profiles["test"].DirectURL != "bolt://test-host:12200" {
		t.Fatalf("profile not written: %+v", read.Config.Profiles)
	}
}

func TestWriteProfile_ExistingProfileNeedsForce(t *testing.T) {
	root := t.TempDir()
	first := ProfileInput{
		ProjectRoot: root,
		Kind:        projectconfig.KindLocal,
		Name:        "test",
		Profile:     projectconfig.ProfileConfig{DirectURL: "bolt://one:12200"},
	}
	if _, err := WriteProfile(first); err != nil {
		t.Fatalf("first WriteProfile: %v", err)
	}

	second := first
	second.Profile = projectconfig.ProfileConfig{DirectURL: "bolt://two:12200"}
	_, err := WriteProfile(second)
	var existing ExistingProfileError
	if !errors.As(err, &existing) {
		t.Fatalf("overwriting an existing profile without force should fail, got %v", err)
	}

	second.Force = true
	res, err := WriteProfile(second)
	if err != nil {
		t.Fatalf("forced overwrite: %v", err)
	}
	if !res.ProfileExisted {
		t.Fatalf("forced overwrite should report the profile pre-existed: %+v", res)
	}
	read, _ := projectconfig.Read(root, projectconfig.KindLocal)
	if read.Config.Profiles["test"].DirectURL != "bolt://two:12200" {
		t.Fatalf("forced overwrite did not replace the profile: %+v", read.Config.Profiles)
	}
}

func TestWriteProfile_SetDefaultAndGitignoreForLocal(t *testing.T) {
	root := t.TempDir()
	res, err := WriteProfile(ProfileInput{
		ProjectRoot: root,
		Kind:        projectconfig.KindLocal,
		Name:        "test",
		Profile:     projectconfig.ProfileConfig{DirectURL: "bolt://test:12200"},
		SetDefault:  true,
	})
	if err != nil {
		t.Fatalf("WriteProfile: %v", err)
	}
	if res.Gitignore == nil || !res.Gitignore.Changed {
		t.Fatalf("local write should ensure .gitignore: %+v", res.Gitignore)
	}
	read, _ := projectconfig.Read(root, projectconfig.KindLocal)
	if read.Config.DefaultProfile != "test" {
		t.Fatalf("setDefault should record defaultProfile: %+v", read.Config)
	}
}

func TestWriteProfile_PreservesExplicitEmptyAllowlist(t *testing.T) {
	root := t.TempDir()
	writeConfigFile(t, root, projectconfig.KindLocal, `{
  "directUrl": "bolt://base:12200",
  "allowedServices": []
}`)

	if _, err := WriteProfile(ProfileInput{
		ProjectRoot: root,
		Kind:        projectconfig.KindLocal,
		Name:        "test",
		Profile:     projectconfig.ProfileConfig{DirectURL: "bolt://test:12200"},
	}); err != nil {
		t.Fatalf("WriteProfile: %v", err)
	}

	read, err := projectconfig.Read(root, projectconfig.KindLocal)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !read.AllowedServicesSet {
		t.Fatalf("explicit block-all allowlist must survive a profile merge")
	}
}

func TestWriteProfile_RejectsEmptyProfile(t *testing.T) {
	root := t.TempDir()
	_, err := WriteProfile(ProfileInput{
		ProjectRoot: root,
		Kind:        projectconfig.KindLocal,
		Name:        "test",
		Profile:     projectconfig.ProfileConfig{},
	})
	if !errors.Is(err, ErrProfileNoFields) {
		t.Fatalf("an empty profile should be rejected, got %v", err)
	}
}

func TestUseProfile_SetsDefaultForDefinedProfile(t *testing.T) {
	root := t.TempDir()
	writeConfigFile(t, root, projectconfig.KindShared, `{
  "profiles": { "test": { "directUrl": "bolt://test:12200" } }
}`)

	res, err := UseProfile(UseProfileInput{ProjectRoot: root, Name: "test"})
	if err != nil {
		t.Fatalf("UseProfile: %v", err)
	}
	if !res.Wrote || res.ConfigPath != projectconfig.ConfigPath(root, projectconfig.KindLocal) {
		t.Fatalf("UseProfile should write the local file: %+v", res)
	}
	local, _ := projectconfig.Read(root, projectconfig.KindLocal)
	if local.Config.DefaultProfile != "test" {
		t.Fatalf("defaultProfile not set in local config: %+v", local.Config)
	}
}

func TestUseProfile_UndefinedProfileErrors(t *testing.T) {
	root := t.TempDir()
	writeConfigFile(t, root, projectconfig.KindShared, `{
  "profiles": { "test": { "directUrl": "bolt://test:12200" } }
}`)

	_, err := UseProfile(UseProfileInput{ProjectRoot: root, Name: "prod"})
	var notDefined ProfileNotDefinedError
	if !errors.As(err, &notDefined) {
		t.Fatalf("undefined profile should error, got %v", err)
	}
	if len(notDefined.Available) != 1 || notDefined.Available[0] != "test" {
		t.Fatalf("error should list available profiles: %+v", notDefined.Available)
	}
	if _, statErr := os.Stat(projectconfig.ConfigPath(root, projectconfig.KindLocal)); !os.IsNotExist(statErr) {
		t.Fatalf("undefined profile use must not write a local config: %v", statErr)
	}
}
