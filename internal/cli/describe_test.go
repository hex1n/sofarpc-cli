package cli

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

func newDescribeTestApp(t *testing.T) *App {
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
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Cwd:     cwd,
		Paths:   paths,
		Runtime: runtime.NewManager(paths, cwd),
	}
}

func TestRunDescribeRequiresSingleFQCN(t *testing.T) {
	app := newDescribeTestApp(t)
	if err := app.runDescribe(nil); err == nil {
		t.Fatal("expected arity error with no positional")
	}
	if err := app.runDescribe([]string{"a", "b"}); err == nil {
		t.Fatal("expected arity error with two positionals")
	}
}

func TestRunDescribePrefersProjectSourceBeforeManifestAndWorker(t *testing.T) {
	original := describeServiceFromProject
	t.Cleanup(func() {
		describeServiceFromProject = original
	})

	var stdout bytes.Buffer
	app := newDescribeTestApp(t)
	app.Stdout = &stdout
	describeServiceFromProject = func(projectRoot, service string) (model.ServiceSchema, error) {
		if projectRoot == "" {
			t.Fatal("projectRoot should not be empty")
		}
		if service != "com.example.OrderFacade" {
			t.Fatalf("service = %q", service)
		}
		return model.ServiceSchema{
			Service: service,
			Methods: []model.MethodSchema{{Name: "importAsset", ParamTypes: []string{"com.example.OrderRequest"}}},
		}, nil
	}

	if err := app.runDescribe([]string{"com.example.OrderFacade"}); err != nil {
		t.Fatalf("runDescribe() error = %v", err)
	}
	if !strings.Contains(stdout.String(), `"service": "com.example.OrderFacade"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestRunDescribeFullResponseIncludesFallbackNotes(t *testing.T) {
	originalProject := describeServiceFromProject
	originalArtifacts := describeServiceFromArtifacts
	t.Cleanup(func() {
		describeServiceFromProject = originalProject
		describeServiceFromArtifacts = originalArtifacts
	})

	var stdout bytes.Buffer
	app := newDescribeTestApp(t)
	app.Stdout = &stdout
	describeServiceFromProject = func(projectRoot, service string) (model.ServiceSchema, error) {
		return model.ServiceSchema{}, fmt.Errorf("source miss")
	}
	describeServiceFromArtifacts = func(projectRoot, service string) (model.ServiceSchema, error) {
		return model.ServiceSchema{
			Service: service,
			Methods: []model.MethodSchema{{Name: "importAsset"}},
		}, nil
	}

	if err := app.runDescribe([]string{"--full-response", "com.example.OrderFacade"}); err != nil {
		t.Fatalf("runDescribe() error = %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		`"contractSource": "jar-javap"`,
		`"contractNotes": [`,
		`"project-source: source miss"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestRunDescribeFullResponseWritesStructuredFailure(t *testing.T) {
	originalProject := describeServiceFromProject
	originalArtifacts := describeServiceFromArtifacts
	t.Cleanup(func() {
		describeServiceFromProject = originalProject
		describeServiceFromArtifacts = originalArtifacts
	})

	var stderr bytes.Buffer
	app := newDescribeTestApp(t)
	app.Stderr = &stderr
	describeServiceFromProject = func(projectRoot, service string) (model.ServiceSchema, error) {
		return model.ServiceSchema{}, fmt.Errorf("source miss")
	}
	describeServiceFromArtifacts = func(projectRoot, service string) (model.ServiceSchema, error) {
		return model.ServiceSchema{}, fmt.Errorf("artifact miss")
	}

	missingJava := filepath.Join(t.TempDir(), "missing-java")
	err := app.runDescribe([]string{"--full-response", "--java-bin", missingJava, "com.example.OrderFacade"})
	exitErr, ok := err.(*exitError)
	if !ok {
		t.Fatalf("expected exitError, got %T (%v)", err, err)
	}
	if !exitErr.Silent() {
		t.Fatalf("expected silent exitError, got %+v", exitErr)
	}

	out := stderr.String()
	for _, want := range []string{
		`"error":`,
		`"contractNotes": [`,
		`"project-source: source miss"`,
		`"jar-javap: artifact miss"`,
		`"workerClasspath": "runtime-only"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected stderr to contain %q, got:\n%s", want, out)
		}
	}
}
