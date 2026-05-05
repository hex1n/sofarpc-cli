package sofarpcwire

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type goldenWireFixture struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Kind        string            `json:"kind"`
	ContentHex  string            `json:"contentHex,omitempty"`
	Want        goldenFixtureWant `json:"want,omitempty"`
}

type goldenFixtureWant struct {
	IsError             *bool             `json:"isError,omitempty"`
	ErrorMsg            string            `json:"errorMsg,omitempty"`
	AppResponseType     string            `json:"appResponseType,omitempty"`
	ResponseProps       map[string]string `json:"responseProps,omitempty"`
	RemoteExceptionType string            `json:"remoteExceptionType,omitempty"`
}

func TestJavaGoldenResponseFixtures(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "golden", "*.json"))
	if err != nil {
		t.Fatalf("glob golden fixtures: %v", err)
	}
	if len(paths) == 0 {
		t.Skip("no Java SOFARPC golden fixtures checked in yet")
	}

	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			var fixture goldenWireFixture
			if err := json.Unmarshal(body, &fixture); err != nil {
				t.Fatalf("parse fixture: %v", err)
			}
			if fixture.Kind != "response-content" {
				t.Skipf("fixture kind %q is not decoded by this harness", fixture.Kind)
			}
			content, err := hex.DecodeString(fixture.ContentHex)
			if err != nil {
				t.Fatalf("decode contentHex: %v", err)
			}
			resp, err := DecodeResponse(content)
			if err != nil {
				t.Fatalf("DecodeResponse: %v", err)
			}
			assertGoldenResponse(t, fixture.Want, resp)
		})
	}
}

func assertGoldenResponse(t *testing.T, want goldenFixtureWant, got DecodedResponse) {
	t.Helper()
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
