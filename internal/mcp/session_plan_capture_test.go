package mcp

import (
	"strings"
	"testing"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
)

func TestSessionStore_CapturePlanStoresSmallPlan(t *testing.T) {
	store := NewSessionStoreWithLimits(0, 0).WithMaxPlanBytes(1024).WithIDFunc(seqIDs())
	session := store.Create(Session{ProjectRoot: "/tmp"})

	capture := store.CapturePlan(session.ID, invoke.Plan{
		SchemaVersion: invoke.PlanSchemaVersion,
		Service:       "com.foo.Svc",
		Method:        "doThing",
		Args:          []any{"hello"},
	})
	if !capture.Captured {
		t.Fatalf("expected capture success, got %+v", capture)
	}
	if capture.PlanBytes <= 0 {
		t.Fatalf("expected positive planBytes, got %+v", capture)
	}
	got, ok := store.Get(session.ID)
	if !ok || got.LastPlan == nil {
		t.Fatalf("expected LastPlan to be stored, got session=%+v ok=%v", got, ok)
	}
}

func TestSessionStore_CapturePlanRejectsOversizedPlan(t *testing.T) {
	store := NewSessionStoreWithLimits(0, 0).WithMaxPlanBytes(64).WithIDFunc(seqIDs())
	session := store.Create(Session{ProjectRoot: "/tmp"})

	capture := store.CapturePlan(session.ID, invoke.Plan{
		SchemaVersion: invoke.PlanSchemaVersion,
		Service:       "com.foo.Svc",
		Method:        "doThing",
		Args:          []any{strings.Repeat("x", 1024)},
	})
	if capture.Captured {
		t.Fatalf("expected capture rejection, got %+v", capture)
	}
	if capture.Reason != "plan-too-large" {
		t.Fatalf("reason = %q, want plan-too-large", capture.Reason)
	}
	if capture.PlanBytes <= capture.MaxBytes {
		t.Fatalf("expected planBytes > maxBytes, got %+v", capture)
	}
	got, ok := store.Get(session.ID)
	if !ok {
		t.Fatal("oversized capture should not remove the session")
	}
	if got.LastPlan != nil {
		t.Fatalf("oversized plan should not be retained, got %+v", got.LastPlan)
	}
}

func TestSessionStore_CapturePlanBumpsLRUOnOversizedPlan(t *testing.T) {
	clock, advance := fakeClock(time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC))
	store := NewSessionStoreWithLimits(time.Hour, 0).WithClock(clock).WithIDFunc(seqIDs()).WithMaxPlanBytes(64)

	s := store.Create(Session{ProjectRoot: "/a"})
	advance(45 * time.Minute)
	capture := store.CapturePlan(s.ID, invoke.Plan{
		SchemaVersion: invoke.PlanSchemaVersion,
		Service:       "com.foo.Svc",
		Method:        "doThing",
		Args:          []any{strings.Repeat("x", 1024)},
	})
	if capture.Captured || capture.Reason != "plan-too-large" {
		t.Fatalf("expected oversized capture rejection, got %+v", capture)
	}
	advance(45 * time.Minute) // 90m since Create, 45m since CapturePlan
	_ = store.Create(Session{ProjectRoot: "/b"})

	if _, ok := store.Get(s.ID); !ok {
		t.Fatal("oversized CapturePlan should still bump lastUsed and keep the session alive")
	}
}

func TestSessionPlanMaxBytesFromEnv(t *testing.T) {
	t.Setenv(envSessionPlanMaxBytes, "2048")
	store := NewSessionStoreWithLimits(0, 0)
	if got := store.MaxPlanBytes(); got != 2048 {
		t.Fatalf("MaxPlanBytes = %d, want 2048", got)
	}
}

func TestSessionPlanMaxBytesEnvZeroDisablesBound(t *testing.T) {
	t.Setenv(envSessionPlanMaxBytes, "0")
	store := NewSessionStoreWithLimits(0, 0)
	if got := store.MaxPlanBytes(); got != 0 {
		t.Fatalf("MaxPlanBytes = %d, want 0", got)
	}
}
