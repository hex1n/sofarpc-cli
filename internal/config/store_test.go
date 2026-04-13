package config

import (
	"path/filepath"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/model"
)

func TestSaveAndLoadContextStore(t *testing.T) {
	paths := Paths{
		ConfigDir:          t.TempDir(),
		CacheDir:           t.TempDir(),
		ContextsFile:       filepath.Join(t.TempDir(), "contexts.json"),
		RuntimeSourcesFile: filepath.Join(t.TempDir(), "runtime-sources.json"),
	}
	store := model.ContextStore{
		Active: "dev",
		Contexts: map[string]model.Context{
			"dev": {
				Name:      "dev",
				Mode:      model.ModeDirect,
				DirectURL: "bolt://127.0.0.1:12200",
			},
		},
	}
	if err := SaveContextStore(paths, store); err != nil {
		t.Fatalf("SaveContextStore() error = %v", err)
	}
	loaded, err := LoadContextStore(paths)
	if err != nil {
		t.Fatalf("LoadContextStore() error = %v", err)
	}
	if loaded.Active != "dev" {
		t.Fatalf("expected active context dev, got %q", loaded.Active)
	}
	if loaded.Contexts["dev"].DirectURL != "bolt://127.0.0.1:12200" {
		t.Fatalf("unexpected direct url %q", loaded.Contexts["dev"].DirectURL)
	}
}

func TestSaveAndLoadRuntimeSourceStore(t *testing.T) {
	paths := Paths{
		ConfigDir:          t.TempDir(),
		CacheDir:           t.TempDir(),
		ContextsFile:       filepath.Join(t.TempDir(), "contexts.json"),
		RuntimeSourcesFile: filepath.Join(t.TempDir(), "runtime-sources.json"),
	}
	store := model.RuntimeSourceStore{
		Active: "workspace",
		Sources: map[string]model.RuntimeSource{
			"workspace": {
				Name: "workspace",
				Kind: "directory",
				Path: "C:\\work\\runtimes",
			},
		},
	}
	if err := SaveRuntimeSourceStore(paths, store); err != nil {
		t.Fatalf("SaveRuntimeSourceStore() error = %v", err)
	}
	loaded, err := LoadRuntimeSourceStore(paths)
	if err != nil {
		t.Fatalf("LoadRuntimeSourceStore() error = %v", err)
	}
	if loaded.Active != "workspace" {
		t.Fatalf("expected active source workspace, got %q", loaded.Active)
	}
	if loaded.Sources["workspace"].Path != "C:\\work\\runtimes" {
		t.Fatalf("unexpected source path %q", loaded.Sources["workspace"].Path)
	}
}
