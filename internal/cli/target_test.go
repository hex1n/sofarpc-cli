package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/model"
)

func newTargetTestApp(t *testing.T) *App {
	t.Helper()
	cwd := t.TempDir()
	configDir := t.TempDir()
	paths := config.Paths{
		ConfigDir:          configDir,
		CacheDir:           t.TempDir(),
		ContextsFile:       filepath.Join(configDir, "contexts.json"),
		RuntimeSourcesFile: filepath.Join(configDir, "runtime-sources.json"),
	}
	return &App{
		Stdout: io.Discard,
		Stderr: io.Discard,
		Cwd:    cwd,
		Paths:  paths,
	}
}

func TestRunTargetShowPrintsResolvedDirectContext(t *testing.T) {
	app := newTargetTestApp(t)
	store := model.ContextStore{
		Active: "dev-direct",
		Contexts: map[string]model.Context{
			"dev-direct": {
				Name:             "dev-direct",
				Mode:             model.ModeDirect,
				DirectURL:        "bolt://127.0.0.1:1",
				Protocol:         "bolt",
				Serialization:    "hessian2",
				TimeoutMS:        3000,
				ConnectTimeoutMS: 1000,
			},
		},
	}
	if err := config.SaveContextStore(app.Paths, store); err != nil {
		t.Fatalf("SaveContextStore() error = %v", err)
	}

	var stdout bytes.Buffer
	app.Stdout = &stdout
	if err := app.runTargetShow(nil); err != nil {
		t.Fatalf("runTargetShow() error = %v", err)
	}

	out := stdout.String()
	for _, want := range []string{
		"project root:      " + app.Cwd,
		"active context:    dev-direct",
		"mode:              direct",
		"direct url:        bolt://127.0.0.1:1",
		"protocol:          bolt",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestRunTargetShowUsesManifestServiceUniqueID(t *testing.T) {
	app := newTargetTestApp(t)
	manifestPath := filepath.Join(app.Cwd, "sofarpc.manifest.json")
	if err := config.SaveManifest(manifestPath, model.Manifest{
		DefaultTarget: model.TargetConfig{
			Mode:             model.ModeDirect,
			DirectURL:        "bolt://127.0.0.1:1",
			Protocol:         "bolt",
			Serialization:    "hessian2",
			TimeoutMS:        4000,
			ConnectTimeoutMS: 1200,
		},
		Services: map[string]model.ServiceConfig{
			"com.example.OrderFacade": {UniqueID: "blue"},
		},
	}); err != nil {
		t.Fatalf("SaveManifest() error = %v", err)
	}

	var stdout bytes.Buffer
	app.Stdout = &stdout
	if err := app.runTargetShow([]string{"--service", "com.example.OrderFacade"}); err != nil {
		t.Fatalf("runTargetShow() error = %v", err)
	}

	out := stdout.String()
	for _, want := range []string{
		"project root:      " + app.Cwd,
		"service:           com.example.OrderFacade",
		"uniqueId:          blue",
		"timeout ms:        4000",
		"connect timeout:   1200",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestRunTargetShowSupportsProjectOutsideCurrentDirectory(t *testing.T) {
	app := newTargetTestApp(t)
	projectRoot := t.TempDir()
	manifestPath := filepath.Join(projectRoot, "sofarpc.manifest.json")
	if err := config.SaveManifest(manifestPath, model.Manifest{
		DefaultTarget: model.TargetConfig{
			Mode:             model.ModeDirect,
			DirectURL:        "bolt://127.0.0.1:1",
			Protocol:         "bolt",
			Serialization:    "hessian2",
			TimeoutMS:        3000,
			ConnectTimeoutMS: 1000,
		},
	}); err != nil {
		t.Fatalf("SaveManifest() error = %v", err)
	}

	var stdout bytes.Buffer
	app.Stdout = &stdout
	if err := app.runTargetShow([]string{"--project", projectRoot}); err != nil {
		t.Fatalf("runTargetShow() error = %v", err)
	}

	out := stdout.String()
	for _, want := range []string{
		"project root:      " + projectRoot,
		"manifest path:     " + manifestPath,
		"manifest loaded:   true",
		"mode:              direct",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestResolveTargetReportIncludesAllCandidatesAndExplanation(t *testing.T) {
	app := newTargetTestApp(t)
	projectRoot := t.TempDir()
	nested := filepath.Join(projectRoot, "svc", "impl")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll(nested) error = %v", err)
	}

	store := model.ContextStore{
		Active: "z-active",
		Contexts: map[string]model.Context{
			"z-active": {
				Name:      "z-active",
				Mode:      model.ModeDirect,
				DirectURL: "bolt://127.0.0.1:19000",
			},
			"b-project": {
				Name:        "b-project",
				Mode:        model.ModeDirect,
				DirectURL:   "bolt://127.0.0.1:19001",
				ProjectRoot: projectRoot,
			},
			"a-project": {
				Name:        "a-project",
				Mode:        model.ModeDirect,
				DirectURL:   "bolt://127.0.0.1:19002",
				ProjectRoot: projectRoot,
			},
		},
	}
	if err := config.SaveContextStore(app.Paths, store); err != nil {
		t.Fatalf("SaveContextStore() error = %v", err)
	}

	report, err := app.resolveTargetReport(projectRoot, invocationInputs{}, true, true)
	if err != nil {
		t.Fatalf("resolveTargetReport() error = %v", err)
	}
	if report.ActiveContext != "a-project" {
		t.Fatalf("ActiveContext = %q, want a-project", report.ActiveContext)
	}
	if len(report.Candidates) != 3 {
		t.Fatalf("Candidates len = %d, want 3", len(report.Candidates))
	}
	if len(report.Layers) == 0 {
		t.Fatal("expected target layers")
	}
	foundSelected := false
	for _, candidate := range report.Candidates {
		if candidate.Name == "a-project" {
			foundSelected = candidate.Selected
			if !strings.Contains(strings.Join(candidate.Roles, ","), "project-context") {
				t.Fatalf("candidate roles = %v, want project-context", candidate.Roles)
			}
		}
	}
	if !foundSelected {
		t.Fatal("expected a-project to be selected")
	}
	if len(report.Explain) == 0 {
		t.Fatal("expected explanation lines")
	}
	if !strings.Contains(strings.Join(report.Explain, "\n"), "wins by name") {
		t.Fatalf("Explain = %v, want tie-break explanation", report.Explain)
	}
	if report.Layers[len(report.Layers)-1].Name != "final-target" {
		t.Fatalf("last layer = %q, want final-target", report.Layers[len(report.Layers)-1].Name)
	}
}

func TestRunTargetShowPrintsCandidatesAndSelection(t *testing.T) {
	app := newTargetTestApp(t)
	projectRoot := t.TempDir()
	store := model.ContextStore{
		Active: "z-active",
		Contexts: map[string]model.Context{
			"z-active": {
				Name:      "z-active",
				Mode:      model.ModeDirect,
				DirectURL: "bolt://127.0.0.1:19000",
			},
			"a-project": {
				Name:        "a-project",
				Mode:        model.ModeDirect,
				DirectURL:   "bolt://127.0.0.1:19002",
				ProjectRoot: projectRoot,
			},
		},
	}
	if err := config.SaveContextStore(app.Paths, store); err != nil {
		t.Fatalf("SaveContextStore() error = %v", err)
	}

	var stdout bytes.Buffer
	app.Stdout = &stdout
	if err := app.runTargetShow([]string{"--project", projectRoot, "--all", "--explain"}); err != nil {
		t.Fatalf("runTargetShow() error = %v", err)
	}

	out := stdout.String()
	for _, want := range []string{
		"candidates:",
		"* a-project [project-context]",
		"z-active [active-context]",
		"layers:",
		"selected-context:a-project [context]",
		"final-target [resolved]",
		"selection:",
		"selected project-scoped context",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestResolveTargetReportAllowsExplicitDirectWhenActiveContextIsStale(t *testing.T) {
	app := newTargetTestApp(t)
	store := model.ContextStore{
		Active:   "missing-active",
		Contexts: map[string]model.Context{},
	}
	if err := config.SaveContextStore(app.Paths, store); err != nil {
		t.Fatalf("SaveContextStore() error = %v", err)
	}

	report, err := app.resolveTargetReport("", invocationInputs{
		DirectURL:     "bolt://127.0.0.1:19003",
		Protocol:      "bolt",
		Serialization: "hessian2",
	}, false, false)
	if err != nil {
		t.Fatalf("resolveTargetReport() error = %v", err)
	}
	if report.ActiveContext != "" {
		t.Fatalf("ActiveContext = %q, want empty", report.ActiveContext)
	}
	if report.Target.DirectURL != "bolt://127.0.0.1:19003" {
		t.Fatalf("DirectURL = %q", report.Target.DirectURL)
	}
}

func TestResolveTargetReportAllowsExplicitDirectWhenManifestDefaultContextIsStale(t *testing.T) {
	app := newTargetTestApp(t)
	manifestPath := filepath.Join(app.Cwd, "sofarpc.manifest.json")
	if err := config.SaveManifest(manifestPath, model.Manifest{
		DefaultContext: "missing-default",
		DefaultTarget: model.TargetConfig{
			Protocol:         "bolt",
			Serialization:    "hessian2",
			TimeoutMS:        3000,
			ConnectTimeoutMS: 1000,
		},
	}); err != nil {
		t.Fatalf("SaveManifest() error = %v", err)
	}

	report, err := app.resolveTargetReport("", invocationInputs{
		DirectURL:     "bolt://127.0.0.1:19004",
		Protocol:      "bolt",
		Serialization: "hessian2",
	}, false, true)
	if err != nil {
		t.Fatalf("resolveTargetReport() error = %v", err)
	}
	if report.Target.DirectURL != "bolt://127.0.0.1:19004" {
		t.Fatalf("DirectURL = %q", report.Target.DirectURL)
	}
	if got := strings.Join(report.Explain, "\n"); !strings.Contains(got, "explicit flag overrides applied") {
		t.Fatalf("Explain = %v", report.Explain)
	}
}

func TestResolveTargetReportIncludesServiceManifestAndExplicitLayers(t *testing.T) {
	app := newTargetTestApp(t)
	manifestPath := filepath.Join(app.Cwd, "sofarpc.manifest.json")
	if err := config.SaveManifest(manifestPath, model.Manifest{
		DefaultTarget: model.TargetConfig{
			Mode:             model.ModeDirect,
			DirectURL:        "bolt://127.0.0.1:19010",
			Protocol:         "bolt",
			Serialization:    "hessian2",
			TimeoutMS:        3000,
			ConnectTimeoutMS: 1000,
		},
		Services: map[string]model.ServiceConfig{
			"com.example.OrderFacade": {UniqueID: "blue"},
		},
	}); err != nil {
		t.Fatalf("SaveManifest() error = %v", err)
	}

	report, err := app.resolveTargetReport("", invocationInputs{
		Service:   "com.example.OrderFacade",
		DirectURL: "bolt://127.0.0.1:19011",
		Protocol:  "bolt",
		TimeoutMS: 9000,
		UniqueID:  "green",
	}, true, true)
	if err != nil {
		t.Fatalf("resolveTargetReport() error = %v", err)
	}

	wantLayers := []string{
		"explicit-flags",
		"manifest.service",
		"manifest.defaultTarget",
		"built-in-defaults",
		"final-target",
	}
	if len(report.Layers) != len(wantLayers) {
		t.Fatalf("Layers len = %d, want %d", len(report.Layers), len(wantLayers))
	}
	for i, want := range wantLayers {
		if report.Layers[i].Name != want {
			t.Fatalf("layer[%d] = %q, want %q", i, report.Layers[i].Name, want)
		}
	}
	if report.Layers[0].Target.UniqueID != "green" {
		t.Fatalf("explicit layer uniqueId = %q", report.Layers[0].Target.UniqueID)
	}
	if report.Layers[1].Target.UniqueID != "blue" {
		t.Fatalf("service layer uniqueId = %q", report.Layers[1].Target.UniqueID)
	}
	if report.Target.DirectURL != "bolt://127.0.0.1:19011" {
		t.Fatalf("final DirectURL = %q", report.Target.DirectURL)
	}
	if report.Target.UniqueID != "green" {
		t.Fatalf("final UniqueID = %q", report.Target.UniqueID)
	}
}
