package sofarpcwire

import (
	"bytes"
	"encoding/hex"
	"testing"
)

const knownPortfolioAvailableCashResponseHex = "4fbe636f6d2e616c697061792e736f66612e7270632e636f72652e726573706f6e73652e536f6661526573706f6e7365940769734572726f72086572726f724d73670b617070526573706f6e73650d726573706f6e736550726f70736f90464e4fc833636f6d2e6578616d706c652e736572766963656170702e6661636164652e6d6f64656c2e4f7065726174696f6e526573756c7496077375636365737304636f6465076d6573736167650974696d657374616d700464617461086d657461646174616f9154e007737563636573734c0000019dae7234ef4fc847636f6d2e6578616d706c652e736572766963656170702e6661636164652e6d6f64656c2e726573706f6e73652e73616c65732e4461696c79486f6c64696e67526573706f6e736591116461696c79486f6c64696e67496e666f736f92566e014fc843636f6d2e6578616d706c652e736572766963656170702e6661636164652e6d6f64656c2e726573706f6e73652e73616c65732e4461696c79486f6c64696e67496e666f94066d70436f64650866756e64436f64650b686f6c64696e67446174650f686f6c64696e675175616e746974796f934c06066c852f02200004434153480832303236303431344fa46a6176612e6d6174682e426967446563696d616c910576616c75656f9406302e303030307a4d74001e6a6176612e7574696c2e436f6c6c656374696f6e7324456d7074794d61707a4e"

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

	values, ok := arg.fields["longValues"].(*javaArraysArrayList)
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

func TestBuildGenericRequestMatchesJavaContent(t *testing.T) {
	t.Parallel()

	req, err := BuildGenericRequest(RequestSpec{
		Service:    "com.example.serviceapp.facade.sales.holdings.SalesDailyHoldingsFacade",
		Method:     "queryPortfolioAvailableCash",
		ParamTypes: []string{"com.example.serviceapp.facade.model.request.DailyHoldingsQueryRequest"},
		Args: []any{
			map[string]any{
				"@type":      "com.example.serviceapp.facade.model.request.DailyHoldingsQueryRequest",
				"tradeDate":  "20260414",
				"mpCode":     int64(434153733362950144),
				"mpCodeList": []any{int64(434153733362950144)},
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
		[]byte("com.example.serviceapp.facade.sales.holdings.SalesDailyHoldingsFacade:1.0"),
		[]byte("com.example.serviceapp.facade.model.request.DailyHoldingsQueryRequest"),
		[]byte("java.util.Arrays$ArrayList"),
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
		Service:    "com.example.serviceapp.facade.sales.holdings.SalesDailyHoldingsFacade",
		Method:     "queryPortfolioAvailableCash",
		ParamTypes: []string{"com.example.serviceapp.facade.model.request.DailyHoldingsQueryRequest"},
		Args: []any{
			map[string]any{
				"@type":      "com.example.serviceapp.facade.model.request.DailyHoldingsQueryRequest",
				"tradeDate":  "20260414",
				"mpCode":     int64(1001),
				"mpCodeList": []any{int64(1001)},
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
	if !bytes.Contains(req.Content, []byte("queryPortfolioAvailableCash")) {
		t.Fatal("content should contain method name")
	}
}

func TestDecodeResponseMatchesKnownSuccessPayload(t *testing.T) {
	t.Parallel()

	content, err := hex.DecodeString(knownPortfolioAvailableCashResponseHex)
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
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
	if got := app["type"]; got != "com.example.serviceapp.facade.model.OperationResult" {
		t.Fatalf("app.type = %v", got)
	}

	appFields, ok := app["fields"].(map[string]any)
	if !ok {
		t.Fatalf("app.fields type = %T", app["fields"])
	}
	if got, ok := appFields["success"].(bool); !ok || !got {
		t.Fatalf("app.success = %#v", appFields["success"])
	}
	if got := appFields["message"]; got != "success" {
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
	items, ok := dataFields["dailyHoldingInfos"].([]any)
	if !ok {
		t.Fatalf("dailyHoldingInfos type = %T", dataFields["dailyHoldingInfos"])
	}
	if len(items) != 1 {
		t.Fatalf("len(dailyHoldingInfos) = %d", len(items))
	}

	holding, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("holding type = %T", items[0])
	}
	holdingFields, ok := holding["fields"].(map[string]any)
	if !ok {
		t.Fatalf("holding.fields type = %T", holding["fields"])
	}
	if got := holdingFields["mpCode"]; got != int64(434153733362950144) {
		t.Fatalf("holding.mpCode = %#v", got)
	}
	if got := holdingFields["fundCode"]; got != "CASH" {
		t.Fatalf("holding.fundCode = %#v", got)
	}
}
