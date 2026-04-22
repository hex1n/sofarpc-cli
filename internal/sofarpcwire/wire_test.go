package sofarpcwire

import (
	"bytes"
	"testing"
)

func TestTargetServiceUniqueName(t *testing.T) {
	t.Parallel()

	if got := TargetServiceUniqueName("com.example.DemoFacade", "", ""); got != "com.example.DemoFacade:1.0" {
		t.Fatalf("TargetServiceUniqueName() = %q", got)
	}
	if got := TargetServiceUniqueName("com.example.DemoFacade", "2.0", "gray"); got != "com.example.DemoFacade:2.0:gray" {
		t.Fatalf("TargetServiceUniqueName() = %q", got)
	}
}

func TestNormalizeArgsPromotesTypedObjectsAndSlices(t *testing.T) {
	t.Parallel()

	args := NormalizeArgs([]any{
		map[string]any{
			"@type":      "com.example.Demo",
			"value":      int64(7),
			"longValues": []any{int64(1), int64(2)},
		},
	})

	arg, ok := args[0].(javaTypedObject)
	if !ok {
		t.Fatalf("arg type = %T", args[0])
	}
	if got := arg.typeName; got != "com.example.Demo" {
		t.Fatalf("typeName = %v", got)
	}

	values, ok := arg.fields["longValues"].(*javaArrayList)
	if !ok {
		t.Fatalf("longValues type = %T", arg.fields["longValues"])
	}
	rawValues := values.Get()
	if len(rawValues) != 2 || rawValues[0] != int64(1) || rawValues[1] != int64(2) {
		t.Fatalf("longValues = %#v", rawValues)
	}
}

func TestNormalizeArgs_PromotesBigDecimalTypedObject(t *testing.T) {
	t.Parallel()

	args := NormalizeArgs([]any{
		map[string]any{
			"@type": "java.math.BigDecimal",
			"value": "1000.5",
		},
	})

	arg, ok := args[0].(javaTypedObject)
	if !ok {
		t.Fatalf("arg type = %T", args[0])
	}
	if arg.typeName != "java.math.BigDecimal" {
		t.Fatalf("typeName = %q", arg.typeName)
	}
	if got := arg.fields["value"]; got != "1000.5" {
		t.Fatalf("value = %#v", got)
	}
}

