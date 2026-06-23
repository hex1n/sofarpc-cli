package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

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
	sources := opts.TargetSources
	server.AddTool(&sdkmcp.Tool{
		Name:         "sofarpc_replay",
		Title:        "Replay SOFARPC Invocation",
		Description:  "Replay a captured invocation. Accepts a payload from sofarpc_invoke's dryRun output, or a sessionId to look up a captured plan. Replay requires a supported plan schemaVersion.",
		Annotations:  remoteInvokeAnnotations("Replay SOFARPC Invocation"),
		InputSchema:  replayInputSchema(),
		OutputSchema: replayOutputSchema(),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		notifyToolProgress(ctx, req, 0, 4, "decoding replay input")
		in, payload, err := decodeReplayInput(req)
		if err != nil {
			out := ReplayOutput{Error: asErrcodeError(err)}
			return invokeToolResult(out, errorText("replay failed", err), true), nil
		}

		notifyToolProgress(ctx, req, 1, 4, "extracting replay plan")
		plan, source, err := extractPlan(in, payload, sessions)
		if err != nil {
			out := ReplayOutput{Error: asErrcodeError(err)}
			return invokeToolResult(out, errorText("replay failed", err), true), nil
		}

		if in.DryRun {
			out := ReplayOutput{Ok: true, Plan: plan, Source: source}
			notifyToolProgress(ctx, req, 4, 4, "replay dry-run complete")
			return invokeToolResultWithLinks(out, summarizeReplay(plan, source, true), false, replayResourceLinks(in.SessionID, source)...), nil
		}

		notifyToolProgress(ctx, req, 2, 4, "resolving replay safety scope")
		scope, scopeErr := resolveToolScope(sources, sessions, in.SessionID, in.Cwd, in.Project)
		if scopeErr != nil {
			ecerr := errcode.New(errcode.ArgsInvalid, "replay", scopeErr.Error())
			out := ReplayOutput{Plan: plan, Source: source, Error: ecerr}
			return invokeToolResult(out, errorText("replay failed", ecerr), true), nil
		}
		toolSources := scope.Sources
		notifyToolProgress(ctx, req, 3, 4, "executing replay plan")
		execution := executePlanWithPolicy(ctx, *plan, "replay", toolSources, nil)
		if execution.Err != nil {
			out := ReplayOutput{Plan: plan, Source: source, Diagnostics: execution.Outcome.Diagnostics, Error: asErrcodeError(execution.Err)}
			return invokeToolResultWithLinks(out, planExecutionErrorText("replay", execution), true, replayResourceLinks(in.SessionID, source)...), nil
		}
		out := ReplayOutput{
			Ok:          true,
			Plan:        plan,
			Source:      source,
			Result:      execution.Outcome.Result,
			Diagnostics: execution.Outcome.Diagnostics,
		}
		notifyToolProgress(ctx, req, 4, 4, "replay complete")
		return invokeToolResultWithLinks(out, summarizeReplay(plan, source, false), false, replayResourceLinks(in.SessionID, source)...), nil
	})
}

type rawReplayInput struct {
	SessionID string          `json:"sessionId,omitempty"`
	Cwd       string          `json:"cwd,omitempty"`
	Project   string          `json:"project,omitempty"`
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
		Cwd:       raw.Cwd,
		Project:   raw.Project,
		DryRun:    raw.DryRun,
	}, raw.Payload, nil
}

// extractPlan decides whether to load the plan from a session or from
// the supplied payload. When both are present, payload supplies the plan
// and sessionId supplies workspace/safety context.
func extractPlan(in ReplayInput, payload json.RawMessage, sessions *SessionStore) (*invoke.Plan, string, error) {
	hasSession := in.SessionID != ""
	hasPayload := len(bytes.TrimSpace(payload)) > 0 && !bytes.Equal(bytes.TrimSpace(payload), []byte("null"))
	switch {
	case hasSession && hasPayload:
		plan, err := planFromPayload(payload)
		return plan, "payload", err
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
	if err := invoke.ValidateReplayPlan(*session.LastPlan, "replay"); err != nil {
		return nil, "", err
	}
	return session.LastPlan, "session", nil
}

func planFromPayload(payload json.RawMessage) (*invoke.Plan, error) {
	planPayload, err := replayPlanPayload(payload)
	if err != nil {
		return nil, err
	}
	var plan invoke.Plan
	dec := json.NewDecoder(bytes.NewReader(planPayload))
	dec.UseNumber()
	if err := dec.Decode(&plan); err != nil {
		return nil, errcode.New(errcode.ArgsInvalid, "replay",
			fmt.Sprintf("payload is not a plan: %v", err)).
			WithHint("sofarpc_invoke", map[string]any{"dryRun": true},
				"produce a plan with invoke dryRun and pass it verbatim")
	}
	if err := invoke.ValidateReplayPlan(plan, "replay"); err != nil {
		return nil, err
	}
	return &plan, nil
}

func replayPlanPayload(payload json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(payload)
	var envelope struct {
		Plan              json.RawMessage `json:"plan,omitempty"`
		StructuredContent struct {
			Plan json.RawMessage `json:"plan,omitempty"`
		} `json:"structuredContent,omitempty"`
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	if err := dec.Decode(&envelope); err != nil {
		return payload, nil
	}
	switch {
	case len(envelope.Plan) > 0:
		return nonNullReplayPlanPayload(envelope.Plan)
	case len(envelope.StructuredContent.Plan) > 0:
		return nonNullReplayPlanPayload(envelope.StructuredContent.Plan)
	default:
		return payload, nil
	}
}

func nonNullReplayPlanPayload(payload json.RawMessage) (json.RawMessage, error) {
	if bytes.Equal(bytes.TrimSpace(payload), []byte("null")) {
		return nil, errcode.New(errcode.ArgsInvalid, "replay",
			"payload envelope contains a null plan").
			WithHint("sofarpc_invoke", map[string]any{"dryRun": true},
				"pass the plan from invoke dryRun output")
	}
	return payload, nil
}

func summarizeReplay(plan *invoke.Plan, source string, dryRun bool) string {
	prefix := "replay"
	if dryRun {
		prefix = "dry-run replay"
	}
	summary := fmt.Sprintf("%s (%s): %s.%s target=%s",
		prefix, source, plan.Service, plan.Method, targetAddr(plan.Target))
	if plan.Profile != "" {
		summary += " profile=" + plan.Profile
	}
	return summary
}
