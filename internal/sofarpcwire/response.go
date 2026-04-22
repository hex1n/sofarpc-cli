package sofarpcwire

import "fmt"

// response.go holds the Sofa-layer adapters that sit on top of the
// Hessian decoder. They interpret a generic decoded value (either a
// Sofa response envelope or a java throwable) and flatten it into the
// DecodedResponse shape the invoke pipeline consumes.

func decodeSofaResponse(content []byte) (DecodedResponse, error) {
	decoder := hessianDecoder{data: content}
	value, err := decoder.readValue()
	if err != nil {
		return DecodedResponse{}, fmt.Errorf("decode SofaResponse: %w", err)
	}

	fields, typeName, ok := typedObject(value)
	if !ok {
		return DecodedResponse{}, fmt.Errorf("unexpected response type %T", value)
	}
	if typeName != ResponseClass {
		return decodeRemoteException(typeName, fields), nil
	}

	resp := DecodedResponse{
		AppResponse:   fields["appResponse"],
		ResponseProps: toStringMap(fields["responseProps"]),
	}
	if isError, ok := fields["isError"].(bool); ok {
		resp.IsError = isError
	}
	if errorMsg, ok := fields["errorMsg"].(string); ok {
		resp.ErrorMsg = errorMsg
	}
	return resp, nil
}

func typedObject(value any) (map[string]any, string, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, "", false
	}
	gotType, _ := object["type"].(string)
	fields, ok := object["fields"].(map[string]any)
	if !ok {
		return nil, gotType, false
	}
	return fields, gotType, true
}

func decodeRemoteException(typeName string, fields map[string]any) DecodedResponse {
	resp := DecodedResponse{
		IsError:     true,
		AppResponse: map[string]any{"type": typeName, "fields": fields},
	}
	resp.ErrorMsg = firstNonEmptyString(
		stringField(fields, "message"),
		stringField(fields, "errorMsg"),
		stringField(fields, "msg"),
		stringField(fields, "detailMessage"),
		stringField(fields, "localizedMessage"),
	)
	if resp.ErrorMsg == "" {
		if causeType := nestedTypeField(fields, "cause"); causeType != "" {
			resp.ErrorMsg = typeName + ": cause=" + causeType
		} else {
			resp.ErrorMsg = typeName
		}
	}
	if stack, ok := fields["stackTrace"]; ok {
		resp.ResponseProps = map[string]string{
			"remoteExceptionType": typeName,
			"stackTrace":          summarizeValue(stack),
		}
		return resp
	}
	resp.ResponseProps = map[string]string{
		"remoteExceptionType": typeName,
	}
	return resp
}

func stringField(fields map[string]any, key string) string {
	if fields == nil {
		return ""
	}
	raw, ok := fields[key]
	if !ok || raw == nil {
		return ""
	}
	switch typed := raw.(type) {
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case int64:
		return fmt.Sprintf("%d", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case float64:
		return fmt.Sprintf("%v", typed)
	default:
		return ""
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func nestedTypeField(fields map[string]any, key string) string {
	if fields == nil {
		return ""
	}
	raw, ok := fields[key]
	if !ok {
		return ""
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	typeName, _ := obj["type"].(string)
	return typeName
}

func summarizeValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []any:
		return fmt.Sprintf("list(len=%d)", len(typed))
	case map[string]any:
		if typeName, _ := typed["type"].(string); typeName != "" {
			return "object(" + typeName + ")"
		}
		if entries, ok := typed["entries"].(map[string]any); ok {
			return fmt.Sprintf("map(len=%d)", len(entries))
		}
		return fmt.Sprintf("map(len=%d)", len(typed))
	default:
		return fmt.Sprintf("%T", value)
	}
}

func toStringMap(value any) map[string]string {
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]string:
		if len(typed) == 0 {
			return nil
		}
		out := make(map[string]string, len(typed))
		for key, item := range typed {
			out[key] = item
		}
		return out
	case map[string]any:
		if entries, ok := typed["entries"].(map[string]any); ok {
			return toStringMap(entries)
		}
		if len(typed) == 0 {
			return nil
		}
		out := make(map[string]string, len(typed))
		for key, item := range typed {
			out[key] = fmt.Sprint(item)
		}
		return out
	default:
		return map[string]string{
			"value": fmt.Sprint(value),
		}
	}
}
