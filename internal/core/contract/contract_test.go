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

func TestBuildSkeleton_BigDecimalUsesTypedObject(t *testing.T) {
	sk := BuildSkeleton([]string{"java.math.BigDecimal"}, NewInMemoryStore())
	if string(sk[0]) != `{"@type":"java.math.BigDecimal","value":"0"}` {
		t.Fatalf("BigDecimal skeleton: %s", sk[0])
	}
}

func TestBuildSkeleton_ContainerWithoutTypeArgsFallsBackToEmpty(t *testing.T) {
	// Erasure can strip type parameters; without them the agent has no
	// way to know the element type, so an empty placeholder is the only
	// honest answer.
	sk := BuildSkeleton([]string{"java.util.List", "java.util.Map"}, NewInMemoryStore())
	if string(sk[0]) != `[]` {
		t.Fatalf("bare List: %s", sk[0])
	}
	if string(sk[1]) != `{}` {
		t.Fatalf("bare Map: %s", sk[1])
	}
}

func TestBuildSkeleton_ListExpandsElementType(t *testing.T) {
	store := NewInMemoryStore(facadesemantic.Class{
		FQN:  "com.foo.Order",
		Kind: facadesemantic.KindClass,
		Fields: []facadesemantic.Field{
			{Name: "id", JavaType: "java.lang.Long"},
		},
	})
	sk := BuildSkeleton([]string{"java.util.List<com.foo.Order>"}, store)
	// Outer is an array with exactly one element — the element must be
	// the Order skeleton with @type so Hessian2 can pick the class.
	var arr []any
	if err := json.Unmarshal(sk[0], &arr); err != nil {
		t.Fatalf("not a JSON array: %s — %v", sk[0], err)
	}
	if len(arr) != 1 {
		t.Fatalf("expected one sample element; got %v", arr)
	}
	elem, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("element should be an object; got %T", arr[0])
	}
	if elem["@type"] != "com.foo.Order" {
		t.Fatalf("element @type should be com.foo.Order; got %v", elem)
	}
	if _, has := elem["id"]; !has {
		t.Fatalf("element should include id field; got %v", elem)
	}
}

func TestBuildSkeleton_MapExpandsValueType(t *testing.T) {
	store := NewInMemoryStore(facadesemantic.Class{
		FQN:  "com.foo.Bar",
		Kind: facadesemantic.KindClass,
		Fields: []facadesemantic.Field{
			{Name: "name", JavaType: "java.lang.String"},
		},
	})
	sk := BuildSkeleton([]string{"java.util.Map<java.lang.String, com.foo.Bar>"}, store)
	var obj map[string]any
	if err := json.Unmarshal(sk[0], &obj); err != nil {
		t.Fatalf("not a JSON object: %s — %v", sk[0], err)
	}
	val, has := obj["<key>"]
	if !has {
		t.Fatalf("map skeleton should use <key> as placeholder; got %v", obj)
	}
	entry, ok := val.(map[string]any)
	if !ok {
		t.Fatalf("map value should be Bar object; got %T", val)
	}
	if entry["@type"] != "com.foo.Bar" {
		t.Fatalf("map value @type should be com.foo.Bar; got %v", entry)
	}
}

func TestBuildSkeleton_NestedGenericsRoundTrip(t *testing.T) {
	// Map<String, List<Foo>> — nested generic with a user type inside.
	// The outer comma must not split the inner List<Foo>, and the inner
	// List element must be the Foo skeleton.
	store := NewInMemoryStore(facadesemantic.Class{
		FQN:  "com.foo.Foo",
		Kind: facadesemantic.KindClass,
		Fields: []facadesemantic.Field{
			{Name: "label", JavaType: "java.lang.String"},
		},
	})
	sk := BuildSkeleton([]string{"java.util.Map<java.lang.String, java.util.List<com.foo.Foo>>"}, store)
	var obj map[string]any
	if err := json.Unmarshal(sk[0], &obj); err != nil {
		t.Fatalf("not JSON: %s — %v", sk[0], err)
	}
	inner, ok := obj["<key>"].([]any)
	if !ok {
		t.Fatalf("nested list missing; got %T", obj["<key>"])
	}
	if len(inner) != 1 {
		t.Fatalf("nested list should carry one sample; got %v", inner)
	}
	elem, ok := inner[0].(map[string]any)
	if !ok {
		t.Fatalf("inner element should be Foo object; got %T", inner[0])
	}
	if elem["@type"] != "com.foo.Foo" {
		t.Fatalf("inner @type should be com.foo.Foo; got %v", elem)
	}
}

func TestBuildSkeleton_ArrayWrapsElement(t *testing.T) {
	sk := BuildSkeleton([]string{"java.lang.String[]"}, NewInMemoryStore())
	if string(sk[0]) != `[""]` {
		t.Fatalf("String[] should render as array of String placeholder; got %s", sk[0])
	}
}

func TestBuildSkeleton_WildcardResolvesToBound(t *testing.T) {
	store := NewInMemoryStore(facadesemantic.Class{
		FQN:  "com.foo.Item",
		Kind: facadesemantic.KindClass,
		Fields: []facadesemantic.Field{
			{Name: "sku", JavaType: "java.lang.String"},
		},
	})
	// `List<? extends Item>` is a consumer — agent reads Items out. We
	// still render the Item skeleton so the agent sees the expected
	// shape.
	sk := BuildSkeleton([]string{"java.util.List<? extends com.foo.Item>"}, store)
	var arr []any
	if err := json.Unmarshal(sk[0], &arr); err != nil {
		t.Fatalf("not JSON: %s", sk[0])
	}
	if len(arr) != 1 {
		t.Fatalf("expected one element; got %v", arr)
	}
	elem := arr[0].(map[string]any)
	if elem["@type"] != "com.foo.Item" {
		t.Fatalf("wildcard should resolve to Item; got %v", elem)
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

func TestBuildSkeleton_IncludesInheritedFields(t *testing.T) {
	store := NewInMemoryStore(
		facadesemantic.Class{
			FQN:    "com.foo.Base",
			Kind:   facadesemantic.KindClass,
			Fields: []facadesemantic.Field{{Name: "id", JavaType: "java.lang.Long"}},
		},
		facadesemantic.Class{
			FQN:        "com.foo.Child",
			Kind:       facadesemantic.KindClass,
			Superclass: "com.foo.Base",
			Fields:     []facadesemantic.Field{{Name: "name", JavaType: "java.lang.String"}},
		},
	)

	sk := BuildSkeleton([]string{"com.foo.Child"}, store)
	var obj map[string]any
	if err := json.Unmarshal(sk[0], &obj); err != nil {
		t.Fatalf("skeleton not valid json: %v — %s", err, sk[0])
	}
	if _, ok := obj["id"]; !ok {
		t.Fatalf("inherited id field missing: %v", obj)
	}
	if _, ok := obj["name"]; !ok {
		t.Fatalf("child name field missing: %v", obj)
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
