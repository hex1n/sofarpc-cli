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

func executePlanWithPolicy(ctx context.Context, plan invoke.Plan, phase string, sources target.Sources, capture *PlanCaptureResult) planExecutionResult {
	if err := validateExecutionPolicy(plan, phase, sources); err != nil {
		return planExecutionResult{
			Err:      err,
			Rejected: true,
			Outcome: invoke.Outcome{
				Diagnostics: diagnosticsWithCapture(targetConfigDiagnostics(sources), capture),
			},
		}
	}
	outcome, err := invoke.Execute(ctx, plan, phase)
	outcome.Diagnostics = diagnosticsWithCapture(outcome.Diagnostics, capture)
	return planExecutionResult{Outcome: outcome, Err: err}
}

func planExecutionErrorText(phase string, result planExecutionResult) string {
	prefix := phase + " failed"
	if result.Rejected {
		prefix = phase + " rejected"
	}
	return errorText(prefix, result.Err)
}

func validateExecutionPolicy(plan invoke.Plan, phase string, sources target.Sources) error {
	return executionPolicyFromEnv(sources).Validate(plan, phase)
}

func validateRealInvoke(service string) error {
	return executionPolicyFromEnv(target.Sources{}).ValidateRealInvoke(service, "invoke")
}

func executionPolicyFromEnv(sources target.Sources) invoke.ExecutionPolicy {
	allowedServices, allowedServicesConfigured, allowedServicesSource := allowedServicesForSources(sources)
	return invoke.ExecutionPolicy{
		AllowInvoke:               invoke.EnvFlag(envAllowInvoke),
		AllowedServices:           allowedServices,
		AllowedServicesConfigured: allowedServicesConfigured,
		AllowedServicesSource:     allowedServicesSource,
		AllowTargetOverride:       invoke.EnvFlag(envAllowTargetOverride),
		AllowedTargetHosts:        invoke.EnvList(envAllowedTargetHosts),
		Sources:                   sources,
	}
}

func allowedServicesForSources(sources target.Sources) ([]string, bool, string) {
	allowlist := target.ServiceAllowlistForSources(sources)
	if allowlist.Configured {
		return allowlist.Services, true, allowlist.Source
	}
	return nil, false, ""
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
