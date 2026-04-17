package facadeschema

import (
	"strings"
	"testing"
)

func TestBuildMethodSchemaExpandsInheritedDTOs(t *testing.T) {
	registry := fixtureRegistry()

	got, err := BuildMethodSchema(registry, "com.example.UserFacade", "getUser", nil, []string{"必传", "required"})
	if err != nil {
		t.Fatalf("BuildMethodSchema error = %v", err)
	}
	if got.Service != "com.example.UserFacade" {
		t.Fatalf("Service = %q", got.Service)
	}
	if got.File != "src/main/java/com/example/UserFacade.java" {
		t.Fatalf("File = %q", got.File)
	}
	if got.Method.Name != "getUser" {
		t.Fatalf("Method.Name = %q", got.Method.Name)
	}
	if len(got.Method.ParamTypes) != 1 || got.Method.ParamTypes[0] != "com.example.UserRequest" {
		t.Fatalf("ParamTypes = %v", got.Method.ParamTypes)
	}
	if len(got.Method.ParamsSkeleton) != 1 {
		t.Fatalf("ParamsSkeleton len = %d", len(got.Method.ParamsSkeleton))
	}
	request, ok := got.Method.ParamsSkeleton[0].(map[string]interface{})
	if !ok {
		t.Fatalf("ParamsSkeleton[0] type = %T", got.Method.ParamsSkeleton[0])
	}
	if request["tenantId"] != "" {
		t.Fatalf("tenantId skeleton = %#v", request["tenantId"])
	}
	if request["status"] != "ACTIVE" {
		t.Fatalf("status skeleton = %#v", request["status"])
	}
	items, ok := request["items"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("items skeleton = %#v", request["items"])
	}
	item, ok := items[0].(map[string]interface{})
	if !ok || item["code"] != "" {
		t.Fatalf("item skeleton = %#v", items[0])
	}
	if len(got.Method.ParamsFieldInfo) != 1 {
		t.Fatalf("ParamsFieldInfo len = %d", len(got.Method.ParamsFieldInfo))
	}
	param := got.Method.ParamsFieldInfo[0]
	if param.RequiredHint != "必传 用户请求" {
		t.Fatalf("RequiredHint = %q", param.RequiredHint)
	}
	if len(param.Fields) != 3 {
		t.Fatalf("Fields len = %d", len(param.Fields))
	}
	if !fieldRequired(param.Fields, "tenantId") {
		t.Fatalf("tenantId should be required: %+v", param.Fields)
	}
	if got.Method.ResponseWarning == "" || !strings.Contains(got.Method.ResponseWarningReason, "Optional getter") {
		t.Fatalf("response warning = %#v / %#v", got.Method.ResponseWarning, got.Method.ResponseWarningReason)
	}
}

func TestBuildMethodSchemaRejectsOverloadedMethodWithoutTypes(t *testing.T) {
	registry := Registry{
		"com.example.OverloadedFacade": {
			FQN:        "com.example.OverloadedFacade",
			SimpleName: "OverloadedFacade",
			Kind:       "interface",
			Methods: []SemanticMethodInfo{
				{Name: "put", Parameters: []SemanticParameterInfo{{Name: "value", Type: "java.lang.String"}}},
				{Name: "put", Parameters: []SemanticParameterInfo{{Name: "value", Type: "java.lang.Long"}}},
			},
		},
	}

	_, err := BuildMethodSchema(registry, "com.example.OverloadedFacade", "put", nil, []string{"必传"})
	if err == nil {
		t.Fatal("BuildMethodSchema error = nil, want overload error")
	}
	if !strings.Contains(err.Error(), "overloaded") {
		t.Fatalf("BuildMethodSchema error = %v", err)
	}
}

