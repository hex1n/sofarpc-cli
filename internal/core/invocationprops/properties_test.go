package invocationprops

import (
	"encoding/json"
	"testing"
)

func TestMerge_PrecedenceUnsetAndRedaction(t *testing.T) {
	empty := ""
	projectTenant := "shared"
	projectLegacy := "old"
	localTenant := "local"

	props, err := Merge(
		Source{Name: "input", Declarations: Declarations{
			"color": {Value: &empty},
		}},
		Source{Name: "project-local", Declarations: Declarations{
			"tenant": {Value: &localTenant},
			"legacy": {Unset: true},
			"auth":   {Env: " LOCAL_TOKEN "},
		}},
		Source{Name: "project", Declarations: Declarations{
			"tenant": {Value: &projectTenant},
			"legacy": {Value: &projectLegacy},
			"auth":   {Env: "PROJECT_TOKEN"},
		}},
	)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if got := valueOf(props["tenant"]); got != "local" {
		t.Fatalf("tenant: got %q", got)
	}
	if _, ok := props["legacy"]; ok {
		t.Fatalf("legacy should be masked by project-local unset: %#v", props["legacy"])
	}
	if got := valueOf(props["color"]); got != "" {
		t.Fatalf("literal empty value should be preserved, got %q", got)
	}
	auth := props["auth"]
	if auth.Env != "LOCAL_TOKEN" || !auth.Redacted {
		t.Fatalf("auth should use redacted local env reference: %+v", auth)
	}
}

func TestNormalizeInput_RejectsInvalidDeclarations(t *testing.T) {
	value := "x"
	tests := map[string]Declarations{
		"multiple sources": {
			"k": {Value: &value, Env: "TOKEN"},
		},
		"redacted input": {
			"k": {Env: "TOKEN", Redacted: true},
		},
	}
	for name, decls := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeInput(decls); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestNormalizeInput_AllowsBaggageKeysThatLookLikeSOFARequestProps(t *testing.T) {
	value := "x"
	props, err := NormalizeInput(Declarations{
		"type":            {Value: &value},
		"generic.revise":  {Value: &value},
		"sofa_head_route": {Value: &value},
	})
	if err != nil {
		t.Fatalf("NormalizeInput: %v", err)
	}
	if got := valueOf(props["sofa_head_route"]); got != "x" {
		t.Fatalf("sofa_head_route = %q", got)
	}
}

func TestNormalizePlan_RedactsEnvAndRejectsUnset(t *testing.T) {
	props, err := NormalizePlan(Declarations{
		"auth": {Env: "TOKEN"},
	})
	if err != nil {
		t.Fatalf("NormalizePlan: %v", err)
	}
	if got := props["auth"]; got.Env != "TOKEN" || !got.Redacted {
		t.Fatalf("env plan property should be redacted: %+v", got)
	}

	if _, err := NormalizePlan(Declarations{"tenant": {Unset: true}}); err == nil {
		t.Fatal("expected unset to be rejected in replay plan")
	}
}

func TestResolve_ResolvesEnvAndRejectsMissingOrEmptyEnv(t *testing.T) {
	empty := ""
	props := Declarations{
		"literal": {Value: &empty},
		"auth":    {Env: "TOKEN", Redacted: true},
	}
	resolved, err := Resolve(props, func(name string) (string, bool) {
		if name == "TOKEN" {
			return "secret", true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved["literal"] != "" || resolved["auth"] != "secret" {
		t.Fatalf("resolved values: %#v", resolved)
	}

	for name, lookup := range map[string]EnvLookup{
		"missing": func(string) (string, bool) { return "", false },
		"empty":   func(string) (string, bool) { return "", true },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Resolve(Declarations{"auth": {Env: "TOKEN", Redacted: true}}, lookup); err == nil {
				t.Fatal("expected missing or empty env error")
			}
		})
	}
}

func TestDeclarationUnmarshal_RejectsUnknownFieldAndMultipleJSONValues(t *testing.T) {
	tests := map[string]string{
		"unknown":  `{"value":"x","extra":true}`,
		"multiple": `{"value":"x"} {}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			var decl Declaration
			err := json.Unmarshal([]byte(body), &decl)
			if err == nil {
				t.Fatal("expected decode error")
			}
		})
	}
}

func valueOf(decl Declaration) string {
	if decl.Value == nil {
		return "<nil>"
	}
	return *decl.Value
}
