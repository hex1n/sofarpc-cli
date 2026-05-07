package invoke

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/javamodel"
)

func TestBuildPlan_HappyPathWithSkeletonArgs(t *testing.T) {
	facade := contract.NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.foo.Svc",
			Kind: javamodel.KindInterface,
			Methods: []javamodel.Method{
				{Name: "doThing", ParamTypes: []string{"com.foo.Req"}, ReturnType: "com.foo.Resp"},
			},
		},
		javamodel.Class{
			FQN:  "com.foo.Req",
			Kind: javamodel.KindClass,
			Fields: []javamodel.Field{
				{Name: "id", JavaType: "java.lang.Long"},
			},
		},
	)

	plan, err := BuildPlan(
		Input{
			Service:       "com.foo.Svc",
			Method:        "doThing",
			Version:       "2.0",
			TargetAppName: "demo-app",
			Target:        target.Input{DirectURL: "bolt://host:12200"},
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
	if plan.Version != "2.0" {
		t.Fatalf("version: got %q want 2.0", plan.Version)
	}
	if plan.TargetAppName != "demo-app" {
		t.Fatalf("targetAppName: got %q want demo-app", plan.TargetAppName)
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
		javamodel.Class{
			FQN:  "com.foo.Svc",
			Kind: javamodel.KindInterface,
			Methods: []javamodel.Method{
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

func TestBuildPlan_SingleArgBareValuePassThrough(t *testing.T) {
	facade := contract.NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.foo.Svc",
			Kind: javamodel.KindInterface,
			Methods: []javamodel.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			},
		},
	)
	plan, err := BuildPlan(
		Input{
			Service: "com.foo.Svc",
			Method:  "doThing",
			Args:    "hello",
			Target:  target.Input{DirectURL: "bolt://h:1"},
		},
		facade,
		target.Sources{},
	)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if got := plan.Args[0]; got != "hello" {
		t.Fatalf("user args should pass through, got %v", got)
	}
}

func TestBuildPlan_JSONLookingStringArgStaysString(t *testing.T) {
	facade := contract.NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.foo.Svc",
			Kind: javamodel.KindInterface,
			Methods: []javamodel.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			},
		},
	)
	plan, err := BuildPlan(
		Input{
			Service: "com.foo.Svc",
			Method:  "doThing",
			Args:    `{"id":7}`,
			Target:  target.Input{DirectURL: "bolt://h:1"},
		},
		facade,
		target.Sources{},
	)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if got := plan.Args[0]; got != `{"id":7}` {
		t.Fatalf("string arg should remain literal JSON text, got %#v", got)
	}
}

func TestBuildPlan_StringifiedInlineJSONArrayForSingleListArgIsDecodedAsValue(t *testing.T) {
	facade := contract.NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.foo.Svc",
			Kind: javamodel.KindInterface,
			Methods: []javamodel.Method{
				{Name: "doThing", ParamTypes: []string{"java.util.List<com.foo.Item>"}},
			},
		},
		javamodel.Class{
			FQN:  "com.foo.Item",
			Kind: javamodel.KindClass,
			Fields: []javamodel.Field{
				{Name: "id", JavaType: "java.lang.Long"},
			},
		},
	)
	plan, err := BuildPlan(
		Input{
			Service: "com.foo.Svc",
			Method:  "doThing",
			Args:    `[{"id":7},{"id":8}]`,
			Target:  target.Input{DirectURL: "bolt://h:1"},
		},
		facade,
		target.Sources{},
	)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	items, ok := plan.Args[0].([]any)
	if !ok {
		t.Fatalf("args[0] type: %T", plan.Args[0])
	}
	if len(items) != 2 {
		t.Fatalf("items length: got %d want 2", len(items))
	}
	first := items[0].(map[string]any)
	if first["@type"] != "com.foo.Item" || first["id"] != int64(7) {
		t.Fatalf("first item: %#v", first)
	}
	second := items[1].(map[string]any)
	if second["@type"] != "com.foo.Item" || second["id"] != int64(8) {
		t.Fatalf("second item: %#v", second)
	}
}

