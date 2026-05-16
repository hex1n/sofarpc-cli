package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	"github.com/hex1n/sofarpc-cli/internal/javamodel"
	"github.com/hex1n/sofarpc-cli/internal/sofarpcwire"
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
	if out.Plan.SchemaVersion != invoke.PlanSchemaVersion {
		t.Fatalf("schemaVersion: got %q want %q", out.Plan.SchemaVersion, invoke.PlanSchemaVersion)
	}
}

func TestReplay_InvokeOutputPayloadDryRunReturnsPlan(t *testing.T) {
	plan := samplePlan()
	out := callReplay(t, Options{}, map[string]any{
		"payload": InvokeOutput{
			Ok:   true,
			Plan: &plan,
		},
		"dryRun": true,
	})
	if !out.Ok {
		t.Fatalf("dry-run replay should accept invoke output envelope; got error=%+v", out.Error)
	}
	if out.Source != "payload" {
		t.Fatalf("source: got %q want payload", out.Source)
	}
	if out.Plan == nil || out.Plan.Service != plan.Service || out.Plan.Method != plan.Method {
		t.Fatalf("plan round-trip mismatch: %+v", out.Plan)
	}
}

func TestReplay_SDKStructuredContentPayloadDryRunReturnsPlan(t *testing.T) {
	plan := samplePlan()
	out := callReplay(t, Options{}, map[string]any{
		"payload": map[string]any{
			"structuredContent": InvokeOutput{
				Ok:   true,
				Plan: &plan,
			},
		},
		"dryRun": true,
	})
	if !out.Ok {
		t.Fatalf("dry-run replay should accept SDK structuredContent envelope; got error=%+v", out.Error)
	}
	if out.Plan == nil || out.Plan.Service != plan.Service || out.Plan.Method != plan.Method {
		t.Fatalf("plan round-trip mismatch: %+v", out.Plan)
	}
}

func TestReplay_NonDryRunRequiresAllowInvokeEnv(t *testing.T) {
	t.Setenv(envAllowInvoke, "false")

	out := callReplay(t, Options{}, map[string]any{
		"payload": samplePlan(),
	})
	if out.Error == nil || out.Error.Code != errcode.InvocationRejected {
		t.Fatalf("expected InvocationRejected, got %+v", out.Error)
	}
	if out.Error.Phase != "replay" {
		t.Fatalf("phase = %q, want replay", out.Error.Phase)
	}
	if !strings.Contains(out.Error.Message, envAllowInvoke) {
		t.Fatalf("error message should mention %s: %q", envAllowInvoke, out.Error.Message)
	}
	if out.Plan == nil || out.Source != "payload" {
		t.Fatalf("rejected replay should keep plan/source: plan=%+v source=%q", out.Plan, out.Source)
	}
}

func TestReplay_NonDryRunRespectsAllowedServices(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	t.Setenv(envAllowedServices, "com.foo.OtherFacade")

	out := callReplay(t, Options{}, map[string]any{
		"payload": samplePlan(),
	})
	if out.Error == nil || out.Error.Code != errcode.InvocationRejected {
		t.Fatalf("expected InvocationRejected, got %+v", out.Error)
	}
	if out.Error.Phase != "replay" {
		t.Fatalf("phase = %q, want replay", out.Error.Phase)
	}
	if !strings.Contains(out.Error.Message, envAllowedServices) {
		t.Fatalf("error message should mention %s: %q", envAllowedServices, out.Error.Message)
	}
	if out.Plan == nil || out.Source != "payload" {
		t.Fatalf("rejected replay should keep plan/source: plan=%+v source=%q", out.Plan, out.Source)
	}
}

func TestReplay_NonDryRunRejectsTargetOverrideByDefault(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	t.Setenv(envAllowedServices, "")
	t.Setenv(envAllowTargetOverride, "false")

	out := callReplay(t, Options{}, map[string]any{
		"payload": samplePlan(),
	})
	if out.Error == nil || out.Error.Code != errcode.InvocationRejected {
		t.Fatalf("expected InvocationRejected, got %+v", out.Error)
	}
	if out.Error.Phase != "replay" {
		t.Fatalf("phase = %q, want replay", out.Error.Phase)
	}
	if !strings.Contains(out.Error.Message, envAllowTargetOverride) {
		t.Fatalf("error message should mention %s: %q", envAllowTargetOverride, out.Error.Message)
	}
	if out.Plan == nil || out.Source != "payload" {
		t.Fatalf("rejected replay should keep plan/source: plan=%+v source=%q", out.Plan, out.Source)
	}
}

