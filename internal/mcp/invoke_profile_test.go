package mcp

import (
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
)

const profileFixtureConfig = `{
  "allowedServices": ["com.foo.Svc"],
  "defaultProfile": "local",
  "invocationProperties": { "appName": { "value": "demo" } },
  "profiles": {
    "test": {
      "directUrl": "bolt://test-host:12200",
      "invocationProperties": { "tenant": { "value": "test-tenant" } }
    },
    "local": { "directUrl": "bolt://127.0.0.1:12200" }
  }
}`

func TestOpenInvoke_SessionInheritsActiveProfile(t *testing.T) {
	dir := t.TempDir()
	writeOpenFile(t, dir, ".sofarpc/config.json", profileFixtureConfig)
	sessions := NewSessionStore()

	open := callOpen(t, Options{Sessions: sessions}, map[string]any{"cwd": dir, "profile": "test"})
	if open.ProfileError != "" {
		t.Fatalf("unexpected open profile error: %s", open.ProfileError)
	}
	if open.ActiveProfile != "test" {
		t.Fatalf("ActiveProfile: got %q want test", open.ActiveProfile)
	}
	if !containsString(open.AvailableProfiles, "test") || !containsString(open.AvailableProfiles, "local") {
		t.Fatalf("AvailableProfiles: got %v", open.AvailableProfiles)
	}
	if open.Target.DirectURL != "bolt://test-host:12200" {
		t.Fatalf("open target: got %q want test profile url", open.Target.DirectURL)
	}

	// Invoke by sessionId with no per-call profile -> inherits session "test".
	out := callInvoke(t, Options{Sessions: sessions}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"types":     []any{"java.lang.String"},
		"args":      []any{"hello"},
		"sessionId": open.SessionID,
		"dryRun":    true,
	})
	if !out.Ok || out.Plan == nil {
		t.Fatalf("invoke dry-run failed: %+v", out.Error)
	}
	if out.Plan.Target.DirectURL != "bolt://test-host:12200" {
		t.Fatalf("invoke should target the session profile, got %q", out.Plan.Target.DirectURL)
	}
	if got := out.Plan.InvocationProperties["tenant"].Value; got == nil || *got != "test-tenant" {
		t.Fatalf("invoke should carry profile invocation property, got %#v", out.Plan.InvocationProperties["tenant"])
	}
	if got := out.Plan.InvocationProperties["appName"].Value; got == nil || *got != "demo" {
		t.Fatalf("invoke should inherit base invocation property, got %#v", out.Plan.InvocationProperties["appName"])
	}
}

func TestOpenInvoke_PerCallProfileOverridesSession(t *testing.T) {
	dir := t.TempDir()
	writeOpenFile(t, dir, ".sofarpc/config.json", profileFixtureConfig)
	sessions := NewSessionStore()

	open := callOpen(t, Options{Sessions: sessions}, map[string]any{"cwd": dir, "profile": "test"})

	// Per-call profile=local must override the session's "test".
	out := callInvoke(t, Options{Sessions: sessions}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"types":     []any{"java.lang.String"},
		"args":      []any{"hi"},
		"sessionId": open.SessionID,
		"profile":   "local",
		"dryRun":    true,
	})
	if !out.Ok || out.Plan == nil {
		t.Fatalf("invoke dry-run failed: %+v", out.Error)
	}
	if out.Plan.Target.DirectURL != "bolt://127.0.0.1:12200" {
		t.Fatalf("per-call profile should win, got %q", out.Plan.Target.DirectURL)
	}
	// The "local" profile has no tenant; only the base appName should remain.
	if _, ok := out.Plan.InvocationProperties["tenant"]; ok {
		t.Fatalf("local profile carries no tenant; got %#v", out.Plan.InvocationProperties["tenant"])
	}
	if got := out.Plan.InvocationProperties["appName"].Value; got == nil || *got != "demo" {
		t.Fatalf("base appName should remain, got %#v", out.Plan.InvocationProperties["appName"])
	}
}

func TestOpen_UnknownProfileIsHardError(t *testing.T) {
	dir := t.TempDir()
	writeOpenFile(t, dir, ".sofarpc/config.json", profileFixtureConfig)
	sessions := NewSessionStore()

	open := callOpen(t, Options{Sessions: sessions}, map[string]any{"cwd": dir, "profile": "prod"})
	if open.ProfileError == "" {
		t.Fatalf("expected ProfileError for undefined profile")
	}
	if open.SessionID != "" {
		t.Fatalf("no session should be opened for an undefined profile, got %q", open.SessionID)
	}
}

// TestInvoke_TrustedFallbackPreservesSessionProfile guards against the auto->
// trusted fallback resolving the target with an empty profile. The first
// BuildPlan misses on the empty contract store (ContractUnresolvable) and the
// complete service/method/types/args tuple drives the fallback; the fallback
// must keep the session's "test" profile rather than collapsing to the
// configured defaultProfile ("local").
func TestInvoke_TrustedFallbackPreservesSessionProfile(t *testing.T) {
	dir := t.TempDir()
	writeOpenFile(t, dir, ".sofarpc/config.json", profileFixtureConfig)
	sessions := NewSessionStore()

	open := callOpen(t, Options{Sessions: sessions}, map[string]any{"cwd": dir, "profile": "test"})
	if open.ProfileError != "" || open.SessionID == "" {
		t.Fatalf("open failed: profileErr=%q id=%q", open.ProfileError, open.SessionID)
	}

	out := callInvoke(t, Options{Sessions: sessions, Contract: contract.NewInMemoryStore()}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"types":     []any{"java.lang.String"},
		"args":      []any{"hello"},
		"sessionId": open.SessionID,
		"dryRun":    true,
	})
	if !out.Ok || out.Plan == nil {
		t.Fatalf("trusted fallback dry-run failed: %+v", out.Error)
	}
	if out.Plan.ContractSource != "trusted" {
		t.Fatalf("expected the auto->trusted fallback path, got contractSource=%q", out.Plan.ContractSource)
	}
	if out.Plan.Target.DirectURL != "bolt://test-host:12200" {
		t.Fatalf("trusted fallback dropped the session profile, got %q", out.Plan.Target.DirectURL)
	}
	if out.Plan.Profile != "test" {
		t.Fatalf("plan profile provenance: got %q want test", out.Plan.Profile)
	}
	if got := out.Plan.InvocationProperties["tenant"].Value; got == nil || *got != "test-tenant" {
		t.Fatalf("trusted fallback should keep profile invocation properties, got %#v", out.Plan.InvocationProperties["tenant"])
	}
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