func TestBuildPlan_NormalizesFacadeBackedArgs(t *testing.T) {
	facade := contract.NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.foo.Svc",
			Kind: javamodel.KindInterface,
			Methods: []javamodel.Method{
				{Name: "doThing", ParamTypes: []string{"com.foo.Req"}},
			},
		},
		javamodel.Class{
			FQN:  "com.foo.Item",
			Kind: javamodel.KindClass,
			Fields: []javamodel.Field{
				{Name: "id", JavaType: "java.lang.Long"},
			},
		},
		javamodel.Class{
			FQN:  "com.foo.Req",
			Kind: javamodel.KindClass,
			Fields: []javamodel.Field{
				{Name: "amount", JavaType: "java.math.BigDecimal"},
				{Name: "items", JavaType: "java.util.List<com.foo.Item>"},
			},
		},
	)

	plan, err := BuildPlan(
		Input{
			Service: "com.foo.Svc",
			Method:  "doThing",
			Args: []any{
				map[string]any{
					"amount": 12.5,
					"items": []any{
						map[string]any{"id": "7"},
					},
				},
			},
			Target: target.Input{DirectURL: "bolt://h:1"},
		},
		facade,
		target.Sources{},
	)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	arg := plan.Args[0].(map[string]any)
	if got := arg["@type"]; got != "com.foo.Req" {
		t.Fatalf("@type: got %#v", got)
	}
	amount := arg["amount"].(map[string]any)
	if amount["@type"] != "java.math.BigDecimal" || amount["value"] != "12.5" {
		t.Fatalf("amount: %#v", amount)
	}
	items := arg["items"].([]any)
	first := items[0].(map[string]any)
	if first["@type"] != "com.foo.Item" || first["id"] != int64(7) {
		t.Fatalf("first item: %#v", first)
	}
}

