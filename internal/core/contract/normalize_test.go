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

func TestNormalizeArgs_NormalizesEnumFieldInDTO(t *testing.T) {
	store := NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.foo.StatusRequest",
			Kind: javamodel.KindClass,
			Fields: []javamodel.Field{
				{Name: "status", JavaType: "com.foo.Status"},
			},
		},
		javamodel.Class{
			FQN:           "com.foo.Status",
			Kind:          javamodel.KindEnum,
			EnumConstants: []string{"ACTIVE", "DISABLED"},
		},
	)

	args, err := NormalizeArgs([]string{"com.foo.StatusRequest"}, []any{
		map[string]any{"status": "ACTIVE"},
	}, store)
	if err != nil {
		t.Fatalf("NormalizeArgs: %v", err)
	}

	got := args[0].(map[string]any)
	want := map[string]any{
		"@type":  "com.foo.StatusRequest",
		"status": map[string]any{"@type": "com.foo.Status", "name": "ACTIVE"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args[0]: got %#v want %#v", got, want)
	}
}

func TestNormalizeArgs_AllowsEnumLikeStringWhenClassIsMissing(t *testing.T) {
	store := NewInMemoryStore(javamodel.Class{
		FQN:  "com.foo.StatusRequest",
		Kind: javamodel.KindClass,
		Fields: []javamodel.Field{
			{Name: "status", JavaType: "com.foo.Status"},
		},
	})

	args, err := NormalizeArgs([]string{"com.foo.StatusRequest"}, []any{
		map[string]any{"status": "ACTIVE"},
	}, store)
	if err != nil {
		t.Fatalf("NormalizeArgs: %v", err)
	}

	got := args[0].(map[string]any)
	want := map[string]any{
		"@type":  "com.foo.StatusRequest",
		"status": map[string]any{"@type": "com.foo.Status", "name": "ACTIVE"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args[0]: got %#v want %#v", got, want)
	}
}

func TestNormalizeArgs_AllowsTopLevelEnumLikeStringWhenClassIsMissing(t *testing.T) {
	args, err := NormalizeArgs([]string{"com.foo.Status"}, []any{"ACTIVE"}, NewInMemoryStore())
	if err != nil {
		t.Fatalf("NormalizeArgs: %v", err)
	}
	want := []any{map[string]any{"@type": "com.foo.Status", "name": "ACTIVE"}}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args: got %#v want %#v", args, want)
	}
}

func TestNormalizeArgs_NormalizesEnumContainers(t *testing.T) {
	store := NewInMemoryStore(javamodel.Class{
		FQN:           "com.foo.Status",
		Kind:          javamodel.KindEnum,
		EnumConstants: []string{"ACTIVE", "DISABLED"},
	})

	args, err := NormalizeArgs(
		[]string{
			"java.util.List<com.foo.Status>",
			"java.util.Map<java.lang.String, com.foo.Status>",
			"com.foo.Status[]",
		},
		[]any{
			[]any{"ACTIVE"},
			map[string]any{"current": "DISABLED"},
			[]any{"ACTIVE", "DISABLED"},
		},
		store,
	)
	if err != nil {
		t.Fatalf("NormalizeArgs: %v", err)
	}

	active := map[string]any{"@type": "com.foo.Status", "name": "ACTIVE"}
	disabled := map[string]any{"@type": "com.foo.Status", "name": "DISABLED"}
	want := []any{
		[]any{active},
		map[string]any{"current": disabled},
		[]any{active, disabled},
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args: got %#v want %#v", args, want)
	}
}

func TestNormalizeArgs_NormalizesEnumDiscoveredBySuperclass(t *testing.T) {
	store := NewInMemoryStore(javamodel.Class{
		FQN:        "com.foo.Status",
		Kind:       javamodel.KindClass,
		Superclass: "java.lang.Enum<com.foo.Status>",
	})

	args, err := NormalizeArgs([]string{"com.foo.Status"}, []any{"ACTIVE"}, store)
	if err != nil {
		t.Fatalf("NormalizeArgs: %v", err)
	}
	want := []any{map[string]any{"@type": "com.foo.Status", "name": "ACTIVE"}}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args: got %#v want %#v", args, want)
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

func TestNormalizeArgs_RejectsUnassignableExplicitAtType(t *testing.T) {
	store := NewInMemoryStore(
		javamodel.Class{FQN: "com.foo.Base", Kind: javamodel.KindClass},
		javamodel.Class{FQN: "com.foo.Other", Kind: javamodel.KindClass},
	)

	_, err := NormalizeArgs([]string{"com.foo.Base"}, []any{
		map[string]any{"@type": "com.foo.Other"},
	}, store)
	if err == nil {
		t.Fatal("expected explicit @type mismatch to fail")
	}
}

func TestNormalizeArgs_AllowsAssignableExplicitAtType(t *testing.T) {
	store := NewInMemoryStore(
		javamodel.Class{FQN: "com.foo.Base", Kind: javamodel.KindClass},
		javamodel.Class{
			FQN:        "com.foo.Child",
			Kind:       javamodel.KindClass,
			Superclass: "com.foo.Base",
			Fields:     []javamodel.Field{{Name: "id", JavaType: "java.lang.Long"}},
		},
	)

	args, err := NormalizeArgs([]string{"com.foo.Base"}, []any{
		map[string]any{"@type": "com.foo.Child", "id": "9"},
	}, store)
	if err != nil {
		t.Fatalf("NormalizeArgs: %v", err)
	}
	child := args[0].(map[string]any)
	if child["@type"] != "com.foo.Child" || child["id"] != int64(9) {
		t.Fatalf("child: %#v", child)
	}
}

func TestNormalizeArgs_AllowsAssignableParameterizedInterfaceAtType(t *testing.T) {
	store := NewInMemoryStore(
		javamodel.Class{
			FQN:  "com.foo.Base",
			Kind: javamodel.KindInterface,
		},
		javamodel.Class{
			FQN:        "com.foo.Child",
			Kind:       javamodel.KindClass,
			Interfaces: []string{"com.foo.Base<com.foo.Foo>"},
		},
	)

	args, err := NormalizeArgs([]string{"com.foo.Base<com.foo.Foo>"}, []any{
		map[string]any{"@type": "com.foo.Child"},
	}, store)
	if err != nil {
		t.Fatalf("NormalizeArgs: %v", err)
	}
	child := args[0].(map[string]any)
	if child["@type"] != "com.foo.Child" {
		t.Fatalf("child: %#v", child)
	}
}

func TestNormalizeArgs_RejectsWrongExplicitDecimalAtType(t *testing.T) {
	_, err := NormalizeArgs([]string{"java.math.BigDecimal"}, []any{
		map[string]any{"@type": "java.math.BigInteger", "value": "10"},
	}, NewInMemoryStore())
	if err == nil {
		t.Fatal("expected explicit decimal @type mismatch to fail")
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
			// Enum values become the canonical GenericObject shape SOFA's
			// GenericUtils can realize into a Java enum constant. We do not
			// validate the constant name; that is the server's job.
			name:       "enum_string_to_generic_object",
			paramTypes: []string{"com.foo.Mood"},
			args:       []any{"HAPPY"},
			want:       []any{map[string]any{"@type": "com.foo.Mood", "name": "HAPPY"}},
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
