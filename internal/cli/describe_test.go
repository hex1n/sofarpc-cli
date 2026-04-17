package cli

import (
	"bytes"
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
