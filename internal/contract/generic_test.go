package contract

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/facadekit"
)

func TestCompileProjectMethodArgsAddsNestedObjectTypes(t *testing.T) {
	method := ProjectMethod{
		ServiceInfo: facadekit.SemanticClassInfo{
			FQN: "com.example.OrderFacade",
			Methods: []facadekit.SemanticMethodInfo{
				{
					Name: "importAsset",
					Parameters: []facadekit.SemanticParameterInfo{
						{Name: "request", Type: "com.example.OrderRequest"},
					},
				},
			},
		},
		MethodInfo: facadekit.SemanticMethodInfo{
			Name: "importAsset",
			Parameters: []facadekit.SemanticParameterInfo{
				{Name: "request", Type: "com.example.OrderRequest"},
			},
		},
		Registry: facadekit.Registry{
			"com.example.OrderRequest": {
				FQN:        "com.example.OrderRequest",
				SimpleName: "OrderRequest",
				Kind:       "class",
				Fields: []facadekit.SemanticFieldInfo{
					{Name: "items", JavaType: "java.util.List<com.example.OrderItem>"},
					{Name: "meta", JavaType: "java.util.Map<java.lang.String, com.example.OrderMeta>"},
					{Name: "child", JavaType: "com.example.ChildRequest"},
					{Name: "status", JavaType: "com.example.Status"},
				},
			},
			"com.example.OrderItem": {
				FQN:        "com.example.OrderItem",
				SimpleName: "OrderItem",
				Kind:       "class",
				Fields:     []facadekit.SemanticFieldInfo{{Name: "code", JavaType: "java.lang.String"}},
			},
			"com.example.OrderMeta": {
				FQN:        "com.example.OrderMeta",
				SimpleName: "OrderMeta",
				Kind:       "class",
				Fields:     []facadekit.SemanticFieldInfo{{Name: "memo", JavaType: "java.lang.String"}},
			},
			"com.example.ChildRequest": {
				FQN:        "com.example.ChildRequest",
				SimpleName: "ChildRequest",
				Kind:       "class",
				Fields:     []facadekit.SemanticFieldInfo{{Name: "name", JavaType: "java.lang.String"}},
			},
			"com.example.Status": {
				FQN:           "com.example.Status",
				SimpleName:    "Status",
				Kind:          "enum",
				EnumConstants: []string{"ACTIVE", "INACTIVE"},
			},
		},
	}

	raw := json.RawMessage(`[{"items":[{"code":"A1"}],"meta":{"m1":{"memo":"hello"}},"child":{"name":"kid"},"status":"ACTIVE"}]`)
	compiled, err := CompileProjectMethodArgs(raw, method)
	if err != nil {
		t.Fatalf("CompileProjectMethodArgs() error = %v", err)
	}

	var got []map[string]any
	if err := json.Unmarshal(compiled, &got); err != nil {
		t.Fatalf("Unmarshal compiled: %v", err)
	}
	want := []map[string]any{
		{
			"@type": "com.example.OrderRequest",
			"items": []any{
				map[string]any{
					"@type": "com.example.OrderItem",
					"code":  "A1",
				},
			},
			"meta": map[string]any{
				"m1": map[string]any{
					"@type": "com.example.OrderMeta",
					"memo":  "hello",
				},
			},
			"child": map[string]any{
				"@type": "com.example.ChildRequest",
				"name":  "kid",
			},
			"status": "ACTIVE",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("compiled = %#v, want %#v", got, want)
	}
}