func TestReplay_ConfigErrorDiagnosticsUseSessionProject(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	t.Setenv(envAllowedServices, "")
	t.Setenv(envAllowTargetOverride, "false")
	projectRoot := t.TempDir()
	writeMCPProjectFile(t, projectRoot, ".sofarpc/config.local.json", `{"mode":"registry","directUrl":"bolt://project-host:12200"}`)
	sessions := NewSessionStore()
	session := sessions.Create(Session{ProjectRoot: projectRoot})
	if !sessions.UpdatePlan(session.ID, samplePlan()) {
		t.Fatal("UpdatePlan should capture sample plan")
	}

	out := callReplay(t, Options{
		Sessions: sessions,
		TargetSources: target.Sources{
			Env: target.Config{DirectURL: "bolt://env-host:12200"},
		},
	}, map[string]any{
		"sessionId": session.ID,
	})
	if out.Error == nil || out.Error.Code != errcode.InvocationRejected {
		t.Fatalf("expected invocation rejected, got %+v", out.Error)
	}
	if out.Error.Hint == nil || out.Error.Hint.NextArgs["project"] != projectRoot {
		t.Fatalf("hint should preserve project context, got %+v", out.Error.Hint)
	}
	assertConfigDiagnostics(t, out.Diagnostics, projectRoot)
}

func TestReplay_PayloadNonDryRunWithUnsupportedTargetSurfacesInvocationRejected(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	t.Setenv(envAllowedServices, "")

	out := callReplay(t, Options{}, map[string]any{
		"payload": sampleRegistryPlan(),
	})
	if out.Error == nil || out.Error.Code != errcode.InvocationRejected {
		t.Fatalf("expected InvocationRejected, got %+v", out.Error)
	}
	if out.Plan == nil {
		t.Fatal("plan should still be attached on InvocationRejected")
	}
}

func TestReplay_PayloadDirectTransportRoundTrip(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	t.Setenv(envAllowedServices, "")

	plan := samplePlan()
	appResponse := sofarpcwire.NormalizeArgs([]any{
		map[string]any{
			"@type":   "com.example.demo.Result",
			"success": true,
			"message": "ok",
		},
	})[0]
	responseBytes, err := sofarpcwire.BuildSuccessResponse(appResponse)
	if err != nil {
		t.Fatalf("BuildSuccessResponse: %v", err)
	}
	directURL, stop := fakeDirectServer(t, responseBytes)
	defer stop()
	plan.Target.DirectURL = directURL

	out := callReplay(t, Options{
		TargetSources: target.Sources{
			Env: target.Config{DirectURL: directURL},
		},
	}, map[string]any{
		"payload": plan,
	})
	if !out.Ok {
		t.Fatalf("expected Ok=true, got error=%+v diagnostics=%+v", out.Error, out.Diagnostics)
	}
	if out.Source != "payload" {
		t.Fatalf("source: got %q want payload", out.Source)
	}
	if transport, _ := out.Diagnostics["transport"].(string); transport != invoke.DirectTransportName {
		t.Fatalf("transport: got %q want %q", transport, invoke.DirectTransportName)
	}
	result, ok := out.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", out.Result)
	}
	if got := result["type"]; got != "com.example.demo.Result" {
		t.Fatalf("result.type: got %#v", got)
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
		javamodel.Class{
			FQN: "com.foo.Svc", Kind: javamodel.KindInterface,
			Methods: []javamodel.Method{
				{Name: "doThing", ParamTypes: []string{"java.lang.String"}, ReturnType: "java.lang.String"},
			},
		},
	)
	opts := Options{Sessions: sessions, Contract: store}
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
	if inv.Plan == nil || inv.Plan.SchemaVersion != invoke.PlanSchemaVersion {
		t.Fatalf("captured plan schemaVersion = %+v", inv.Plan)
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
	if out.Plan.SchemaVersion != invoke.PlanSchemaVersion {
		t.Fatalf("schemaVersion: got %q want %q", out.Plan.SchemaVersion, invoke.PlanSchemaVersion)
	}
}

func TestReplay_PayloadMissingSchemaVersionIsRejected(t *testing.T) {
	plan := samplePlan()
	plan.SchemaVersion = ""
	out := callReplay(t, Options{}, map[string]any{
		"payload": plan,
		"dryRun":  true,
	})
	if out.Error == nil || out.Error.Code != errcode.PlanVersionUnsupported {
		t.Fatalf("expected PlanVersionUnsupported, got %+v", out.Error)
	}
}

