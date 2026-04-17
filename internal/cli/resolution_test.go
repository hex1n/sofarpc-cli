package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
	"github.com/hex1n/sofarpc-cli/internal/targetmodel"
)

func TestResolveInvocationPrefersFlagsOverContextAndManifest(t *testing.T) {
	cwd := t.TempDir()
	paths := config.Paths{
		ConfigDir:          t.TempDir(),
		CacheDir:           t.TempDir(),
		ContextsFile:       filepath.Join(t.TempDir(), "contexts.json"),
		RuntimeSourcesFile: filepath.Join(t.TempDir(), "runtime-sources.json"),
	}
	store := targetmodel.ContextStore{
		Active: "dev",
		Contexts: map[string]targetmodel.Context{
			"dev": {
				Name:      "dev",
				Mode:      targetmodel.ModeDirect,
				DirectURL: "bolt://127.0.0.1:12200",
				Protocol:  "bolt",
			},
		},
	}
	if err := config.SaveContextStore(paths, store); err != nil {
		t.Fatalf("SaveContextStore() error = %v", err)
	}
	manifest := targetmodel.Manifest{
		SchemaVersion:  "v1alpha1",
		SofaRPCVersion: defaultSofaRPCVersion,
		DefaultTarget: targetmodel.TargetConfig{
			Mode:      targetmodel.ModeDirect,
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
	store := targetmodel.ContextStore{
		Active: "dev",
		Contexts: map[string]targetmodel.Context{
			"dev": {
				Name:      "dev",
				Mode:      targetmodel.ModeDirect,
				DirectURL: "bolt://127.0.0.1:12200",
				UniqueID:  "context-uid",
			},
		},
	}
	if err := config.SaveContextStore(paths, store); err != nil {
		t.Fatalf("SaveContextStore() error = %v", err)
	}
	manifest := targetmodel.Manifest{
		SchemaVersion:  "v1alpha1",
		SofaRPCVersion: defaultSofaRPCVersion,
		DefaultTarget: targetmodel.TargetConfig{
			Mode:      targetmodel.ModeDirect,
			DirectURL: "bolt://127.0.0.1:12201",
			UniqueID:  "manifest-uid",
		},
		Services: map[string]targetmodel.ServiceConfig{
			"com.example.UserService": {
				UniqueID: "service-uid",
				Methods: map[string]targetmodel.MethodConfig{
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
	store := targetmodel.ContextStore{
		Active: "dev",
		Contexts: map[string]targetmodel.Context{
			"dev": {
				Name:      "dev",
				Mode:      targetmodel.ModeDirect,
				DirectURL: "bolt://127.0.0.1:12200",
				UniqueID:  "context-uid",
			},
		},
	}
	if err := config.SaveContextStore(paths, store); err != nil {
		t.Fatalf("SaveContextStore() error = %v", err)
	}
	manifest := targetmodel.Manifest{
		SchemaVersion:  "v1alpha1",
		SofaRPCVersion: defaultSofaRPCVersion,
		DefaultTarget: targetmodel.TargetConfig{
			Mode:      targetmodel.ModeDirect,
			DirectURL: "bolt://127.0.0.1:12201",
			UniqueID:  "manifest-uid",
		},
		Services: map[string]targetmodel.ServiceConfig{
			"com.example.UserService": {
				UniqueID: "service-uid",
				Methods: map[string]targetmodel.MethodConfig{
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

func TestResolveActiveContextSelectsProjectContextBeforeActive(t *testing.T) {
	cwd := t.TempDir()
	repoRoot := filepath.Join(cwd, "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("create repo root: %v", err)
	}
	store := targetmodel.ContextStore{
		Active: "global",
		Contexts: map[string]targetmodel.Context{
			"global": {
				Name:        "global",
				Mode:        targetmodel.ModeDirect,
				DirectURL:   "bolt://127.0.0.1:12200",
				ProjectRoot: "",
			},
			"project-a": {
				Name:        "project-a",
				Mode:        targetmodel.ModeDirect,
				DirectURL:   "bolt://127.0.0.1:12201",
				ProjectRoot: repoRoot,
			},
		},
	}
	name, ctx := resolveActiveContext(store, "", "", repoRoot, filepath.Join(repoRoot, "sofarpc.manifest.json"))
	if name != "project-a" {
		t.Fatalf("expected project context name, got %q", name)
	}
	if ctx.DirectURL != "bolt://127.0.0.1:12201" {
		t.Fatalf("expected project context direct URL, got %q", ctx.DirectURL)
	}
}

func TestAutoResolveStubPathsFromFacadeConfig(t *testing.T) {
	project := t.TempDir()
	cfgDir := filepath.Join(project, ".sofarpc")
	modDir := filepath.Join(project, "api", "build", "libs")
	depsDir := filepath.Join(project, "api", "target", "facade-deps")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("create module dir: %v", err)
	}
	if err := os.MkdirAll(depsDir, 0o755); err != nil {
		t.Fatalf("create deps dir: %v", err)
	}

	jarFile := filepath.Join(modDir, "order-facade-1.0.0.jar")
	if err := os.WriteFile(jarFile, []byte("fake"), 0o644); err != nil {
		t.Fatalf("write jar file: %v", err)
	}
	depFile := filepath.Join(depsDir, "dep-1.0.0.jar")
	if err := os.WriteFile(depFile, []byte("fake"), 0o644); err != nil {
		t.Fatalf("write dep file: %v", err)
	}

	cfgPath := filepath.Join(cfgDir, "config.json")
	cfg := struct {
		FacadeModules []struct {
			Name    string `json:"name"`
			JarGlob string `json:"jarGlob"`
			DepsDir string `json:"depsDir"`
		} `json:"facadeModules"`
	}{
		FacadeModules: []struct {
			Name    string `json:"name"`
			JarGlob string `json:"jarGlob"`
			DepsDir string `json:"depsDir"`
		}{
			{
				Name:    "order",
				JarGlob: "api/build/libs/*-facade-*.jar",
				DepsDir: "api/target/facade-deps",
			},
		},
	}
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("create .sofarpc dir: %v", err)
	}
	if body, err := json.Marshal(cfg); err == nil {
		if err := os.WriteFile(cfgPath, append(body, '\n'), 0o644); err != nil {
			t.Fatalf("write facade config: %v", err)
		}
	} else {
		t.Fatalf("marshal facade config: %v", err)
	}

	paths, err := resolveStubPaths(project, filepath.Join(project, "sofarpc.manifest.json"), nil, "", "")
	if err != nil {
		t.Fatalf("resolveStubPaths() error = %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 stub paths, got %d", len(paths))
	}
}

func TestAutoResolveStubPathsFromDiscoveredProjectArtifacts(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatalf("create .git dir: %v", err)
	}
	moduleDir := filepath.Join(project, "order-facade")
	sourceDir := filepath.Join(moduleDir, "src", "main", "java", "com", "example")
	depsDir := filepath.Join(moduleDir, "target", "facade-deps")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	if err := os.MkdirAll(depsDir, 0o755); err != nil {
		t.Fatalf("create deps dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "pom.xml"), []byte(`<project><artifactId>order-facade</artifactId></project>`), 0o644); err != nil {
		t.Fatalf("write pom.xml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "OrderFacade.java"), []byte(`package com.example; public interface OrderFacade {}`), 0o644); err != nil {
		t.Fatalf("write OrderFacade.java: %v", err)
	}
	jarFile := filepath.Join(moduleDir, "target", "order-facade-1.0.0.jar")
	depFile := filepath.Join(depsDir, "dep-1.0.0.jar")
	if err := os.MkdirAll(filepath.Dir(jarFile), 0o755); err != nil {
		t.Fatalf("create target dir: %v", err)
	}
	if err := os.WriteFile(jarFile, []byte("fake"), 0o644); err != nil {
		t.Fatalf("write jar file: %v", err)
	}
	if err := os.WriteFile(depFile, []byte("fake"), 0o644); err != nil {
		t.Fatalf("write dep file: %v", err)
	}

	paths, err := resolveStubPaths(project, filepath.Join(project, "sofarpc.manifest.json"), nil, "", "com.example.OrderFacade")
	if err != nil {
		t.Fatalf("resolveStubPaths() error = %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 stub paths, got %d (%v)", len(paths), paths)
	}
}

func TestAutoResolveStubPathsNarrowsToMatchedModule(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatalf("create .git dir: %v", err)
	}
	writeModule := func(name, service string) string {
		moduleDir := filepath.Join(project, name)
		sourceDir := filepath.Join(moduleDir, "src", "main", "java", "com", "example")
		if err := os.MkdirAll(sourceDir, 0o755); err != nil {
			t.Fatalf("create source dir for %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(moduleDir, "pom.xml"), []byte(`<project><artifactId>`+name+`</artifactId></project>`), 0o644); err != nil {
			t.Fatalf("write pom.xml for %s: %v", name, err)
		}
		className := strings.TrimPrefix(service, "com.example.")
		if err := os.WriteFile(filepath.Join(sourceDir, className+".java"), []byte(`package com.example; public interface `+className+` {}`), 0o644); err != nil {
			t.Fatalf("write source for %s: %v", name, err)
		}
		jarFile := filepath.Join(moduleDir, "target", name+"-1.0.0.jar")
		if err := os.MkdirAll(filepath.Dir(jarFile), 0o755); err != nil {
			t.Fatalf("create target dir for %s: %v", name, err)
		}
		if err := os.WriteFile(jarFile, []byte("fake"), 0o644); err != nil {
			t.Fatalf("write jar file for %s: %v", name, err)
		}
		return jarFile
	}
	orderJar := writeModule("order-facade", "com.example.OrderFacade")
	_ = writeModule("user-facade", "com.example.UserFacade")

	paths, err := resolveStubPaths(project, filepath.Join(project, "sofarpc.manifest.json"), nil, "", "com.example.OrderFacade")
	if err != nil {
		t.Fatalf("resolveStubPaths() error = %v", err)
	}
	if len(paths) != 1 || paths[0] != orderJar {
		t.Fatalf("paths = %v, want [%s]", paths, orderJar)
	}
}
