package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestReplay_PayloadDryRunReturnsPlan(t *testing.T) {
	plan := samplePlan()
	out := callReplay(t, Options{}, map[string]any{
		"payload": plan,
		"dryRun":  true,
	})
	if !out.Ok {
		t.Fatalf("dry-run replay should succeed; got error=%+v", out.Error)
	}
	if out.Source != "payload" {
		t.Fatalf("source: got %q want payload", out.Source)
	}
	if out.Plan == nil || out.Plan.Service != plan.Service || out.Plan.Method != plan.Method {
		t.Fatalf("plan round-trip mismatch: %+v", out.Plan)
	}
}

func TestReplay_PayloadNonDryRunSurfacesDaemonUnavailable(t *testing.T) {
	out := callReplay(t, Options{}, map[string]any{
		"payload": samplePlan(),
	})
	if out.Error == nil || out.Error.Code != errcode.DaemonUnavailable {
		t.Fatalf("expected DaemonUnavailable, got %+v", out.Error)
	}
	if out.Plan == nil {
		t.Fatal("plan should still be attached when worker is missing")
	}
}

func TestReplay_BothSessionAndPayloadIsArgsInvalid(t *testing.T) {
	out := callReplay(t, Options{}, map[string]any{
		"sessionId": "ws_anything",
		"payload":   samplePlan(),
	})
	if out.Error == nil || out.Error.Code != errcode.ArgsInvalid {
		t.Fatalf("expected ArgsInvalid, got %+v", out.Error)
	}
}

func TestReplay_NeitherSessionNorPayloadIsArgsInvalid(t *testing.T) {
	out := callReplay(t, Options{}, map[string]any{})
	if out.Error == nil || out.Error.Code != errcode.ArgsInvalid {
		t.Fatalf("expected ArgsInvalid, got %+v", out.Error)
	}
}

func TestReplay_SessionNotFoundIsArgsInvalid(t *testing.T) {
	out := callReplay(t, Options{}, map[string]any{
		"sessionId": "ws_does_not_exist",
	})
	if out.Error == nil || out.Error.Code != errcode.ArgsInvalid {
		t.Fatalf("expected ArgsInvalid, got %+v", out.Error)
	}
}

func TestReplay_SessionWithoutPlanIsArgsInvalid(t *testing.T) {
	sessions := NewSessionStore()
	session := sessions.Create(Session{ProjectRoot: "/tmp"})
	out := callReplay(t, Options{Sessions: sessions}, map[string]any{
		"sessionId": session.ID,
	})
	if out.Error == nil || out.Error.Code != errcode.ArgsInvalid {
		t.Fatalf("expected ArgsInvalid, got %+v", out.Error)
	}
}

func TestReplay_SessionPlanRoundTrip(t *testing.T) {
	sessions := NewSessionStore()
	store := contract.NewInMemoryStore(
		facadesemantic.Class{
			FQN: "com.foo.Svc", Kind: facadesemantic.KindInterface,
			Methods: []facadesemantic.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}, ReturnType: "java.lang.String"},
			},
		},
	)
	opts := Options{Sessions: sessions, Facade: store}
	session := sessions.Create(Session{ProjectRoot: "/tmp"})

	// Tag the session via invoke dry-run.
	inv := callInvoke(t, opts, map[string]any{
		"service":   "com.foo.Svc",
		"method":    "doThing",
		"directUrl": "bolt://h:1",
		"sessionId": session.ID,
		"dryRun":    true,
	})
	if !inv.Ok {
		t.Fatalf("invoke should succeed; error=%+v", inv.Error)
	}

	// Now replay against the session.
	out := callReplay(t, opts, map[string]any{
		"sessionId": session.ID,
		"dryRun":    true,
	})
	if !out.Ok {
		t.Fatalf("session replay should succeed; got error=%+v", out.Error)
	}
	if out.Source != "session" {
		t.Fatalf("source: got %q want session", out.Source)
	}
	if out.Plan == nil || out.Plan.Method != "doThing" {
		t.Fatalf("plan round-trip mismatch: %+v", out.Plan)
	}
}

func TestReplay_PayloadMissingServiceIsArgsInvalid(t *testing.T) {
	plan := samplePlan()
	plan.Service = ""
	out := callReplay(t, Options{}, map[string]any{
		"payload": plan,
		"dryRun":  true,
	})
	if out.Error == nil || out.Error.Code != errcode.ArgsInvalid {
		t.Fatalf("expected ArgsInvalid, got %+v", out.Error)
	}
}

func TestReplay_PayloadMissingTargetModeIsTargetMissing(t *testing.T) {
	plan := samplePlan()
	plan.Target.Mode = ""
	out := callReplay(t, Options{}, map[string]any{
		"payload": plan,
		"dryRun":  true,
	})
	if out.Error == nil || out.Error.Code != errcode.TargetMissing {
		t.Fatalf("expected TargetMissing, got %+v", out.Error)
	}
}

func samplePlan() invoke.Plan {
	return invoke.Plan{
		Service:    "com.foo.Svc",
		Method:     "doThing",
		ParamTypes: []string{"java.lang.String"},
		Args:       []any{"hello"},
		Target: target.Config{
			Mode:      target.ModeDirect,
			DirectURL: "bolt://h:1",
		},
		ArgSource: "user",
	}
}

func callReplay(t *testing.T, opts Options, args map[string]any) ReplayOutput {
	t.Helper()
	server := New(opts)
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	result, err := client.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "sofarpc_replay",
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("call replay: %v", err)
	}
	body, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	var out ReplayOutput
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal structured: %v", err)
	}
	return out
}
