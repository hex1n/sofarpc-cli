package invoke

import (
	"errors"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
)

func TestBuildPlan_HappyPathWithSkeletonArgs(t *testing.T) {
	facade := contract.NewInMemoryStore(
		facadesemantic.Class{
			FQN:  "com.foo.Svc",
			Kind: facadesemantic.KindInterface,
			Methods: []facadesemantic.Method{
				{Name: "doThing", ParamTypes: []string{"com.foo.Req"}, ReturnType: "com.foo.Resp"},
			},
		},
		facadesemantic.Class{
			FQN:  "com.foo.Req",
			Kind: facadesemantic.KindClass,
			Fields: []facadesemantic.Field{
				{Name: "id", JavaType: "java.lang.Long"},
			},
		},
	)

	plan, err := BuildPlan(
		Input{
			Service: "com.foo.Svc",
			Method:  "doThing",
			Target:  target.Input{DirectURL: "bolt://host:12200"},
		},
		facade,
		target.Sources{},
	)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.Target.Mode != target.ModeDirect {
		t.Fatalf("target.mode: got %q", plan.Target.Mode)
	}
	if plan.ReturnType != "com.foo.Resp" {
		t.Fatalf("returnType: got %q", plan.ReturnType)
	}
	if plan.ArgSource != "skeleton" {
		t.Fatalf("argSource: got %q want skeleton", plan.ArgSource)
	}
	if len(plan.Args) != 1 {
		t.Fatalf("args arity: got %d", len(plan.Args))
	}
	arg, ok := plan.Args[0].(map[string]any)
	if !ok {
		t.Fatalf("skeleton arg should be an object, got %T", plan.Args[0])
	}
	if arg["@type"] != "com.foo.Req" {
		t.Fatalf("skeleton should inject @type, got %v", arg)
	}
}

func TestBuildPlan_UserArgsPassThrough(t *testing.T) {
	facade := contract.NewInMemoryStore(
		facadesemantic.Class{
			FQN:  "com.foo.Svc",
			Kind: facadesemantic.KindInterface,
			Methods: []facadesemantic.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			},
		},
	)
	plan, err := BuildPlan(
		Input{
			Service: "com.foo.Svc",
			Method:  "doThing",
			Args:    []any{"hello"},
			Target:  target.Input{DirectURL: "bolt://h:1"},
		},
		facade,
		target.Sources{},
	)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.ArgSource != "user" {
		t.Fatalf("argSource: got %q want user", plan.ArgSource)
	}
	if plan.Args[0] != "hello" {
		t.Fatalf("user args should pass through, got %v", plan.Args[0])
	}
}

func TestBuildPlan_ArgsArityMismatch(t *testing.T) {
	facade := contract.NewInMemoryStore(
		facadesemantic.Class{
			FQN:  "com.foo.Svc",
			Kind: facadesemantic.KindInterface,
			Methods: []facadesemantic.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String", "java.lang.Long"}},
			},
		},
	)
	_, err := BuildPlan(
		Input{
			Service: "com.foo.Svc",
			Method:  "doThing",
			Args:    []any{"only-one"},
			Target:  target.Input{DirectURL: "bolt://h:1"},
		},
		facade,
		target.Sources{},
	)
	assertCode(t, err, errcode.ArgsInvalid)
}

func TestBuildPlan_TargetMissing(t *testing.T) {
	facade := contract.NewInMemoryStore()
	_, err := BuildPlan(
		Input{Service: "com.foo.Svc", Method: "doThing"},
		facade,
		target.Sources{},
	)
	assertCode(t, err, errcode.TargetMissing)
}

func TestBuildPlan_FacadeNil(t *testing.T) {
	_, err := BuildPlan(
		Input{
			Service: "com.foo.Svc",
			Method:  "doThing",
			Target:  target.Input{DirectURL: "bolt://h:1"},
		},
		nil,
		target.Sources{},
	)
	assertCode(t, err, errcode.FacadeNotConfigured)
}

func TestBuildPlan_PropagatesContractErrors(t *testing.T) {
	facade := contract.NewInMemoryStore()
	_, err := BuildPlan(
		Input{
			Service: "com.foo.Missing",
			Method:  "doThing",
			Target:  target.Input{DirectURL: "bolt://h:1"},
		},
		facade,
		target.Sources{},
	)
	assertCode(t, err, errcode.ContractUnresolvable)
}

func TestBuildPlan_IncludesLayersAndOverloads(t *testing.T) {
	facade := contract.NewInMemoryStore(facadesemantic.Class{
		FQN:  "com.foo.Svc",
		Kind: facadesemantic.KindInterface,
		Methods: []facadesemantic.Method{
			{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			{Name: "doThing", ParamTypes: []string{"java.lang.Long"}},
		},
	})
	plan, err := BuildPlan(
		Input{
			Service:    "com.foo.Svc",
			Method:     "doThing",
			ParamTypes: []string{"java.lang.Long"},
			Target:     target.Input{DirectURL: "bolt://h:1"},
		},
		facade,
		target.Sources{},
	)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(plan.Overloads) != 2 {
		t.Fatalf("overloads: got %d want 2", len(plan.Overloads))
	}
	if len(plan.TargetLayers) == 0 {
		t.Fatal("targetLayers should be populated")
	}
}

func assertCode(t *testing.T, err error, want errcode.Code) {
	t.Helper()
	var ecerr *errcode.Error
	if !errors.As(err, &ecerr) {
		t.Fatalf("expected *errcode.Error, got %T: %v", err, err)
	}
	if ecerr.Code != want {
		t.Fatalf("code: got %q want %q", ecerr.Code, want)
	}
}
