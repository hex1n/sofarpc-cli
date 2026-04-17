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
	t.Cleanup(func() {
		describeServiceFromProject = original
		describeServiceFromArtifacts = originalArtifacts
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
	t.Cleanup(func() {
		describeServiceFromProject = original
		describeServiceFromArtifacts = originalArtifacts
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
	t.Cleanup(func() {
		describeServiceFromProject = original
		describeServiceFromArtifacts = originalArtifacts
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
