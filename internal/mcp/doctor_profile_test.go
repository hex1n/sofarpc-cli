package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDoctor_ProfileCheckReportsActiveAndAvailable verifies that doctor honours
// a selected Target Profile: the profile check reports it active and lists the
// available profiles, the target check resolves the profile endpoint, and the
// invocation-properties check merges the profile's declarations over the base.
func TestDoctor_ProfileCheckReportsActiveAndAvailable(t *testing.T) {
	dir := t.TempDir()
	writeOpenFile(t, dir, ".sofarpc/config.json", profileFixtureConfig)

	out := callDoctor(t, Options{}, map[string]any{"cwd": dir, "profile": "test"})

	profileCheck := findCheck(t, out, "profile")
	if !profileCheck.Ok {
		t.Fatalf("profile check should pass for a defined profile, got %+v", profileCheck)
	}
	if !strings.Contains(profileCheck.Detail, "test") {
		t.Fatalf("profile detail should name the active profile, got %q", profileCheck.Detail)
	}
	profileData := mustMarshal(t, profileCheck.Data)
	if !strings.Contains(profileData, "local") || !strings.Contains(profileData, "test") {
		t.Fatalf("profile data should list available profiles, got %s", profileData)
	}

	// The target check resolves the profile's endpoint (reachability aside).
	targetCheck := findCheck(t, out, "target")
	if !strings.Contains(targetCheck.Detail, "test-host:12200") {
		t.Fatalf("target check should resolve the profile endpoint, got %q", targetCheck.Detail)
	}

	// The profile's invocation property overlays the base property.
	propsCheck := findCheck(t, out, "invocation-properties")
	if !propsCheck.Ok {
		t.Fatalf("invocation-properties should pass, got %+v", propsCheck)
	}
	propsData := mustMarshal(t, propsCheck.Data)
	if !strings.Contains(propsData, "tenant") || !strings.Contains(propsData, "appName") {
		t.Fatalf("invocation-properties should merge profile (tenant) and base (appName), got %s", propsData)
	}
}

// TestDoctor_UnknownProfileFailsChecks proves a named-but-undefined profile is a
// hard failure across the dependent checks rather than a silent fall-through to
// base settings.
func TestDoctor_UnknownProfileFailsChecks(t *testing.T) {
	dir := t.TempDir()
	writeOpenFile(t, dir, ".sofarpc/config.json", profileFixtureConfig)

	out := callDoctor(t, Options{}, map[string]any{"cwd": dir, "profile": "prod"})

	for _, name := range []string{"profile", "target", "invoke-policy", "invocation-properties"} {
		check := findCheck(t, out, name)
		if check.Ok {
			t.Fatalf("%s check should fail for an undefined profile, got %+v", name, check)
		}
		if !strings.Contains(check.Detail, "not defined") {
			t.Fatalf("%s detail should explain the undefined profile, got %q", name, check.Detail)
		}
	}
}

// TestDoctor_SessionProfileInheritedByDoctor confirms a doctor call by sessionId
// inherits the session's Active Target Profile when no per-call profile is set.
func TestDoctor_SessionProfileInheritedByDoctor(t *testing.T) {
	dir := t.TempDir()
	writeOpenFile(t, dir, ".sofarpc/config.json", profileFixtureConfig)
	sessions := NewSessionStore()

	open := callOpen(t, Options{Sessions: sessions}, map[string]any{"cwd": dir, "profile": "test"})
	if open.ActiveProfile != "test" {
		t.Fatalf("open ActiveProfile: got %q want test", open.ActiveProfile)
	}

	out := callDoctor(t, Options{Sessions: sessions}, map[string]any{"sessionId": open.SessionID})
	profileCheck := findCheck(t, out, "profile")
	if !profileCheck.Ok || !strings.Contains(profileCheck.Detail, "test") {
		t.Fatalf("doctor should inherit the session profile, got %+v", profileCheck)
	}
}

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(body)
}
