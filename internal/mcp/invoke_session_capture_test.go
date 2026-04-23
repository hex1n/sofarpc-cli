package mcp

import (
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/javamodel"
)

func TestInvoke_DryRunOversizedPlanStillReturnsPlanButSkipsSessionCapture(t *testing.T) {
	sessions := NewSessionStoreWithLimits(0, 0).WithMaxPlanBytes(128).WithIDFunc(seqIDs())
	session := sessions.Create(Session{ProjectRoot: "/tmp"})
	store := contract.NewInMemoryStore(javamodel.Class{
		FQN: "com.foo.Svc", Kind: javamodel.KindInterface,
		Methods: []javamodel.Method{{Name: "doThing", ParamTypes: []string{"java.lang.String"}}},
	})

	out := callInvoke(t, Options{Sessions: sessions, Contract: store}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"directUrl": "bolt://h:1",
		"sessionId": session.ID,
		"args":      []any{strings.Repeat("x", 2048)},
		"dryRun":    true,
	})
	if !out.Ok {
		t.Fatalf("dry-run should still succeed; got error=%+v", out.Error)
	}
	if out.Plan == nil || out.Plan.Args[0] == "" {
		t.Fatalf("full plan should still be returned, got %+v", out.Plan)
	}
	capture, ok := out.Diagnostics["sessionPlanCapture"].(map[string]any)
	if !ok {
		t.Fatalf("expected sessionPlanCapture diagnostic, got %+v", out.Diagnostics)
	}
	if got := capture["reason"]; got != "plan-too-large" {
		t.Fatalf("capture reason = %#v, want plan-too-large", got)
	}
	stored, ok := sessions.Get(session.ID)
	if !ok {
		t.Fatal("session should still exist")
	}
	if stored.LastPlan != nil {
		t.Fatalf("oversized plan should not be retained in session, got %+v", stored.LastPlan)
	}
}

func TestInvoke_DryRunCapturedPlanCanReplayBySession(t *testing.T) {
	sessions := NewSessionStoreWithLimits(0, 0).WithMaxPlanBytes(4096).WithIDFunc(seqIDs())
	session := sessions.Create(Session{ProjectRoot: "/tmp"})
	store := contract.NewInMemoryStore(javamodel.Class{
		FQN: "com.foo.Svc", Kind: javamodel.KindInterface,
		Methods: []javamodel.Method{{Name: "doThing", ParamTypes: []string{"java.lang.String"}}},
	})

	out := callInvoke(t, Options{Sessions: sessions, Contract: store}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"directUrl": "bolt://h:1",
		"sessionId": session.ID,
		"args":      []any{"hello"},
		"dryRun":    true,
	})
	if !out.Ok {
		t.Fatalf("dry-run should succeed; got error=%+v", out.Error)
	}
	if out.Diagnostics != nil {
		if _, exists := out.Diagnostics["sessionPlanCapture"]; exists {
			t.Fatalf("successful capture should not add noisy diagnostics: %+v", out.Diagnostics)
		}
	}

	replay := callReplay(t, Options{Sessions: sessions}, map[string]any{
		"sessionId": session.ID,
		"dryRun":    true,
	})
	if !replay.Ok {
		t.Fatalf("session replay should succeed; got error=%+v", replay.Error)
	}
	if replay.Plan == nil || replay.Plan.SchemaVersion != invoke.PlanSchemaVersion {
		t.Fatalf("replayed plan mismatch: %+v", replay.Plan)
	}
}

func TestInvoke_OversizedSessionCaptureRequiresPayloadReplay(t *testing.T) {
	sessions := NewSessionStoreWithLimits(0, 0).WithMaxPlanBytes(128).WithIDFunc(seqIDs())
	session := sessions.Create(Session{ProjectRoot: "/tmp"})
	store := contract.NewInMemoryStore(javamodel.Class{
		FQN: "com.foo.Svc", Kind: javamodel.KindInterface,
		Methods: []javamodel.Method{{Name: "doThing", ParamTypes: []string{"java.lang.String"}}},
	})

	inv := callInvoke(t, Options{Sessions: sessions, Contract: store}, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"directUrl": "bolt://h:1",
		"sessionId": session.ID,
		"args":      []any{strings.Repeat("x", 2048)},
		"dryRun":    true,
	})
	if !inv.Ok || inv.Plan == nil {
		t.Fatalf("invoke dry-run should return full plan; out=%+v", inv)
	}

	bySession := callReplay(t, Options{Sessions: sessions}, map[string]any{
		"sessionId": session.ID,
		"dryRun":    true,
	})
	if bySession.Error == nil || bySession.Error.Code != errcode.ArgsInvalid {
		t.Fatalf("session replay should fail after skipped capture, got %+v", bySession.Error)
	}

	byPayload := callReplay(t, Options{Sessions: sessions}, map[string]any{
		"payload": inv.Plan,
		"dryRun": true,
	})
	if !byPayload.Ok {
		t.Fatalf("payload replay should still work, got error=%+v", byPayload.Error)
	}
}
