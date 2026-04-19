package contract

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
)

func TestResolveMethod_RequiresService(t *testing.T) {
	store := NewInMemoryStore()
	_, err := ResolveMethod(store, "", "do", nil)
	assertCode(t, err, errcode.ServiceMissing)
}

func TestResolveMethod_RequiresMethod(t *testing.T) {
	store := NewInMemoryStore()
	_, err := ResolveMethod(store, "com.foo.Svc", "", nil)
	assertCode(t, err, errcode.MethodMissing)
}

func TestResolveMethod_ServiceNotInStore(t *testing.T) {
	store := NewInMemoryStore()
	_, err := ResolveMethod(store, "com.foo.Svc", "doThing", nil)
	assertCode(t, err, errcode.ContractUnresolvable)
}

func TestResolveMethod_MethodNotFound(t *testing.T) {
	store := NewInMemoryStore(facadesemantic.Class{
		FQN:     "com.foo.Svc",
		Kind:    facadesemantic.KindInterface,
		Methods: []facadesemantic.Method{{Name: "other"}},
	})
	_, err := ResolveMethod(store, "com.foo.Svc", "doThing", nil)
	assertCode(t, err, errcode.MethodNotFound)
}

func TestResolveMethod_SingleOverloadReturnsDirectly(t *testing.T) {
	store := NewInMemoryStore(facadesemantic.Class{
		FQN:  "com.foo.Svc",
		Kind: facadesemantic.KindInterface,
		Methods: []facadesemantic.Method{
			{Name: "doThing", ParamTypes: []string{"java.lang.String"}, ReturnType: "java.lang.String"},
		},
	})
	res, err := ResolveMethod(store, "com.foo.Svc", "doThing", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Selected != 0 {
		t.Fatalf("selected: got %d want 0", res.Selected)
	}
	if res.Method.ReturnType != "java.lang.String" {
		t.Fatalf("returnType: got %q", res.Method.ReturnType)
	}
}

func TestResolveMethod_AmbiguousWithoutParamTypes(t *testing.T) {
	store := NewInMemoryStore(facadesemantic.Class{
		FQN:  "com.foo.Svc",
		Kind: facadesemantic.KindInterface,
		Methods: []facadesemantic.Method{
			{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			{Name: "doThing", ParamTypes: []string{"java.lang.String", "java.lang.Integer"}},
		},
	})
	_, err := ResolveMethod(store, "com.foo.Svc", "doThing", nil)
	assertCode(t, err, errcode.MethodAmbiguous)
}

func TestResolveMethod_DisambiguatesByParamTypes(t *testing.T) {
	store := NewInMemoryStore(facadesemantic.Class{
		FQN:  "com.foo.Svc",
		Kind: facadesemantic.KindInterface,
		Methods: []facadesemantic.Method{
			{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			{Name: "doThing", ParamTypes: []string{"java.lang.String", "java.lang.Integer"}},
		},
	})
	res, err := ResolveMethod(store, "com.foo.Svc", "doThing", []string{"java.lang.String", "java.lang.Integer"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Method.ParamTypes) != 2 {
		t.Fatalf("selected overload should have 2 params, got %v", res.Method.ParamTypes)
	}
}

func TestResolveMethod_ParamTypesDoNotMatch(t *testing.T) {
	store := NewInMemoryStore(facadesemantic.Class{
		FQN:  "com.foo.Svc",
		Kind: facadesemantic.KindInterface,
		Methods: []facadesemantic.Method{
			{Name: "doThing", ParamTypes: []string{"java.lang.String"}},
			{Name: "doThing", ParamTypes: []string{"java.lang.Long"}},
		},
	})
	_, err := ResolveMethod(store, "com.foo.Svc", "doThing", []string{"java.lang.Integer"})
	assertCode(t, err, errcode.MethodNotFound)
}

func TestBuildSkeleton_PrimitivesAndStrings(t *testing.T) {
	sk := BuildSkeleton([]string{"java.lang.String", "java.lang.Long", "boolean"}, NewInMemoryStore())
	if string(sk[0]) != `""` {
		t.Fatalf("String skeleton: %s", sk[0])
	}
	if string(sk[1]) != `0` {
		t.Fatalf("Long skeleton: %s", sk[1])
	}
	if string(sk[2]) != `false` {
		t.Fatalf("boolean skeleton: %s", sk[2])
	}
}

func TestBuildSkeleton_ContainerRendersEmpty(t *testing.T) {
	sk := BuildSkeleton([]string{"java.util.List<com.foo.Order>", "java.util.Map<java.lang.String,java.lang.Long>"}, NewInMemoryStore())
	if string(sk[0]) != `[]` {
		t.Fatalf("List skeleton: %s", sk[0])
	}
	if string(sk[1]) != `{}` {
		t.Fatalf("Map skeleton: %s", sk[1])
	}
}

func TestBuildSkeleton_UserTypeInjectsAtType(t *testing.T) {
	store := NewInMemoryStore(facadesemantic.Class{
		FQN:        "com.foo.Order",
		SimpleName: "Order",
		Kind:       facadesemantic.KindClass,
		Fields: []facadesemantic.Field{
			{Name: "id", JavaType: "java.lang.Long", Required: true},
			{Name: "sku", JavaType: "java.lang.String"},
		},
	})
	sk := BuildSkeleton([]string{"com.foo.Order"}, store)
	var obj map[string]any
	if err := json.Unmarshal(sk[0], &obj); err != nil {
		t.Fatalf("skeleton not valid json: %v — %s", err, sk[0])
	}
	if obj["@type"] != "com.foo.Order" {
		t.Fatalf("@type missing or wrong: %v", obj)
	}
	if _, ok := obj["id"]; !ok {
		t.Fatal("id field missing from skeleton")
	}
	// @type must be first — keeps the output readable.
	if !strings.HasPrefix(string(sk[0]), `{"@type":`) {
		t.Fatalf("@type should be first key; got %s", sk[0])
	}
}

func TestBuildSkeleton_EnumUsesFirstConstant(t *testing.T) {
	store := NewInMemoryStore(facadesemantic.Class{
		FQN:           "com.foo.Status",
		Kind:          facadesemantic.KindEnum,
		EnumConstants: []string{"ACTIVE", "INACTIVE"},
	})
	sk := BuildSkeleton([]string{"com.foo.Status"}, store)
	if string(sk[0]) != `"ACTIVE"` {
		t.Fatalf("enum skeleton: %s", sk[0])
	}
}

func TestBuildSkeleton_RecursiveUserTypeTerminatesOnCycle(t *testing.T) {
	store := NewInMemoryStore(
		facadesemantic.Class{
			FQN:    "com.foo.Node",
			Kind:   facadesemantic.KindClass,
			Fields: []facadesemantic.Field{{Name: "parent", JavaType: "com.foo.Node"}},
		},
	)
	sk := BuildSkeleton([]string{"com.foo.Node"}, store)
	// The cycle guard kicks in on the second visit to com.foo.Node —
	// parent should collapse to the minimal stub.
	if !strings.Contains(string(sk[0]), `"parent":{"@type":"com.foo.Node"}`) {
		t.Fatalf("cycle guard did not fire: %s", sk[0])
	}
}

func TestBuildSkeleton_UnknownUserTypeEmitsStub(t *testing.T) {
	sk := BuildSkeleton([]string{"com.foo.UnknownDTO"}, NewInMemoryStore())
	if string(sk[0]) != `{"@type":"com.foo.UnknownDTO"}` {
		t.Fatalf("unknown user type should emit stub; got %s", sk[0])
	}
}

func assertCode(t *testing.T, err error, want errcode.Code) {
	t.Helper()
	var ecerr *errcode.Error
	if !errors.As(err, &ecerr) {
		t.Fatalf("error should be *errcode.Error, got %T: %v", err, err)
	}
	if ecerr.Code != want {
		t.Fatalf("code: got %q want %q", ecerr.Code, want)
	}
	if ecerr.Hint == nil {
		t.Fatalf("hint should be populated for code %q", ecerr.Code)
	}
}
