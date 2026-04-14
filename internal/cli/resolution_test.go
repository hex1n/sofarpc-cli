package cli

import (
	"path/filepath"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

func TestResolveInvocationPrefersFlagsOverContextAndManifest(t *testing.T) {
	cwd := t.TempDir()
	paths := config.Paths{
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
				Protocol:  "bolt",
			},
		},
	}
	if err := config.SaveContextStore(paths, store); err != nil {
		t.Fatalf("SaveContextStore() error = %v", err)
	}
	manifest := model.Manifest{
		SchemaVersion:  "v1alpha1",
		SofaRPCVersion: defaultSofaRPCVersion,
		DefaultTarget: model.TargetConfig{
			Mode:      model.ModeDirect,
			DirectURL: "bolt://127.0.0.1:12201",
		},
	}
	manifestPath := filepath.Join(cwd, "sofarpc.manifest.json")
	if err := config.SaveManifest(manifestPath, manifest); err != nil {
		t.Fatalf("SaveManifest() error = %v", err)
	}
	app := &App{
		Cwd:     cwd,
		Paths:   paths,
		Runtime: runtime.NewManager(paths, cwd),
	}
	resolved, err := app.resolveInvocation(invocationInputs{
		ManifestPath: manifestPath,
		Service:      "com.example.UserService",
		Method:       "getUser",
		ArgsJSON:     "[]",
		DirectURL:    "bolt://127.0.0.1:23300",
	})
	if err != nil {
		t.Fatalf("resolveInvocation() error = %v", err)
	}
	if got := resolved.Request.Target.DirectURL; got != "bolt://127.0.0.1:23300" {
		t.Fatalf("expected direct url from flags, got %q", got)
	}
}

func TestResolveSofaRPCVersionAttribution(t *testing.T) {
	cases := []struct {
		name        string
		flag        string
		manifest    string
		wantVersion string
		wantSource  string
	}{
		{name: "flag wins", flag: "5.9.0", manifest: "5.8.0", wantVersion: "5.9.0", wantSource: "flag"},
		{name: "manifest when no flag", manifest: "5.8.0", wantVersion: "5.8.0", wantSource: "manifest"},
		{name: "default when neither", wantVersion: defaultSofaRPCVersion, wantSource: "default"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotVersion, gotSource := resolveSofaRPCVersion(tc.flag, tc.manifest)
			if gotVersion != tc.wantVersion {
				t.Fatalf("version: got %q, want %q", gotVersion, tc.wantVersion)
			}
			if gotSource != tc.wantSource {
				t.Fatalf("source: got %q, want %q", gotSource, tc.wantSource)
			}
		})
	}
}

func TestResolveInvocationPrefersServiceUniqueIDOverContextAndManifestDefault(t *testing.T) {
	cwd := t.TempDir()
	configDir := t.TempDir()
	paths := config.Paths{
		ConfigDir:          configDir,
		CacheDir:           t.TempDir(),
		ContextsFile:       filepath.Join(configDir, "contexts.json"),
		RuntimeSourcesFile: filepath.Join(configDir, "runtime-sources.json"),
	}
	store := model.ContextStore{
		Active: "dev",
		Contexts: map[string]model.Context{
			"dev": {
				Name:      "dev",
				Mode:      model.ModeDirect,
				DirectURL: "bolt://127.0.0.1:12200",
				UniqueID:  "context-uid",
			},
		},
	}
	if err := config.SaveContextStore(paths, store); err != nil {
		t.Fatalf("SaveContextStore() error = %v", err)
	}
	manifest := model.Manifest{
		SchemaVersion:  "v1alpha1",
		SofaRPCVersion: defaultSofaRPCVersion,
		DefaultTarget: model.TargetConfig{
			Mode:      model.ModeDirect,
			DirectURL: "bolt://127.0.0.1:12201",
			UniqueID:  "manifest-uid",
		},
		Services: map[string]model.ServiceConfig{
			"com.example.UserService": {
				UniqueID: "service-uid",
				Methods: map[string]model.MethodConfig{
					"getUser": {},
				},
			},
		},
	}
	manifestPath := filepath.Join(cwd, "sofarpc.manifest.json")
	if err := config.SaveManifest(manifestPath, manifest); err != nil {
		t.Fatalf("SaveManifest() error = %v", err)
	}
	app := &App{
		Cwd:     cwd,
		Paths:   paths,
		Runtime: runtime.NewManager(paths, cwd),
	}
	resolved, err := app.resolveInvocation(invocationInputs{
		ManifestPath: manifestPath,
		Service:      "com.example.UserService",
		Method:       "getUser",
		ArgsJSON:     "[]",
	})
	if err != nil {
		t.Fatalf("resolveInvocation() error = %v", err)
	}
	if got := resolved.Request.Target.UniqueID; got != "service-uid" {
		t.Fatalf("expected service uniqueId to win, got %q", got)
	}
}

func TestResolveInvocationPrefersFlagUniqueIDOverServiceUniqueID(t *testing.T) {
	cwd := t.TempDir()
	configDir := t.TempDir()
	paths := config.Paths{
		ConfigDir:          configDir,
		CacheDir:           t.TempDir(),
		ContextsFile:       filepath.Join(configDir, "contexts.json"),
		RuntimeSourcesFile: filepath.Join(configDir, "runtime-sources.json"),
	}
	store := model.ContextStore{
		Active: "dev",
		Contexts: map[string]model.Context{
			"dev": {
				Name:      "dev",
				Mode:      model.ModeDirect,
				DirectURL: "bolt://127.0.0.1:12200",
				UniqueID:  "context-uid",
			},
		},
	}
	if err := config.SaveContextStore(paths, store); err != nil {
		t.Fatalf("SaveContextStore() error = %v", err)
	}
	manifest := model.Manifest{
		SchemaVersion:  "v1alpha1",
		SofaRPCVersion: defaultSofaRPCVersion,
		DefaultTarget: model.TargetConfig{
			Mode:      model.ModeDirect,
			DirectURL: "bolt://127.0.0.1:12201",
			UniqueID:  "manifest-uid",
		},
		Services: map[string]model.ServiceConfig{
			"com.example.UserService": {
				UniqueID: "service-uid",
				Methods: map[string]model.MethodConfig{
					"getUser": {},
				},
			},
		},
	}
	manifestPath := filepath.Join(cwd, "sofarpc.manifest.json")
	if err := config.SaveManifest(manifestPath, manifest); err != nil {
		t.Fatalf("SaveManifest() error = %v", err)
	}
	app := &App{
		Cwd:     cwd,
		Paths:   paths,
		Runtime: runtime.NewManager(paths, cwd),
	}
	resolved, err := app.resolveInvocation(invocationInputs{
		ManifestPath: manifestPath,
		Service:      "com.example.UserService",
		Method:       "getUser",
		ArgsJSON:     "[]",
		UniqueID:     "flag-uid",
	})
	if err != nil {
		t.Fatalf("resolveInvocation() error = %v", err)
	}
	if got := resolved.Request.Target.UniqueID; got != "flag-uid" {
		t.Fatalf("expected flag uniqueId to win, got %q", got)
	}
}