func TestBuildGenericRequestEncodesExpectedStrings(t *testing.T) {
	t.Parallel()

	req, err := BuildGenericRequest(RequestSpec{
		Service:    "com.example.demo.ExampleFacade",
		Method:     "query",
		ParamTypes: []string{"com.example.demo.ExampleRequest"},
		Args: []any{
			map[string]any{
				"@type": "com.example.demo.ExampleRequest",
				"id":    int64(1001),
				"items": []any{int64(1001)},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildGenericRequest() error = %v", err)
	}
	if len(req.Content) == 0 {
		t.Fatal("content should not be empty")
	}
	if req.Content[0] != 'O' {
		t.Fatalf("content should start with custom object definition tag 'O', got 0x%02x", req.Content[0])
	}
	for _, needle := range [][]byte{
		[]byte(RequestClass),
		[]byte("com.example.demo.ExampleFacade:1.0"),
		[]byte("com.example.demo.ExampleRequest"),
		[]byte("java.util.ArrayList"),
	} {
		if !bytes.Contains(req.Content, needle) {
			t.Fatalf("content missing %q", needle)
		}
	}
}

func TestBuildGenericRequest_EncodesBigDecimalTypedObject(t *testing.T) {
	t.Parallel()

	req, err := BuildGenericRequest(RequestSpec{
		Service:    "com.example.Facade",
		Method:     "doThing",
		ParamTypes: []string{"com.example.Request"},
		Args: []any{
			map[string]any{
				"@type": "com.example.Request",
				"amount": map[string]any{
					"@type": "java.math.BigDecimal",
					"value": "1000.5",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildGenericRequest() error = %v", err)
	}
	if !bytes.Contains(req.Content, []byte("java.math.BigDecimal")) {
		t.Fatal("content should contain java.math.BigDecimal typed object")
	}
	if bytes.Contains(req.Content, []byte("@type")) {
		t.Fatal("typed objects should be encoded as Hessian objects, not literal @type map entries")
	}
}

func TestBuildGenericRequest_NonKnownArgsStillUseCustomEncoder(t *testing.T) {
	t.Parallel()

	req, err := BuildGenericRequest(RequestSpec{
		Service:    "com.example.demo.ExampleFacade",
		Method:     "query",
		ParamTypes: []string{"com.example.demo.ExampleRequest"},
		Args: []any{
			map[string]any{
				"@type": "com.example.demo.ExampleRequest",
				"id":    int64(1001),
				"items": []any{int64(1001)},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildGenericRequest() error = %v", err)
	}
	if len(req.Content) == 0 {
		t.Fatal("content should not be empty")
	}
	if req.Content[0] != 'O' {
		t.Fatalf("content should start with custom object definition tag 'O', got 0x%02x", req.Content[0])
	}
	if !bytes.Contains(req.Content, []byte("query")) {
		t.Fatal("content should contain method name")
	}
}

// TestDecodeResponse_RoundTripsSuccessEnvelope encodes a synthesized
// SofaResponse via BuildSuccessResponse and asserts DecodeResponse
// flattens it back into the expected typed-object tree. The fixture
// is built at runtime so there is no golden hex to drift and no wire
// bytes captured from a specific production service.
func TestDecodeResponse_RoundTripsSuccessEnvelope(t *testing.T) {
	t.Parallel()

	appResponse := NormalizeArgs([]any{
		map[string]any{
			"@type":   "com.example.demo.Result",
			"success": true,
			"message": "ok",
			"data": map[string]any{
				"@type": "com.example.demo.ExampleResponse",
				"items": []any{
					map[string]any{
						"@type": "com.example.demo.ExampleItem",
						"id":    int64(1001),
						"code":  "ALPHA",
					},
				},
			},
		},
	})[0]

	content, err := BuildSuccessResponse(appResponse)
	if err != nil {
		t.Fatalf("BuildSuccessResponse() error = %v", err)
	}

	resp, err := DecodeResponse(content)
	if err != nil {
		t.Fatalf("DecodeResponse() error = %v", err)
	}
	if resp.IsError {
		t.Fatalf("resp.IsError = true")
	}
	if resp.ErrorMsg != "" {
		t.Fatalf("resp.ErrorMsg = %q", resp.ErrorMsg)
	}

	app, ok := resp.AppResponse.(map[string]any)
	if !ok {
		t.Fatalf("AppResponse type = %T", resp.AppResponse)
	}
	if got := app["type"]; got != "com.example.demo.Result" {
		t.Fatalf("app.type = %v", got)
	}

	appFields, ok := app["fields"].(map[string]any)
	if !ok {
		t.Fatalf("app.fields type = %T", app["fields"])
	}
	if got, ok := appFields["success"].(bool); !ok || !got {
		t.Fatalf("app.success = %#v", appFields["success"])
	}
	if got := appFields["message"]; got != "ok" {
		t.Fatalf("app.message = %#v", got)
	}

	data, ok := appFields["data"].(map[string]any)
	if !ok {
		t.Fatalf("data type = %T", appFields["data"])
	}
	dataFields, ok := data["fields"].(map[string]any)
	if !ok {
		t.Fatalf("data.fields type = %T", data["fields"])
	}
	itemsWrapper, ok := dataFields["items"].(map[string]any)
	if !ok {
		t.Fatalf("items type = %T", dataFields["items"])
	}
	items, ok := itemsWrapper["items"].([]any)
	if !ok {
		t.Fatalf("items.items type = %T", itemsWrapper["items"])
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d", len(items))
	}

	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("item type = %T", items[0])
	}
	itemFields, ok := item["fields"].(map[string]any)
	if !ok {
		t.Fatalf("item.fields type = %T", item["fields"])
	}
	if got := itemFields["id"]; got != int64(1001) {
		t.Fatalf("item.id = %#v", got)
	}
	if got := itemFields["code"]; got != "ALPHA" {
		t.Fatalf("item.code = %#v", got)
	}
}
