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
		DefaultTarget: model.TargetDefaults{
			Mode:      model.ModeDirect,
			DirectURL: "bolt://127.0.0.1:12201",
		},
	}
	manifestPath := filepath.Join(cwd, "rpcctl.manifest.json")
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
