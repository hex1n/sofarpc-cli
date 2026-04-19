package mcp

import (
	"context"
	"encoding/json"
	"errors"
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
	client := opts.Worker
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "sofarpc_replay",
		Description: "Replay a captured invocation. Accepts a payload from sofarpc_invoke's dryRun output, or a sessionId to look up a captured plan.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in ReplayInput) (*sdkmcp.CallToolResult, ReplayOutput, error) {
		plan, source, err := extractPlan(in, sessions)
		if err != nil {
			out := ReplayOutput{Error: asErrcodeError(err)}
			return errorReplayResult(err), out, nil
		}

		if in.DryRun {
			out := ReplayOutput{Ok: true, Plan: plan, Source: source}
			return &sdkmcp.CallToolResult{
				Content: []sdkmcp.Content{
					&sdkmcp.TextContent{Text: summarizeReplay(plan, source, true)},
				},
			}, out, nil
		}

		resp, werr := client.Invoke(ctx, planToWireRequest(*plan))
		if werr != nil {
			out := ReplayOutput{Plan: plan, Source: source, Error: asErrcodeError(werr)}
			return errorReplayResult(werr), out, nil
		}
		out := ReplayOutput{
			Ok:          true,
			Plan:        plan,
			Source:      source,
			Result:      resp.Result,
			Diagnostics: resp.Diagnostics,
		}
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: summarizeReplay(plan, source, false)},
			},
		}, out, nil
	})
}

// extractPlan decides whether to load the plan from a session or from
// the supplied payload. Exactly one source must be set.
func extractPlan(in ReplayInput, sessions *SessionStore) (*invoke.Plan, string, error) {
	hasSession := in.SessionID != ""
	hasPayload := in.Payload != nil
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
		plan, err := planFromPayload(in.Payload)
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

func planFromPayload(payload any) (*invoke.Plan, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errcode.New(errcode.ArgsInvalid, "replay",
			fmt.Sprintf("payload not JSON-serialisable: %v", err))
	}
	var plan invoke.Plan
	if err := json.Unmarshal(body, &plan); err != nil {
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

func errorReplayResult(err error) *sdkmcp.CallToolResult {
	text := "replay failed"
	var ecerr *errcode.Error
	if errors.As(err, &ecerr) {
		text = fmt.Sprintf("%s: %s", ecerr.Code, ecerr.Message)
	} else if err != nil {
		text = err.Error()
	}
	return &sdkmcp.CallToolResult{
		IsError: true,
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: text}},
	}
}

func summarizeReplay(plan *invoke.Plan, source string, dryRun bool) string {
	prefix := "replay"
	if dryRun {
		prefix = "dry-run replay"
	}
	return fmt.Sprintf("%s (%s): %s.%s target=%s",
		prefix, source, plan.Service, plan.Method, targetAddr(plan.Target))
}
