package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

func TestRunRuntimeSourceSetPreservesURLTemplate(t *testing.T) {
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
		"--path", "https://example.test/runtime/{version}/rpc-runtime-worker-sofa-{version}.jar",
		"remote",
	}); err != nil {
		t.Fatalf("runRuntimeSourceSet() error = %v", err)
	}

	store, err := config.LoadRuntimeSourceStore(paths)
	if err != nil {
		t.Fatalf("LoadRuntimeSourceStore() error = %v", err)
	}
	if got := store.Sources["remote"].Path; got != "https://example.test/runtime/{version}/rpc-runtime-worker-sofa-{version}.jar" {
		t.Fatalf("expected URL template to be preserved, got %q", got)
	}
}

func TestRunRuntimeSourceSetStoresSHA256URLForURLTemplate(t *testing.T) {
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
		"--path", "https://example.test/runtime/{version}/rpc-runtime-worker-sofa-{version}.jar",
		"--sha256-url", "https://example.test/runtime/{version}/rpc-runtime-worker-sofa-{version}.jar.sha256",
		"remote",
	}); err != nil {
		t.Fatalf("runRuntimeSourceSet() error = %v", err)
	}

	store, err := config.LoadRuntimeSourceStore(paths)
	if err != nil {
		t.Fatalf("LoadRuntimeSourceStore() error = %v", err)
	}
	if got := store.Sources["remote"].SHA256URL; got != "https://example.test/runtime/{version}/rpc-runtime-worker-sofa-{version}.jar.sha256" {
		t.Fatalf("expected SHA-256 URL template to be preserved, got %q", got)
	}
}

func TestRunRuntimeSourceSetRejectsSHA256URLForLocalSources(t *testing.T) {
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

	err := app.runRuntimeSourceSet([]string{
		"--kind", "directory",
		"--path", ".",
		"--sha256-url", "https://example.test/runtime/{version}.sha256",
		"local",
	})
	if err == nil {
		t.Fatal("expected local runtime source to reject --sha256-url")
	}
}

func TestRunRuntimeSourceSetPreservesManifestURL(t *testing.T) {
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
		"--kind", "manifest-url",
		"--path", "https://example.test/runtime/manifest.json",
		"catalog",
	}); err != nil {
		t.Fatalf("runRuntimeSourceSet() error = %v", err)
	}

	store, err := config.LoadRuntimeSourceStore(paths)
	if err != nil {
		t.Fatalf("LoadRuntimeSourceStore() error = %v", err)
	}
	source := store.Sources["catalog"]
	if source.Kind != "manifest-url" {
		t.Fatalf("expected manifest-url kind, got %+v", source)
	}
	if source.Path != "https://example.test/runtime/manifest.json" {
		t.Fatalf("expected manifest URL to be preserved, got %q", source.Path)
	}
}

func TestRunRuntimeSourceListIncludesValidationsWhenVersionProvided(t *testing.T) {
	cwd := t.TempDir()
	configDir := t.TempDir()
	paths := config.Paths{
		ConfigDir:          configDir,
		CacheDir:           t.TempDir(),
		ContextsFile:       filepath.Join(configDir, "contexts.json"),
		RuntimeSourcesFile: filepath.Join(configDir, "runtime-sources.json"),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/runtime/5.7.6/rpc-runtime-worker-sofa-5.7.6.jar":
			w.WriteHeader(http.StatusOK)
		case "/runtime/5.7.6/rpc-runtime-worker-sofa-5.7.6.jar.sha256":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := config.SaveRuntimeSourceStore(paths, model.RuntimeSourceStore{
		Active: "remote",
		Sources: map[string]model.RuntimeSource{
			"remote": {
				Name:      "remote",
				Kind:      "url-template",
				Path:      server.URL + "/runtime/{version}/rpc-runtime-worker-sofa-{version}.jar",
				SHA256URL: server.URL + "/runtime/{version}/rpc-runtime-worker-sofa-{version}.jar.sha256",
			},
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeSourceStore() error = %v", err)
	}

	var stdout bytes.Buffer
	app := &App{
		Stdout:  &stdout,
		Stderr:  io.Discard,
		Cwd:     cwd,
		Paths:   paths,
		Runtime: runtime.NewManager(paths, cwd),
	}

	if err := app.runRuntimeSourceList([]string{"--version", "5.7.6"}); err != nil {
		t.Fatalf("runRuntimeSourceList() error = %v", err)
	}

	var report model.RuntimeSourceListReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if report.Active != "remote" || report.Version != "5.7.6" {
		t.Fatalf("unexpected report header %+v", report)
	}
	if len(report.Validations) != 1 {
		t.Fatalf("expected one validation, got %+v", report)
	}
	validation := report.Validations[0]
	if !validation.OK || !validation.Active || !validation.ArtifactReachable || !validation.ChecksumAvailable {
		t.Fatalf("expected successful validation summary, got %+v", validation)
	}
}