func TestBuildPlan_ArgsArityMismatch(t *testing.T) {
	facade := contract.NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.foo.Svc",
			Kind: javamodel.KindInterface,
			Methods: []javamodel.Method{
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
	// Hint must carry the real service/method so the agent can follow
	// it directly — empty values would leave describe unable to run.
	var ecerr *errcode.Error
	if !errors.As(err, &ecerr) {
		t.Fatalf("err is not *errcode.Error: %T", err)
	}
	if ecerr.Hint == nil || ecerr.Hint.NextTool != "sofarpc_describe" {
		t.Fatalf("hint should route to sofarpc_describe, got %+v", ecerr.Hint)
	}
	if svc, _ := ecerr.Hint.NextArgs["service"].(string); svc != "com.foo.Svc" {
		t.Fatalf("hint.NextArgs.service: got %q want com.foo.Svc", svc)
	}
	if m, _ := ecerr.Hint.NextArgs["method"].(string); m != "doThing" {
		t.Fatalf("hint.NextArgs.method: got %q want doThing", m)
	}
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

func TestBuildPlan_TrustedMode_HappyPath(t *testing.T) {
	plan, err := BuildPlan(
		Input{
			Service:    "com.foo.Svc",
			Method:     "doThing",
			ParamTypes: []string{"java.lang.String", "com.foo.Req"},
			Args:       []any{"hello", map[string]any{"@type": "com.foo.Req", "id": 1}},
			Target:     target.Input{DirectURL: "bolt://h:1"},
		},
		nil,
		target.Sources{},
	)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.ContractSource != "trusted" {
		t.Fatalf("contractSource: got %q want trusted", plan.ContractSource)
	}
	if plan.ArgSource != "user" {
		t.Fatalf("argSource: got %q want user", plan.ArgSource)
	}
	if len(plan.ParamTypes) != 2 {
		t.Fatalf("paramTypes arity: got %d want 2", len(plan.ParamTypes))
	}
	if plan.Args[0] != "hello" {
		t.Fatalf("args[0]: got %v want hello", plan.Args[0])
	}
	arg, ok := plan.Args[1].(map[string]any)
	if !ok {
		t.Fatalf("args[1] type: %T", plan.Args[1])
	}
	if _, exists := arg["@type"]; !exists {
		t.Fatalf("trusted mode should preserve explicit payload: %#v", arg)
	}
	if len(plan.Overloads) != 0 {
		t.Fatalf("overloads should be empty in trusted mode, got %d", len(plan.Overloads))
	}
}

func TestBuildPlan_TrustedMode_SingleArgBareValue(t *testing.T) {
	plan, err := BuildPlan(
		Input{
			Service:    "com.foo.Svc",
			Method:     "doThing",
			ParamTypes: []string{"java.lang.String"},
			Args:       "hello",
			Target:     target.Input{DirectURL: "bolt://h:1"},
		},
		nil,
		target.Sources{},
	)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if got := plan.Args[0]; got != "hello" {
		t.Fatalf("args[0]: got %v want hello", got)
	}
}

func TestBuildPlan_TrustedMode_StringifiedJSONObjectArgDecoded(t *testing.T) {
	plan, err := BuildPlan(
		Input{
			Service:    "com.foo.Svc",
			Method:     "doThing",
			ParamTypes: []string{"com.foo.Req"},
			Args:       `{"@type":"com.foo.Req","id":7}`,
			Target:     target.Input{DirectURL: "bolt://h:1"},
		},
		nil,
		target.Sources{},
	)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	arg, ok := plan.Args[0].(map[string]any)
	if !ok {
		t.Fatalf("args[0] type: %T", plan.Args[0])
	}
	if got := arg["@type"]; got != "com.foo.Req" {
		t.Fatalf("@type: got %#v", got)
	}
	if got := arg["id"]; got != json.Number("7") {
		t.Fatalf("id: got %#v want 7", got)
	}
}

func TestBuildPlan_TrustedMode_MissingParamTypes(t *testing.T) {
	_, err := BuildPlan(
		Input{
			Service: "com.foo.Svc",
			Method:  "doThing",
			Args:    []any{"hello"},
			Target:  target.Input{DirectURL: "bolt://h:1"},
		},
		nil,
		target.Sources{},
	)
	assertCode(t, err, errcode.FacadeNotConfigured)
}

func TestBuildPlan_TrustedMode_MissingArgs(t *testing.T) {
	_, err := BuildPlan(
		Input{
			Service:    "com.foo.Svc",
			Method:     "doThing",
			ParamTypes: []string{"java.lang.String"},
			Target:     target.Input{DirectURL: "bolt://h:1"},
		},
		nil,
		target.Sources{},
	)
	assertCode(t, err, errcode.FacadeNotConfigured)
}

func TestBuildPlan_TrustedMode_ArityMismatch(t *testing.T) {
	_, err := BuildPlan(
		Input{
			Service:    "com.foo.Svc",
			Method:     "doThing",
			ParamTypes: []string{"java.lang.String", "java.lang.Long"},
			Args:       []any{"only-one"},
			Target:     target.Input{DirectURL: "bolt://h:1"},
		},
		nil,
		target.Sources{},
	)
	assertCode(t, err, errcode.ArgsInvalid)
	var ecerr *errcode.Error
	if !errors.As(err, &ecerr) {
		t.Fatalf("err is not *errcode.Error: %T", err)
	}
	if ecerr.Hint == nil || ecerr.Hint.NextTool != "sofarpc_describe" {
		t.Fatalf("hint should route to sofarpc_describe, got %+v", ecerr.Hint)
	}
	if svc, _ := ecerr.Hint.NextArgs["service"].(string); svc != "com.foo.Svc" {
		t.Fatalf("hint.NextArgs.service: got %q", svc)
	}
}

func TestBuildPlan_TrustedMode_MissingService(t *testing.T) {
	_, err := BuildPlan(
		Input{
			Method:     "doThing",
			ParamTypes: []string{"java.lang.String"},
			Args:       []any{"hello"},
			Target:     target.Input{DirectURL: "bolt://h:1"},
		},
		nil,
		target.Sources{},
	)
	assertCode(t, err, errcode.ServiceMissing)
}

func TestBuildPlan_TrustedMode_MissingMethod(t *testing.T) {
	_, err := BuildPlan(
		Input{
			Service:    "com.foo.Svc",
			ParamTypes: []string{"java.lang.String"},
			Args:       []any{"hello"},
			Target:     target.Input{DirectURL: "bolt://h:1"},
		},
		nil,
		target.Sources{},
	)
	assertCode(t, err, errcode.MethodMissing)
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
	facade := contract.NewInMemoryStore(javamodel.Class{
		FQN:  "com.foo.Svc",
		Kind: javamodel.KindInterface,
		Methods: []javamodel.Method{
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
