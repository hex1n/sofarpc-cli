package cli

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
)

func TestResolveServiceSchemaPrefersProjectSource(t *testing.T) {
	original := describeServiceFromProject
	originalArtifacts := describeServiceFromArtifacts
	originalLegacy := describeServiceLegacyFallback
	t.Cleanup(func() {
		describeServiceFromProject = original
		describeServiceFromArtifacts = originalArtifacts
		describeServiceLegacyFallback = originalLegacy
	})

	want := model.ServiceSchema{
		Service: "com.example.OrderFacade",
		Methods: []model.MethodSchema{
			{Name: "importAsset", ParamTypes: []string{"com.example.OrderRequest"}},
		},
	}
	describeServiceFromProject = func(projectRoot, service string) (model.ServiceSchema, error) {
		if projectRoot == "" {
			t.Fatal("projectRoot should not be empty")
		}
		if service != "com.example.OrderFacade" {
			t.Fatalf("service = %q", service)
		}
		return want, nil
	}

	app := &App{Cwd: t.TempDir()}
	got, err := app.resolveServiceSchema(context.Background(), "", runtime.Spec{}, "com.example.OrderFacade", runtime.DescribeOptions{})
	if err != nil {
		t.Fatalf("resolveServiceSchema() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema = %+v, want %+v", got, want)
	}
}

func TestResolveServiceSchemaFallsBackToArtifacts(t *testing.T) {
	original := describeServiceFromProject
	originalArtifacts := describeServiceFromArtifacts
	originalLegacy := describeServiceLegacyFallback
	t.Cleanup(func() {
		describeServiceFromProject = original
		describeServiceFromArtifacts = originalArtifacts
		describeServiceLegacyFallback = originalLegacy
	})

	describeServiceFromProject = func(string, string) (model.ServiceSchema, error) {
		return model.ServiceSchema{}, errors.New("source miss")
	}
	want := model.ServiceSchema{
		Service: "com.example.OrderFacade",
		Methods: []model.MethodSchema{{Name: "importAsset"}},
	}
	describeServiceFromArtifacts = func(projectRoot, service string) (model.ServiceSchema, error) {
		return want, nil
	}

	app := &App{Cwd: t.TempDir()}
	got, err := app.resolveServiceSchema(context.Background(), "", runtime.Spec{}, "com.example.OrderFacade", runtime.DescribeOptions{})
	if err != nil {
		t.Fatalf("resolveServiceSchema() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema = %+v, want %+v", got, want)
	}
}

func TestResolveLocalServiceSchemaSignalsMiss(t *testing.T) {
	original := describeServiceFromProject
	originalArtifacts := describeServiceFromArtifacts
	originalLegacy := describeServiceLegacyFallback
	t.Cleanup(func() {
		describeServiceFromProject = original
		describeServiceFromArtifacts = originalArtifacts
		describeServiceLegacyFallback = originalLegacy
	})

	describeServiceFromProject = func(string, string) (model.ServiceSchema, error) {
		return model.ServiceSchema{}, errors.New("source miss")
	}
	describeServiceFromArtifacts = func(string, string) (model.ServiceSchema, error) {
		return model.ServiceSchema{}, errors.New("artifact miss")
	}

	app := &App{Cwd: t.TempDir()}
	_, err := app.resolveLocalServiceSchema(context.Background(), "", "com.example.OrderFacade", false)
	if !errors.Is(err, errLocalSchemaUnavailable) {
		t.Fatalf("resolveLocalServiceSchema() error = %v, want errLocalSchemaUnavailable", err)
	}
}

func TestResolveServiceSchemaFallsBackToLegacyWorkerDescribeWhenLocalMisses(t *testing.T) {
	originalProject := describeServiceFromProject
	originalArtifacts := describeServiceFromArtifacts
	originalLegacy := describeServiceLegacyFallback
	t.Cleanup(func() {
		describeServiceFromProject = originalProject
		describeServiceFromArtifacts = originalArtifacts
		describeServiceLegacyFallback = originalLegacy
	})

	describeServiceFromProject = func(string, string) (model.ServiceSchema, error) {
		return model.ServiceSchema{}, errors.New("source miss")
	}
	describeServiceFromArtifacts = func(string, string) (model.ServiceSchema, error) {
		return model.ServiceSchema{}, errors.New("artifact miss")
	}
	describeServiceLegacyFallback = func(_ context.Context, _ *runtime.Manager, _ runtime.Spec, service string, _ runtime.DescribeOptions) (model.ServiceSchema, error) {
		return model.ServiceSchema{
			Service: service,
			Methods: []model.MethodSchema{{Name: "legacyFallback"}},
		}, nil
	}

	app := &App{Cwd: t.TempDir(), Runtime: &runtime.Manager{}}
	got, err := app.resolveServiceSchemaDetailed(context.Background(), "", runtime.Spec{}, "com.example.OrderFacade", runtime.DescribeOptions{})
	if err != nil {
		t.Fatalf("resolveServiceSchemaDetailed() error = %v", err)
	}
	if got.Source != "legacy-worker-describe" {
		t.Fatalf("source = %q, want legacy-worker-describe", got.Source)
	}
	if len(got.Schema.Methods) != 1 || got.Schema.Methods[0].Name != "legacyFallback" {
		t.Fatalf("unexpected schema: %+v", got.Schema)
	}
}
