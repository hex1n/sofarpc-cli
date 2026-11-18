package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
)

type planExecutionResult struct {
	Outcome  invoke.Outcome
	Err      error
	Rejected bool
}

func executePlanWithPolicy(ctx context.Context, plan invoke.Plan, phase string, sources target.Sources, profile string, capture *PlanCaptureResult, limiter *InvokeLimiter) planExecutionResult {
	policy := executionPolicyFromEnv(sources)
	policy.RequestedProfile = profile
	if err := policy.Validate(plan, phase); err != nil {
		return planExecutionResult{
			Err:      err,
			Rejected: true,
			Outcome: invoke.Outcome{
				Diagnostics: diagnosticsWithCapture(targetConfigDiagnostics(sources), capture),
			},
		}
	}
	release, limiterDiagnostics, err := limiter.Acquire(ctx, phase, plan)
	if err != nil {
		return planExecutionResult{
			Err:      err,
			Rejected: true,
			Outcome: invoke.Outcome{
				Diagnostics: diagnosticsWithCapture(diagnosticsWithLimiter(targetConfigDiagnostics(sources), limiterDiagnostics), capture),
			},
		}
	}
	defer release()

	outcome, err := invoke.Execute(ctx, plan, phase)
	outcome.Diagnostics = diagnosticsWithCapture(diagnosticsWithLimiter(outcome.Diagnostics, limiterDiagnostics), capture)
	return planExecutionResult{Outcome: outcome, Err: err}
}

func planExecutionErrorText(phase string, result planExecutionResult, err *errcode.Error) string {
	prefix := phase + " failed"
	if result.Rejected {
		prefix = phase + " rejected"
	}
	text := errorText(prefix, err)
	for _, key := range []string{"stage", "requestState", "traceId"} {
		if value, ok := result.Outcome.Diagnostics[key].(string); ok && value != "" {
			text += " " + key + "=" + value
		}
	}
	return text
}

type diagnosticScope struct {
	project   string
	profile   string
	sessionID string
	service   string
	method    string
}

func (scope diagnosticScope) withHintScope(err *errcode.Error) *errcode.Error {
	if err == nil || err.Hint == nil {
		return err
	}
	switch err.Hint.NextTool {
	case "sofarpc_target", "sofarpc_doctor", "sofarpc_describe", "sofarpc_invoke":
	default:
		return err
	}
	args := make(map[string]any, len(err.Hint.NextArgs)+5)
	maps.Copy(args, err.Hint.NextArgs)
	for key, value := range map[string]string{
		"project": scope.project, "sessionId": scope.sessionID, "service": scope.service,
	} {
		if strings.TrimSpace(value) != "" {
			args[key] = value
		}
	}
	switch err.Hint.NextTool {
	case "sofarpc_describe":
		if scope.method != "" {
			args["method"] = scope.method
		}
	case "sofarpc_invoke", "sofarpc_doctor", "sofarpc_target":
		if scope.profile != "" {
			args["profile"] = scope.profile
		}
		if err.Hint.NextTool == "sofarpc_invoke" && scope.method != "" {
			args["method"] = scope.method
		}
	}
	scoped := *err
	return scoped.WithHint(err.Hint.NextTool, args, err.Hint.Reason)
}

func preflightPlan(plan invoke.Plan, phase string, sources target.Sources, scope diagnosticScope) invoke.Preflight {
	policy := executionPolicyFromEnv(sources)
	policy.RequestedProfile = scope.profile
	check := policy.Preflight(plan, phase)
	check.PolicyError = scope.withHintScope(check.PolicyError)
	check.ExecutionError = scope.withHintScope(check.ExecutionError)
	return check
}

func preflightText(check invoke.Preflight) string {
	text := fmt.Sprintf("\npreflight.ready=%t tcp=not-checked", check.Ready)
	if check.PolicyError != nil {
		text += "\npolicy: " + errorText("preflight policy failed", check.PolicyError)
	}
	if check.ExecutionError != nil {
		text += "\nexecution: " + errorText("preflight execution failed", check.ExecutionError)
	}
	return text
}

func diagnosticHintText(tool string, args map[string]any) string {
	if tool == "" {
		return ""
	}
	text := " nextTool=" + tool
	safeArgs := make(map[string]any)
	for _, key := range []string{"project", "profile", "sessionId", "planId", "service", "method", "dryRun", "explain"} {
		switch value := args[key].(type) {
		case string, bool:
			safeArgs[key] = value
		}
	}
	if len(safeArgs) > 0 {
		body, _ := json.Marshal(safeArgs)
		text += " nextArgs=" + string(body)
	}
	return text
}

func executionPolicyFromEnv(sources target.Sources) invoke.ExecutionPolicy {
	return invoke.ExecutionPolicy{
		AllowInvoke:         invoke.EnvFlag(envAllowInvoke),
		AllowTargetOverride: invoke.EnvFlag(envAllowTargetOverride),
		AllowedTargetHosts:  invoke.EnvList(envAllowedTargetHosts),
		Sources:             sources,
	}
}

func capturePlanForSession(sessions *SessionStore, sessionID string, plan invoke.Plan) *PlanCaptureResult {
	if sessions == nil || sessionID == "" {
		return nil
	}
	capture := sessions.CapturePlan(sessionID, plan)
	return &capture
}

// diagnosticsWithCapture attaches the session capture outcome whenever a
// capture was attempted: failures explain why sessionId replay won't work,
// and successes carry the planId handle for later planId replay.
func diagnosticsWithCapture(base map[string]any, capture *PlanCaptureResult) map[string]any {
	if capture == nil {
		return base
	}
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	out["sessionPlanCapture"] = capture
	return out
}

func diagnosticsWithLimiter(base map[string]any, diag InvokeLimiterDiagnostics) map[string]any {
	if !diag.Enabled && diag.GlobalLimit == 0 && diag.PerTargetLimit == 0 {
		return base
	}
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	out["invokeConcurrency"] = diag
	return out
}
