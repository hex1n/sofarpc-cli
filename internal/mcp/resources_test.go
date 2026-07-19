package mcp

import (
	"context"
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSessionPlanEntryResource_ReadByPlanID(t *testing.T) {
	sessions := NewSessionStoreWithLimits(0, 0).WithIDFunc(seqIDs())
	session := sessions.Create(Session{ProjectRoot: "/tmp/proj"})

	first := samplePlan()
	first.Method = "firstMethod"
	second := samplePlan()
	second.Method = "secondMethod"
	firstCapture := sessions.CapturePlan(session.ID, first)
	secondCapture := sessions.CapturePlan(session.ID, second)
	if !firstCapture.Captured || !secondCapture.Captured {
		t.Fatalf("captures should succeed: %+v / %+v", firstCapture, secondCapture)
	}

	server := New(Options{Sessions: sessions})
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	// An older history entry stays addressable after a newer capture.
	entryURI := sessionPlanEntryResourceURI(session.ID, firstCapture.PlanID)
	var entry planResource
	if err := json.Unmarshal([]byte(readResourceText(t, ctx, client, entryURI)), &entry); err != nil {
		t.Fatalf("unmarshal plan entry resource: %v", err)
	}
	if entry.PlanID != firstCapture.PlanID || entry.Plan.Method != "firstMethod" {
		t.Fatalf("plan entry mismatch: %+v", entry)
	}

	// The session resource lists the history newest-first with planIds.
	var summary sessionResource
	if err := json.Unmarshal([]byte(readResourceText(t, ctx, client, sessionResourceURI(session.ID))), &summary); err != nil {
		t.Fatalf("unmarshal session resource: %v", err)
	}
	if len(summary.Plans) != 2 {
		t.Fatalf("session resource should list 2 plans: %+v", summary.Plans)
	}
	if summary.Plans[0].PlanID != secondCapture.PlanID || summary.Plans[0].Method != "secondMethod" {
		t.Fatalf("plans should be newest-first: %+v", summary.Plans)
	}
	if summary.Plans[1].URI != entryURI {
		t.Fatalf("plan ref URI mismatch: got %q want %q", summary.Plans[1].URI, entryURI)
	}

	// Unknown planIds are not found rather than falling back to another plan.
	if _, err := client.ReadResource(ctx, &sdkmcp.ReadResourceParams{
		URI: sessionPlanEntryResourceURI(session.ID, "pl_does_not_exist"),
	}); err == nil {
		t.Fatal("unknown planId should return resource-not-found")
	}
}
