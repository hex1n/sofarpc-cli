package sofarpcwire

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	goldenKindRequestContent  = "request-content"
	goldenKindResponseContent = "response-content"
)

type goldenWireFixture struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Kind        string            `json:"kind"`
	ContentHex  string            `json:"contentHex"`
	Want        goldenFixtureWant `json:"want"`
}

type goldenFixtureWant struct {
	IsError             *bool             `json:"isError,omitempty"`
	ErrorMsg            string            `json:"errorMsg,omitempty"`
	AppResponseType     string            `json:"appResponseType,omitempty"`
	AppResponseJSON     json.RawMessage   `json:"appResponseJson,omitempty"`
	ResponseProps       map[string]string `json:"responseProps,omitempty"`
	RemoteExceptionType string            `json:"remoteExceptionType,omitempty"`
	Service             string            `json:"service,omitempty"`
	Method              string            `json:"method,omitempty"`
	ParamTypes          []string          `json:"paramTypes,omitempty"`
	TargetServiceUnique string            `json:"targetServiceUniqueName,omitempty"`
	ArgsJSON            json.RawMessage   `json:"argsJson,omitempty"`
}

func TestJavaGoldenWireFixtures(t *testing.T) {
	goldenDir := goldenFixtureDir()
	paths, err := filepath.Glob(filepath.Join(goldenDir, "*.json"))
	if err != nil {
		t.Fatalf("glob golden fixtures: %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("no Java SOFARPC golden fixtures in %s; add request-content and response-content fixtures", goldenDir)
	}

	var sawRequest bool
	var sawResponse bool
	for _, path := range paths {
		path := path
		fixture := loadGoldenFixture(t, path)
		switch fixture.Kind {
		case goldenKindRequestContent:
			sawRequest = true
		case goldenKindResponseContent:
			sawResponse = true
		default:
			t.Fatalf("%s: unknown fixture kind %q", path, fixture.Kind)
		}

		t.Run(filepath.Base(path), func(t *testing.T) {
			validateGoldenFixture(t, fixture)
			content, err := hex.DecodeString(fixture.ContentHex)
			if err != nil {
				t.Fatalf("decode contentHex: %v", err)
			}
			if len(content) == 0 {
				t.Fatal("contentHex decoded to empty content")
			}
			switch fixture.Kind {
			case goldenKindRequestContent:
				assertGoldenRequestFixture(t, fixture.Want)
			case goldenKindResponseContent:
				resp, err := DecodeResponse(content)
				if err != nil {
					t.Fatalf("DecodeResponse: %v", err)
				}
				assertGoldenResponse(t, fixture.Want, resp)
			}
		})
	}
	if !sawRequest {
		t.Fatal("missing request-content golden fixture")
	}
	if !sawResponse {
		t.Fatal("missing response-content golden fixture")
	}
}

func goldenFixtureDir() string {
	if dir := strings.TrimSpace(os.Getenv("SOFARPCWIRE_GOLDEN_DIR")); dir != "" {
		return dir
	}
	return filepath.Join("testdata", "golden")
}

func loadGoldenFixture(t *testing.T, path string) goldenWireFixture {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture goldenWireFixture
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&fixture); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != nil {
		if err != io.EOF {
			t.Fatalf("parse fixture trailing data: %v", err)
		}
	}
	return fixture
}

func validateGoldenFixture(t *testing.T, fixture goldenWireFixture) {
	t.Helper()
	if strings.TrimSpace(fixture.Name) == "" {
		t.Fatal("name is required")
	}
	if strings.TrimSpace(fixture.ContentHex) == "" {
		t.Fatal("contentHex is required")
	}
}

func assertGoldenResponse(t *testing.T, want goldenFixtureWant, got DecodedResponse) {
	t.Helper()
	if !hasGoldenResponseExpectation(want) {
		t.Fatal("response-content fixture must include at least one response expectation in want")
	}
	if want.IsError != nil && got.IsError != *want.IsError {
		t.Fatalf("IsError = %v, want %v", got.IsError, *want.IsError)
	}
	if want.ErrorMsg != "" && got.ErrorMsg != want.ErrorMsg {
		t.Fatalf("ErrorMsg = %q, want %q", got.ErrorMsg, want.ErrorMsg)
	}
	if want.AppResponseType != "" {
		fields, typeName, ok := typedObject(got.AppResponse)
		if !ok {
			t.Fatalf("AppResponse is not a typed object: %T", got.AppResponse)
		}
		_ = fields
		if typeName != want.AppResponseType {
			t.Fatalf("AppResponse type = %q, want %q", typeName, want.AppResponseType)
		}
	}
	if len(bytes.TrimSpace(want.AppResponseJSON)) > 0 {
		assertGoldenAppResponseJSON(t, want.AppResponseJSON, got.AppResponse)
	}
	for key, value := range want.ResponseProps {
		if got.ResponseProps[key] != value {
			t.Fatalf("ResponseProps[%q] = %q, want %q", key, got.ResponseProps[key], value)
		}
	}
	if want.RemoteExceptionType != "" {
		if got.ResponseProps["remoteExceptionType"] != want.RemoteExceptionType {
			t.Fatalf("remoteExceptionType = %q, want %q", got.ResponseProps["remoteExceptionType"], want.RemoteExceptionType)
		}
	}
}

