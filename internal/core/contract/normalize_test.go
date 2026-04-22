package contract

import (
	"reflect"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/javamodel"
)

func TestNormalizeArgs_AutoWrapsBigDecimalAndTypedObject(t *testing.T) {
	store := NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.foo.Req",
			Kind: javamodel.KindClass,
			Fields: []javamodel.Field{
				{Name: "amount", JavaType: "java.math.BigDecimal"},
			},
		},
	)

	args, err := NormalizeArgs([]string{"com.foo.Req"}, []any{
		map[string]any{"amount": 1000.5},
	}, store)
	if err != nil {
		t.Fatalf("NormalizeArgs: %v", err)
	}

	obj, ok := args[0].(map[string]any)
	if !ok {
		t.Fatalf("arg type: %T", args[0])
	}
	if got := obj["@type"]; got != "com.foo.Req" {
		t.Fatalf("@type: got %#v", got)
	}
	amount, ok := obj["amount"].(map[string]any)
	if !ok {
		t.Fatalf("amount type: %T", obj["amount"])
	}
	if !reflect.DeepEqual(amount, map[string]any{"@type": "java.math.BigDecimal", "value": "1000.5"}) {
		t.Fatalf("amount: %#v", amount)
	}
}

func TestNormalizeArgs_NormalizesNestedDTOAndListOfDTO(t *testing.T) {
	store := NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.foo.Child",
			Kind: javamodel.KindClass,
			Fields: []javamodel.Field{
				{Name: "id", JavaType: "java.lang.Long"},
			},
		},
		javamodel.Class{
			FQN:  "com.foo.Parent",
			Kind: javamodel.KindClass,
			Fields: []javamodel.Field{
				{Name: "child", JavaType: "com.foo.Child"},
				{Name: "children", JavaType: "java.util.List<com.foo.Child>"},
			},
		},
	)

	args, err := NormalizeArgs([]string{"com.foo.Parent"}, []any{
		map[string]any{
			"child": map[string]any{"id": "42"},
			"children": []any{
				map[string]any{"id": 7.0},
			},
		},
	}, store)
	if err != nil {
		t.Fatalf("NormalizeArgs: %v", err)
	}

	parent := args[0].(map[string]any)
	child := parent["child"].(map[string]any)
	if child["@type"] != "com.foo.Child" {
		t.Fatalf("child @type: %#v", child["@type"])
	}
	if child["id"] != int64(42) {
		t.Fatalf("child.id: %#v", child["id"])
	}

	children := parent["children"].([]any)
	first := children[0].(map[string]any)
	if first["@type"] != "com.foo.Child" {
		t.Fatalf("children[0] @type: %#v", first["@type"])
	}
	if first["id"] != int64(7) {
		t.Fatalf("children[0].id: %#v", first["id"])
	}
}

func TestNormalizeArgs_MapValueNormalization(t *testing.T) {
	store := NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.foo.Entry",
			Kind: javamodel.KindClass,
			Fields: []javamodel.Field{
				{Name: "name", JavaType: "java.lang.String"},
			},
		},
	)

	args, err := NormalizeArgs([]string{"java.util.Map<java.lang.String, com.foo.Entry>"}, []any{
		map[string]any{
			"a": map[string]any{"name": "x"},
		},
	}, store)
	if err != nil {
		t.Fatalf("NormalizeArgs: %v", err)
	}

	values := args[0].(map[string]any)
	entry := values["a"].(map[string]any)
	if entry["@type"] != "com.foo.Entry" {
		t.Fatalf("entry @type: %#v", entry["@type"])
	}
}

func TestNormalizeArgs_InheritedFieldsAreVisible(t *testing.T) {
	store := NewInMemoryStore(
		javamodel.Class{
			FQN:    "com.foo.Base",
			Kind:   javamodel.KindClass,
			Fields: []javamodel.Field{{Name: "id", JavaType: "java.lang.Long"}},
		},
		javamodel.Class{
			FQN:        "com.foo.Child",
			Kind:       javamodel.KindClass,
			Superclass: "com.foo.Base",
			Fields:     []javamodel.Field{{Name: "name", JavaType: "java.lang.String"}},
		},
	)

	args, err := NormalizeArgs([]string{"com.foo.Child"}, []any{
		map[string]any{"id": "9", "name": "demo"},
	}, store)
	if err != nil {
		t.Fatalf("NormalizeArgs: %v", err)
	}

	child := args[0].(map[string]any)
	if child["id"] != int64(9) {
		t.Fatalf("id: %#v", child["id"])
	}
}

func TestNormalizeArgs_ObjectShapeMismatchFails(t *testing.T) {
	store := NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.foo.Req",
			Kind: javamodel.KindClass,
		},
	)

	_, err := NormalizeArgs([]string{"com.foo.Req"}, []any{"not-an-object"}, store)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNormalizeArgs_CollectionShapeMismatchFails(t *testing.T) {
	_, err := NormalizeArgs([]string{"java.util.List<java.lang.String>"}, []any{
		map[string]any{"oops": true},
	}, NewInMemoryStore())
	if err == nil {
		t.Fatal("expected error")
	}
}
