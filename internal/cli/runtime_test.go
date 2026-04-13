package cli

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

func TestRunRuntimeSourceSetStoresDirectorySource(t *testing.T) {
	cwd := t.TempDir()
	configDir := t.TempDir()
	paths := config.Paths{
		ConfigDir:          configDir,
		CacheDir:           t.TempDir(),
		ContextsFile:       filepath.Join(configDir, "contexts.json"),
		RuntimeSourcesFile: filepath.Join(configDir, "runtime-sources.json"),
	}
	app := &App{
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Cwd:     cwd,
		Paths:   paths,
		Runtime: runtime.NewManager(paths, cwd),
	}

	if err := app.runRuntimeSourceSet([]string{
		"--kind", "directory",
		"--path", ".",
		"local",
	}); err != nil {
		t.Fatalf("runRuntimeSourceSet() error = %v", err)
	}

	store, err := config.LoadRuntimeSourceStore(paths)
	if err != nil {
		t.Fatalf("LoadRuntimeSourceStore() error = %v", err)
	}
	source := store.Sources["local"]
	if source.Kind != "directory" {
		t.Fatalf("expected directory kind, got %+v", source)
	}
	if source.Path != filepath.Clean(cwd) {
		t.Fatalf("expected path to resolve against cwd, got %q", source.Path)
	}
	if store.Active != "local" {
		t.Fatalf("expected first source to become active, got %q", store.Active)
	}
}

func TestRunRuntimeSourceSetRejectsURLKinds(t *testing.T) {
	cwd := t.TempDir()
	configDir := t.TempDir()
	paths := config.Paths{
		ConfigDir:          configDir,
		CacheDir:           t.TempDir(),
		ContextsFile:       filepath.Join(configDir, "contexts.json"),
		RuntimeSourcesFile: filepath.Join(configDir, "runtime-sources.json"),
	}
	app := &App{
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Cwd:     cwd,
		Paths:   paths,
		Runtime: runtime.NewManager(paths, cwd),
	}

	if err := app.runRuntimeSourceSet([]string{
		"--kind", "url-template",
		"--path", "https://example.test/{version}.jar",
		"remote",
	}); err == nil {
		t.Fatal("expected url-template kind to be rejected")
	}
}