func TestReplay_PayloadUnsupportedSchemaVersionIsRejected(t *testing.T) {
	plan := samplePlan()
	plan.SchemaVersion = "sofarpc.invoke.plan/v999"
	out := callReplay(t, Options{}, map[string]any{
		"payload": plan,
		"dryRun":  true,
	})
	if out.Error == nil || out.Error.Code != errcode.PlanVersionUnsupported {
		t.Fatalf("expected PlanVersionUnsupported, got %+v", out.Error)
	}
}

func TestReplay_SessionUnsupportedSchemaVersionIsRejected(t *testing.T) {
	sessions := NewSessionStore()
	plan := samplePlan()
	plan.SchemaVersion = "sofarpc.invoke.plan/v999"
	session := sessions.Create(Session{ProjectRoot: "/tmp", LastPlan: &plan})

	out := callReplay(t, Options{Sessions: sessions}, map[string]any{
		"sessionId": session.ID,
		"dryRun":    true,
	})
	if out.Error == nil || out.Error.Code != errcode.PlanVersionUnsupported {
		t.Fatalf("expected PlanVersionUnsupported, got %+v", out.Error)
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

func TestReplay_PayloadArityMismatchIsArgsInvalid(t *testing.T) {
	plan := samplePlan()
	plan.Args = nil
	out := callReplay(t, Options{}, map[string]any{
		"payload": plan,
		"dryRun":  true,
	})
	if out.Error == nil || out.Error.Code != errcode.ArgsInvalid {
		t.Fatalf("expected ArgsInvalid, got %+v", out.Error)
	}
	if !strings.Contains(out.Error.Message, "arity mismatch") {
		t.Fatalf("error message should explain arity mismatch: %q", out.Error.Message)
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

func TestReplay_PayloadEnvelopeWithNullPlanIsArgsInvalid(t *testing.T) {
	out := callReplay(t, Options{}, map[string]any{
		"payload": map[string]any{"plan": nil},
		"dryRun":  true,
	})
	if out.Error == nil || out.Error.Code != errcode.ArgsInvalid {
		t.Fatalf("expected ArgsInvalid, got %+v", out.Error)
	}
}

func TestReplay_DecodePayloadPreservesLargeLongString(t *testing.T) {
	in, payload, err := decodeReplayInput(&sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{
			Arguments: json.RawMessage(`{
				"dryRun": true,
				"payload": {
					"schemaVersion": "sofarpc.invoke.plan/v1",
					"service": "com.foo.Svc",
					"method": "doThing",
					"paramTypes": ["com.foo.Req"],
					"args": [{"id":"434153733362950144"}],
					"target": {"mode":"direct","directUrl":"bolt://h:1"},
					"argSource": "user"
				}
			}`),
		},
	})
	if err != nil {
		t.Fatalf("decodeReplayInput: %v", err)
	}
	if !in.DryRun {
		t.Fatalf("dryRun: got false")
	}
	plan, err := planFromPayload(payload)
	if err != nil {
		t.Fatalf("planFromPayload: %v", err)
	}
	arg, ok := plan.Args[0].(map[string]any)
	if !ok {
		t.Fatalf("arg type: %T", plan.Args[0])
	}
	if got, _ := arg["id"].(string); got != "434153733362950144" {
		t.Fatalf("id: got %#v", arg["id"])
	}
}

func samplePlan() invoke.Plan {
	return invoke.Plan{
		SchemaVersion: invoke.PlanSchemaVersion,
		Service:       "com.foo.Svc",
		Method:        "doThing",
		ParamTypes:    []string{"java.lang.String"},
		Args:          []any{"hello"},
		Target: target.Config{
			Mode:      target.ModeDirect,
			DirectURL: "bolt://h:1",
		},
		ArgSource: "user",
	}
}

func sampleRegistryPlan() invoke.Plan {
	plan := samplePlan()
	plan.Target = target.Config{
		Mode:            target.ModeRegistry,
		RegistryAddress: "zookeeper://h:1",
	}
	return plan
}

func callReplay(t *testing.T, opts Options, args map[string]any) ReplayOutput {
	t.Helper()
	body, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return callReplayRaw(t, opts, body)
}

func callReplayRaw(t *testing.T, opts Options, raw json.RawMessage) ReplayOutput {
	t.Helper()
	server := New(opts)
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	result, err := client.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "sofarpc_replay",
		Arguments: raw,
	})
	if err != nil {
		t.Fatalf("call replay: %v", err)
	}
	body, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	var out ReplayOutput
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("unmarshal structured: %v", err)
	}
	return out
}
