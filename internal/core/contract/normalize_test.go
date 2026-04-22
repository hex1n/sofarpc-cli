package contract

import (
	"encoding/json"
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

// TestNormalizeArgs_Golden exercises the most common shape-coercion
// paths as a single table so regressions surface as a focused diff. Each
// row pins a specific contract behavior (generic erasure, @type
// override, nested arrays, ...) — use the subtest name when triaging.
func TestNormalizeArgs_Golden(t *testing.T) {
	// Shared store covers every row. NewInMemoryStore only fills in
	// classes the row actually references — unused types stay absent
	// so we also exercise the unknown-type fallthrough paths.
	store := NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.foo.Key",
			Kind: javamodel.KindClass,
			Fields: []javamodel.Field{
				{Name: "id", JavaType: "java.lang.Long"},
				{Name: "tag", JavaType: "java.lang.String"},
			},
		},
		javamodel.Class{
			FQN:  "com.foo.Leaf",
			Kind: javamodel.KindClass,
			Fields: []javamodel.Field{
				{Name: "name", JavaType: "java.lang.String"},
			},
		},
		javamodel.Class{
			FQN:  "com.foo.Mid",
			Kind: javamodel.KindClass,
			Fields: []javamodel.Field{
				{Name: "leaves", JavaType: "java.util.List<com.foo.Leaf>"},
			},
		},
		javamodel.Class{
			FQN:        "com.foo.Sub",
			Kind:       javamodel.KindClass,
			Superclass: "com.foo.Mid",
			Fields: []javamodel.Field{
				{Name: "label", JavaType: "java.lang.String"},
			},
		},
		javamodel.Class{
			FQN:           "com.foo.Mood",
			Kind:          javamodel.KindEnum,
			EnumConstants: []string{"HAPPY", "SAD"},
		},
	)

	cases := []struct {
		name       string
		paramTypes []string
		args       []any
		want       []any
	}{
		{
			// Generic erasure: the declared paramType is raw java.util.List
			// but the agent sends a structured element. Loose normalisation
			// keeps the element as-is because there is no element spec.
			name:       "raw_list_preserved",
			paramTypes: []string{"java.util.List"},
			args: []any{
				[]any{map[string]any{"whatever": "goes"}},
			},
			want: []any{
				[]any{map[string]any{"whatever": "goes"}},
			},
		},
		{
			// @type on the input overrides the declared paramType. The
			// declared type is still the fallback for field lookup, but
			// the emitted @type tracks what the agent asserted.
			name:       "atype_overrides_declared",
			paramTypes: []string{"com.foo.Mid"},
			args: []any{
				map[string]any{
					"@type": "com.foo.Sub",
					"label": "hi",
					"leaves": []any{
						map[string]any{"name": "one"},
					},
				},
			},
			want: []any{
				map[string]any{
					"@type": "com.foo.Sub",
					"label": "hi",
					"leaves": []any{
						map[string]any{"@type": "com.foo.Leaf", "name": "one"},
					},
				},
			},
		},
		{
			// Map<String, ComplexKey>: keys stay as provided; values get
			// the nested-object treatment (@type tag + recursive field
			// normalisation).
			name:       "map_with_complex_values",
			paramTypes: []string{"java.util.Map<java.lang.String, com.foo.Key>"},
			args: []any{
				map[string]any{
					"primary":   map[string]any{"id": "10", "tag": "p"},
					"secondary": map[string]any{"id": 20.0, "tag": "s"},
				},
			},
			want: []any{
				map[string]any{
					"primary":   map[string]any{"@type": "com.foo.Key", "id": int64(10), "tag": "p"},
					"secondary": map[string]any{"@type": "com.foo.Key", "id": int64(20), "tag": "s"},
				},
			},
		},
		{
			// Nested arrays: String[][] materialises as []any of []any,
			// preserving the outer and inner ordering.
			name:       "nested_string_arrays",
			paramTypes: []string{"java.lang.String[][]"},
			args: []any{
				[]any{
					[]any{"a", "b"},
					[]any{"c"},
				},
			},
			want: []any{
				[]any{
					[]any{"a", "b"},
					[]any{"c"},
				},
			},
		},
		{
			// Explicit nil inputs propagate through without triggering
			// the object-shape guard.
			name:       "null_field_preserved",
			paramTypes: []string{"com.foo.Leaf"},
			args: []any{
				map[string]any{"name": nil},
			},
			want: []any{
				map[string]any{"@type": "com.foo.Leaf", "name": nil},
			},
		},
		{
			// json.Number is the decoded shape when the MCP client uses
			// UseNumber; we still narrow to int64 for java.lang.Long.
			name:       "json_number_to_long",
			paramTypes: []string{"java.lang.Long"},
			args:       []any{json.Number("12345")},
			want:       []any{int64(12345)},
		},
		{
			// Enum values stay as strings — normalisation does not try
			// to validate the constant name; that is the server's job.
			name:       "enum_string_passthrough",
			paramTypes: []string{"com.foo.Mood"},
			args:       []any{"HAPPY"},
			want:       []any{"HAPPY"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeArgs(tc.paramTypes, tc.args, store)
			if err != nil {
				t.Fatalf("NormalizeArgs: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("mismatch\n got:  %#v\nwant: %#v", got, tc.want)
			}
		})
	}
}
