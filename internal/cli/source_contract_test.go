package cli

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/contract"
	"github.com/hex1n/sofarpc-cli/internal/model"
)

func TestApplyProjectMethodContractSetsGenericAndClearsStubPaths(t *testing.T) {
	originalResolve := resolveProjectMethodContract
	originalResolveArtifact := resolveArtifactMethodContract
	originalCompile := compileProjectMethodArgs
	t.Cleanup(func() {
		resolveProjectMethodContract = originalResolve
		resolveArtifactMethodContract = originalResolveArtifact
		compileProjectMethodArgs = originalCompile
	})

	resolveArtifactMethodContract = func(projectRoot, service, method string, preferredParamTypes []string, rawArgs json.RawMessage) (contract.ProjectMethod, error) {
		t.Fatal("artifact fallback should not run when project contract succeeds")
		return contract.ProjectMethod{}, nil
	}
	resolveProjectMethodContract = func(projectRoot, service, method string, preferredParamTypes []string, rawArgs json.RawMessage) (contract.ProjectMethod, error) {
		if !reflect.DeepEqual(preferredParamTypes, []string{"com.example.OrderRequest"}) {
			t.Fatalf("preferredParamTypes = %v", preferredParamTypes)
		}
		if string(rawArgs) != `{"id":1}` {
			t.Fatalf("rawArgs = %s", rawArgs)
		}
		return contract.ProjectMethod{
			Schema: model.MethodSchema{
				Name:                "importAsset",
				ParamTypes:          []string{"com.example.OrderRequest"},
				ParamTypeSignatures: []string{"com.example.OrderRequest"},
			},
		}, nil
	}
	compileProjectMethodArgs = func(raw json.RawMessage, method contract.ProjectMethod) (json.RawMessage, error) {
		if string(raw) != `[{"id":1}]` {
			t.Fatalf("wrapped raw args = %s", raw)
		}
		return json.RawMessage(`[{"@type":"com.example.OrderRequest","id":1}]`), nil
	}

	app := &App{Cwd: t.TempDir()}
	resolved := resolvedInvocation{
		ManifestPath: "sofarpc.manifest.json",
		Request: model.InvocationRequest{
			Service:             "com.example.OrderFacade",
			Method:              "importAsset",
			ParamTypes:          []string{"com.example.OrderRequest"},
			Args:                json.RawMessage(`{"id":1}`),
			PayloadMode:         model.PayloadRaw,
			ParamTypeSignatures: nil,
		},
		StubPaths: []string{"a.jar", "b.jar"},
	}

	source, cacheHit, notes, err := app.applyProjectMethodContract(context.Background(), &resolved, false)
	if err != nil {
		t.Fatalf("applyProjectMethodContract() error = %v", err)
	}
	if source != "project-source" {
		t.Fatalf("applyProjectMethodContract() source = %q", source)
	}
	if cacheHit {
		t.Fatal("cacheHit should be false without metadata daemon")
	}
	if len(notes) != 0 {
		t.Fatalf("notes = %v, want none", notes)
	}
	if resolved.Request.PayloadMode != model.PayloadGeneric {
		t.Fatalf("PayloadMode = %q", resolved.Request.PayloadMode)
	}
	if len(resolved.StubPaths) != 0 {
		t.Fatalf("StubPaths = %v", resolved.StubPaths)
	}
	if string(resolved.Request.Args) != `[{"@type":"com.example.OrderRequest","id":1}]` {
		t.Fatalf("Args = %s", resolved.Request.Args)
	}
	if !reflect.DeepEqual(resolved.Request.ParamTypeSignatures, []string{"com.example.OrderRequest"}) {
		t.Fatalf("ParamTypeSignatures = %v", resolved.Request.ParamTypeSignatures)
	}
}

func TestApplyProjectMethodContractFallsBackToArtifacts(t *testing.T) {
	originalResolve := resolveProjectMethodContract
	originalResolveArtifact := resolveArtifactMethodContract
	originalCompile := compileProjectMethodArgs
	t.Cleanup(func() {
		resolveProjectMethodContract = originalResolve
		resolveArtifactMethodContract = originalResolveArtifact
		compileProjectMethodArgs = originalCompile
	})

	resolveProjectMethodContract = func(projectRoot, service, method string, preferredParamTypes []string, rawArgs json.RawMessage) (contract.ProjectMethod, error) {
		return contract.ProjectMethod{}, errors.New("source miss")
	}
	resolveArtifactMethodContract = func(projectRoot, service, method string, preferredParamTypes []string, rawArgs json.RawMessage) (contract.ProjectMethod, error) {
		return contract.ProjectMethod{
			Schema: model.MethodSchema{
				Name:                "importAsset",
				ParamTypes:          []string{"com.example.OrderRequest"},
				ParamTypeSignatures: []string{"com.example.OrderRequest"},
			},
		}, nil
	}
	compileProjectMethodArgs = func(raw json.RawMessage, method contract.ProjectMethod) (json.RawMessage, error) {
		return json.RawMessage(`[{"@type":"com.example.OrderRequest"}]`), nil
	}

	app := &App{Cwd: t.TempDir()}
	resolved := resolvedInvocation{
		Request: model.InvocationRequest{
			Service:     "com.example.OrderFacade",
			Method:      "importAsset",
			ParamTypes:  []string{"com.example.OrderRequest"},
			Args:        json.RawMessage(`[{}]`),
			PayloadMode: model.PayloadRaw,
		},
		StubPaths: []string{"api.jar"},
	}

	source, cacheHit, notes, err := app.applyProjectMethodContract(context.Background(), &resolved, false)
	if err != nil {
		t.Fatalf("applyProjectMethodContract() error = %v", err)
	}
	if source != "jar-javap" {
		t.Fatalf("source = %q", source)
	}
	if cacheHit {
		t.Fatal("cacheHit should be false without metadata daemon")
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "project-source: source miss") {
		t.Fatalf("notes = %v", notes)
	}
	if resolved.Request.PayloadMode != model.PayloadGeneric || len(resolved.StubPaths) != 0 {
		t.Fatalf("resolved = %+v", resolved)
	}
}
