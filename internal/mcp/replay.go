package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ReplayOutput is the structured payload for sofarpc_replay. Ok=true
// means the plan executed (or, in dryRun, was extracted cleanly) and
// Result / Diagnostics mirror sofarpc_invoke.
type ReplayOutput struct {
	Ok          bool           `json:"ok"`
	Source      string         `json:"source,omitempty"`
	Plan        *invoke.Plan   `json:"plan,omitempty"`
	Result      any            `json:"result,omitempty"`
	Diagnostics map[string]any `json:"diagnostics,omitempty"`
	Error       *errcode.Error `json:"error,omitempty"`
}

func registerReplay(server *sdkmcp.Server, opts Options) {
	sessions := opts.Sessions
	inputSchema, err := jsonschema.For[ReplayInput](nil)
	if err != nil {
		panic(fmt.Sprintf("infer replay input schema: %v", err))
	}
	server.AddTool(&sdkmcp.Tool{
		Name:        "sofarpc_replay",
		Description: "Replay a captured invocation. Accepts a payload from sofarpc_invoke's dryRun output, or a sessionId to look up a captured plan.",
		InputSchema: inputSchema,
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		in, payload, err := decodeReplayInput(req)
		if err != nil {
			out := ReplayOutput{Error: asErrcodeError(err)}
			return invokeToolResult(out, errorText("replay failed", err), true), nil
		}

		plan, source, err := extractPlan(in, payload, sessions)
		if err != nil {
			out := ReplayOutput{Error: asErrcodeError(err)}
			return invokeToolResult(out, errorText("replay failed", err), true), nil
		}

		if in.DryRun {
			out := ReplayOutput{Ok: true, Plan: plan, Source: source}
			return invokeToolResult(out, summarizeReplay(plan, source, true), false), nil
		}

		outcome, execErr := invoke.Execute(ctx, *plan, "replay")
		if execErr != nil {
			out := ReplayOutput{Plan: plan, Source: source, Diagnostics: outcome.Diagnostics, Error: asErrcodeError(execErr)}
			return invokeToolResult(out, errorText("replay failed", execErr), true), nil
		}
		out := ReplayOutput{
			Ok:          true,
			Plan:        plan,
			Source:      source,
			Result:      outcome.Result,
			Diagnostics: outcome.Diagnostics,
		}
		return invokeToolResult(out, summarizeReplay(plan, source, false), false), nil
	})
}

type rawReplayInput struct {
	SessionID string          `json:"sessionId,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	DryRun    bool            `json:"dryRun,omitempty"`
}

func decodeReplayInput(req *sdkmcp.CallToolRequest) (ReplayInput, json.RawMessage, error) {
	if req == nil || len(req.Params.Arguments) == 0 {
		return ReplayInput{}, nil, nil
	}
	var raw rawReplayInput
	dec := json.NewDecoder(bytes.NewReader(req.Params.Arguments))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return ReplayInput{}, nil, errcode.New(errcode.ArgsInvalid, "replay",
			fmt.Sprintf("parse tool arguments: %v", err))
	}
	return ReplayInput{
		SessionID: raw.SessionID,
		DryRun:    raw.DryRun,
	}, raw.Payload, nil
}

// extractPlan decides whether to load the plan from a session or from
// the supplied payload. Exactly one source must be set.
func extractPlan(in ReplayInput, payload json.RawMessage, sessions *SessionStore) (*invoke.Plan, string, error) {
	hasSession := in.SessionID != ""
	hasPayload := len(bytes.TrimSpace(payload)) > 0 && !bytes.Equal(bytes.TrimSpace(payload), []byte("null"))
	switch {
	case hasSession && hasPayload:
		return nil, "", errcode.New(errcode.ArgsInvalid, "replay",
			"sessionId and payload are mutually exclusive").
			WithHint("sofarpc_replay", nil, "pass exactly one of sessionId / payload")
	case !hasSession && !hasPayload:
		return nil, "", errcode.New(errcode.ArgsInvalid, "replay",
			"provide either sessionId or payload").
			WithHint("sofarpc_invoke", map[string]any{"dryRun": true},
				"run invoke with dryRun=true to produce a replayable plan")
	case hasSession:
		return planFromSession(in.SessionID, sessions)
	default:
		plan, err := planFromPayload(payload)
		return plan, "payload", err
	}
}

func planFromSession(id string, sessions *SessionStore) (*invoke.Plan, string, error) {
	if sessions == nil {
		return nil, "", errcode.New(errcode.ArgsInvalid, "replay",
			"no session store attached").
			WithHint("sofarpc_open", nil, "open a workspace first")
	}
	session, ok := sessions.Get(id)
	if !ok {
		return nil, "", errcode.New(errcode.ArgsInvalid, "replay",
			fmt.Sprintf("session %q not found", id)).
			WithHint("sofarpc_open", nil, "session ids are per-process; reopen the workspace")
	}
	if session.LastPlan == nil {
		return nil, "", errcode.New(errcode.ArgsInvalid, "replay",
			fmt.Sprintf("session %q has no captured invocation", id)).
			WithHint("sofarpc_invoke", map[string]any{"sessionId": id, "dryRun": true},
				"run invoke with this sessionId to capture a plan")
	}
	return session.LastPlan, "session", nil
}

func planFromPayload(payload json.RawMessage) (*invoke.Plan, error) {
	var plan invoke.Plan
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	if err := dec.Decode(&plan); err != nil {
		return nil, errcode.New(errcode.ArgsInvalid, "replay",
			fmt.Sprintf("payload is not a plan: %v", err)).
			WithHint("sofarpc_invoke", map[string]any{"dryRun": true},
				"produce a plan with invoke dryRun and pass it verbatim")
	}
	if plan.Service == "" || plan.Method == "" {
		return nil, errcode.New(errcode.ArgsInvalid, "replay",
			"payload is missing service or method").
			WithHint("sofarpc_invoke", map[string]any{"dryRun": true},
				"use invoke dryRun to get a valid plan shape")
	}
	if plan.Target.Mode == "" {
		return nil, errcode.New(errcode.TargetMissing, "replay",
			"payload has no target mode").
			WithHint("sofarpc_target", map[string]any{"explain": true},
				"resolve the target and re-plan")
	}
	return &plan, nil
}

func summarizeReplay(plan *invoke.Plan, source string, dryRun bool) string {
	prefix := "replay"
	if dryRun {
		prefix = "dry-run replay"
	}
	return fmt.Sprintf("%s (%s): %s.%s target=%s",
		prefix, source, plan.Service, plan.Method, targetAddr(plan.Target))
}
