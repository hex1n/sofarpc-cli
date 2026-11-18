package mcp

import (
	"context"

	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
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

func planExecutionErrorText(phase string, result planExecutionResult) string {
	prefix := phase + " failed"
	if result.Rejected {
		prefix = phase + " rejected"
	}
	return errorText(prefix, result.Err)
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