func TestBuildMethodSchemaUsesPreferredParamTypes(t *testing.T) {
	registry := Registry{
		"com.example.ImportFacade": {
			FQN:        "com.example.ImportFacade",
			SimpleName: "ImportFacade",
			Kind:       "interface",
			Methods: []SemanticMethodInfo{
				{
					Name:       "put",
					ReturnType: "void",
					Parameters: []SemanticParameterInfo{{Name: "request", Type: "com.example.BaseRequest"}},
				},
				{
					Name:       "put",
					ReturnType: "void",
					Parameters: []SemanticParameterInfo{{Name: "request", Type: "com.example.ImportRequest"}},
				},
			},
		},
		"com.example.BaseRequest": {
			FQN:        "com.example.BaseRequest",
			SimpleName: "BaseRequest",
			Kind:       "class",
			Fields:     []SemanticFieldInfo{{Name: "tenantId", JavaType: "java.lang.String"}},
		},
		"com.example.ImportRequest": {
			FQN:        "com.example.ImportRequest",
			SimpleName: "ImportRequest",
			Kind:       "class",
			Fields:     []SemanticFieldInfo{{Name: "batchNo", JavaType: "java.lang.String"}},
		},
	}

	got, err := BuildMethodSchema(registry, "com.example.ImportFacade", "put", []string{"com.example.ImportRequest"}, []string{"必传"})
	if err != nil {
		t.Fatalf("BuildMethodSchema error = %v", err)
	}
	if len(got.Method.ParamTypes) != 1 || got.Method.ParamTypes[0] != "com.example.ImportRequest" {
		t.Fatalf("ParamTypes = %v", got.Method.ParamTypes)
	}
	if len(got.Method.ParamsFieldInfo) != 1 || len(got.Method.ParamsFieldInfo[0].Fields) != 1 {
		t.Fatalf("ParamsFieldInfo = %+v", got.Method.ParamsFieldInfo)
	}
	if got.Method.ParamsFieldInfo[0].Fields[0].Name != "batchNo" {
		t.Fatalf("Selected fields = %+v", got.Method.ParamsFieldInfo[0].Fields)
	}
}

func fieldRequired(fields []FieldSchema, name string) bool {
	for _, field := range fields {
		if field.Name == name {
			return field.Required
		}
	}
	return false
}

func fixtureRegistry() Registry {
	return Registry{
		"com.example.UserFacade": {
			FQN:        "com.example.UserFacade",
			SimpleName: "UserFacade",
			File:       "src/main/java/com/example/UserFacade.java",
			Kind:       "interface",
			Methods: []SemanticMethodInfo{
				{
					Name:       "getUser",
					Javadoc:    "@param request\n\t\t必传 用户请求",
					ReturnType: "com.example.ResponseEnvelope",
					Parameters: []SemanticParameterInfo{{Name: "request", Type: "com.example.UserRequest"}},
				},
			},
		},
		"com.example.BaseRequest": {
			FQN:        "com.example.BaseRequest",
			SimpleName: "BaseRequest",
			Kind:       "class",
			Fields: []SemanticFieldInfo{
				{Name: "tenantId", JavaType: "java.lang.String", Comment: "tenant id 必传", Required: true},
			},
		},
		"com.example.UserRequest": {
			FQN:        "com.example.UserRequest",
			SimpleName: "UserRequest",
			Kind:       "class",
			Superclass: "com.example.BaseRequest",
			Fields: []SemanticFieldInfo{
				{Name: "items", JavaType: "java.util.List<com.example.Item>"},
				{Name: "status", JavaType: "com.example.Status"},
			},
		},
		"com.example.Item": {
			FQN:        "com.example.Item",
			SimpleName: "Item",
			Kind:       "class",
			Fields: []SemanticFieldInfo{
				{Name: "code", JavaType: "java.lang.String"},
			},
		},
		"com.example.Status": {
			FQN:           "com.example.Status",
			SimpleName:    "Status",
			Kind:          "enum",
			EnumConstants: []string{"ACTIVE", "INACTIVE"},
		},
		"com.example.ResponseEnvelope": {
			FQN:           "com.example.ResponseEnvelope",
			SimpleName:    "ResponseEnvelope",
			Kind:          "class",
			MethodReturns: []string{"java.util.Optional<java.lang.String>"},
		},
	}
}