func hasGoldenResponseExpectation(want goldenFixtureWant) bool {
	return want.IsError != nil ||
		want.ErrorMsg != "" ||
		want.AppResponseType != "" ||
		len(bytes.TrimSpace(want.AppResponseJSON)) > 0 ||
		len(want.ResponseProps) > 0 ||
		want.RemoteExceptionType != ""
}

func assertGoldenAppResponseJSON(t *testing.T, want json.RawMessage, got any) {
	t.Helper()
	expected, err := decodeGoldenJSON(want)
	if err != nil {
		t.Fatalf("response-content want.appResponseJson is not valid JSON: %v", err)
	}
	actual, err := normalizeGoldenJSON(goldenCanonicalValue(got))
	if err != nil {
		t.Fatalf("canonicalize AppResponse: %v", err)
	}
	if mismatch := goldenJSONSubsetMismatch(expected, actual, "appResponseJson"); mismatch != "" {
		t.Fatalf("%s\nexpected subset:\n%s\nactual:\n%s", mismatch, prettyGoldenJSON(expected), prettyGoldenJSON(actual))
	}
}

func goldenCanonicalValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		if typeName, _ := typed["type"].(string); typeName != "" {
			if fields, ok := typed["fields"].(map[string]any); ok {
				if rawValue, ok := fields["value"]; ok && isGoldenNumberObject(typeName) {
					return map[string]any{
						"type":  typeName,
						"value": goldenCanonicalValue(rawValue),
					}
				}
				return map[string]any{
					"type":   typeName,
					"fields": goldenCanonicalMap(fields),
				}
			}
			if items, ok := typed["items"].([]any); ok {
				return map[string]any{
					"type":  typeName,
					"items": goldenCanonicalSlice(items),
				}
			}
			if entries, ok := typed["entries"].(map[string]any); ok {
				return map[string]any{
					"type": typeName,
					"map":  goldenCanonicalMap(entries),
				}
			}
		}
		return goldenCanonicalMap(typed)
	case []any:
		return goldenCanonicalSlice(typed)
	default:
		return value
	}
}

func isGoldenNumberObject(typeName string) bool {
	return typeName == "java.math.BigDecimal" || typeName == "java.math.BigInteger"
}

func goldenCanonicalMap(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		if key == "fieldNames" {
			continue
		}
		out[key] = goldenCanonicalValue(value)
	}
	return out
}

func goldenCanonicalSlice(values []any) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = goldenCanonicalValue(value)
	}
	return out
}

func normalizeGoldenJSON(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return decodeGoldenJSON(data)
}

func decodeGoldenJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func goldenJSONSubsetMismatch(expected, actual any, path string) string {
	switch expectedTyped := expected.(type) {
	case map[string]any:
		actualTyped, ok := actual.(map[string]any)
		if !ok {
			return path + ": actual is not an object"
		}
		for key, expectedValue := range expectedTyped {
			actualValue, ok := actualTyped[key]
			if !ok {
				return path + "." + key + ": missing key"
			}
			if mismatch := goldenJSONSubsetMismatch(expectedValue, actualValue, path+"."+key); mismatch != "" {
				return mismatch
			}
		}
	case []any:
		actualTyped, ok := actual.([]any)
		if !ok {
			return path + ": actual is not an array"
		}
		if len(expectedTyped) != len(actualTyped) {
			return path + ": array length mismatch"
		}
		for i, expectedValue := range expectedTyped {
			if mismatch := goldenJSONSubsetMismatch(expectedValue, actualTyped[i], path+"[]"); mismatch != "" {
				return mismatch
			}
		}
	default:
		if !reflect.DeepEqual(expected, actual) {
			return path + ": value mismatch"
		}
	}
	return ""
}

func prettyGoldenJSON(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "<invalid JSON>"
	}
	return string(data)
}

func assertGoldenRequestFixture(t *testing.T, want goldenFixtureWant) {
	t.Helper()
	if strings.TrimSpace(want.Service) == "" {
		t.Fatal("request-content want.service is required")
	}
	if strings.TrimSpace(want.Method) == "" {
		t.Fatal("request-content want.method is required")
	}
	if len(want.ParamTypes) == 0 {
		t.Fatal("request-content want.paramTypes is required")
	}
	if strings.TrimSpace(want.TargetServiceUnique) == "" {
		t.Fatal("request-content want.targetServiceUniqueName is required")
	}
	trimmedArgs := bytes.TrimSpace(want.ArgsJSON)
	if len(trimmedArgs) == 0 || bytes.Equal(trimmedArgs, []byte("null")) {
		t.Fatal("request-content want.argsJson is required")
	}
	var args any
	if err := json.Unmarshal(trimmedArgs, &args); err != nil {
		t.Fatalf("request-content want.argsJson is not valid JSON: %v", err)
	}
}
