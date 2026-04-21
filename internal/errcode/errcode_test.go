package errcode

import (
	"encoding/json"
	"testing"
)

func TestErrorMarshalIncludesHint(t *testing.T) {
	err := New(TargetMissing, "resolve", "target is required").
		WithHint("resolve_target", map[string]any{"service": "com.example.Demo"}, "no target resolved")
	body, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("marshal: %v", marshalErr)
	}
	want := `{"code":"target.missing","message":"target is required","phase":"resolve","hint":{"nextTool":"resolve_target","nextArgs":{"service":"com.example.Demo"},"reason":"no target resolved"}}`
	if got := string(body); got != want {
		t.Fatalf("unexpected marshal output:\n got: %s\nwant: %s", got, want)
	}
}

func TestErrorWithoutHintOmitsField(t *testing.T) {
	err := New(ServiceMissing, "resolve", "--service is required")
	body, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("marshal: %v", marshalErr)
	}
	got := string(body)
	if want := `{"code":"input.service-missing","message":"--service is required","phase":"resolve"}`; got != want {
		t.Fatalf("unexpected marshal output:\n got: %s\nwant: %s", got, want)
	}
}

func TestErrorStringFallsBackToCode(t *testing.T) {
	err := &Error{Code: InvocationRejected}
	if err.Error() != "runtime.rejected" {
		t.Fatalf("expected code fallback, got %q", err.Error())
	}
}

// Nil-receiver guards matter because errcode values travel through `error`
// interfaces — a nil *Error wrapped in an error should not panic on .Error().
func TestErrorOnNilReceiverReturnsEmpty(t *testing.T) {
	var err *Error
	if got := err.Error(); got != "" {
		t.Fatalf("expected empty string from nil receiver, got %q", got)
	}
}

func TestWithHintOnNilReceiverReturnsNil(t *testing.T) {
	var err *Error
	if got := err.WithHint("next", nil, "why"); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}
