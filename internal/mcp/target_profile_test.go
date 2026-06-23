package mcp

import "testing"

// TestTarget_SessionProfileIsInherited guards the documented profile precedence
// (per-call > session > default) for sofarpc_target. Resolving by sessionId
// with no per-call profile must inherit the session's Active Target Profile,
// matching sofarpc_invoke / sofarpc_doctor rather than collapsing to the
// configured defaultProfile ("local").
func TestTarget_SessionProfileIsInherited(t *testing.T) {
	dir := t.TempDir()
	writeOpenFile(t, dir, ".sofarpc/config.json", profileFixtureConfig)
	sessions := NewSessionStore()

	open := callOpen(t, Options{Sessions: sessions}, map[string]any{"cwd": dir, "profile": "test"})
	if open.ProfileError != "" || open.SessionID == "" {
		t.Fatalf("open failed: profileErr=%q id=%q", open.ProfileError, open.SessionID)
	}

	out := callTargetTool(t, Options{Sessions: sessions}, map[string]any{
		"sessionId":        open.SessionID,
		"connectTimeoutMs": 200,
	})
	if out.ProfileError != "" {
		t.Fatalf("unexpected target profile error: %s", out.ProfileError)
	}
	if out.ActiveProfile != "test" {
		t.Fatalf("ActiveProfile: got %q want test", out.ActiveProfile)
	}
	if out.Target.DirectURL != "bolt://test-host:12200" {
		t.Fatalf("target should inherit the session profile, got %q", out.Target.DirectURL)
	}
}

// TestTarget_PerCallProfileOverridesSession confirms a per-call profile still
// wins over the session profile in sofarpc_target.
func TestTarget_PerCallProfileOverridesSession(t *testing.T) {
	dir := t.TempDir()
	writeOpenFile(t, dir, ".sofarpc/config.json", profileFixtureConfig)
	sessions := NewSessionStore()

	open := callOpen(t, Options{Sessions: sessions}, map[string]any{"cwd": dir, "profile": "test"})
	if open.SessionID == "" {
		t.Fatalf("open failed: %+v", open)
	}

	out := callTargetTool(t, Options{Sessions: sessions}, map[string]any{
		"sessionId":        open.SessionID,
		"profile":          "local",
		"connectTimeoutMs": 200,
	})
	if out.ActiveProfile != "local" {
		t.Fatalf("per-call profile should win, got %q", out.ActiveProfile)
	}
	if out.Target.DirectURL != "bolt://127.0.0.1:12200" {
		t.Fatalf("per-call profile target, got %q", out.Target.DirectURL)
	}
}
