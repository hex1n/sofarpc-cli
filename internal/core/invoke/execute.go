package invoke

import (
	"context"
	"errors"
	"fmt"

	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
)

type Outcome struct {
	Result      any
	Diagnostics map[string]any
}

// Preflight reports the local policy and plan checks for a dry-run.
type Preflight struct {
	Ready          bool           `json:"ready"`
	PolicyError    *errcode.Error `json:"policyError,omitempty"`
	ExecutionError *errcode.Error `json:"executionError,omitempty"`
}

// Preflight checks the plan without acquiring an invocation slot or opening a connection.
func (p ExecutionPolicy) Preflight(plan Plan, phase string) Preflight {
	policyErr := p.Validate(plan, phase)
	executionErr := ValidateExecutablePlan(plan, phase)
	if executionErr == nil {
		if _, err := target.ParseDirectDialAddress(plan.Target.DirectURL); err != nil {
			executionErr = targetInvalidError(phase, fmt.Sprintf("invalid directUrl %q: %v", plan.Target.DirectURL, err))
		}
	}
	return Preflight{
		Ready:          policyErr == nil && executionErr == nil,
		PolicyError:    preflightError(phase, policyErr),
		ExecutionError: preflightError(phase, executionErr),
	}
}

func preflightError(phase string, err error) *errcode.Error {
	if err == nil {
		return nil
	}
	var structured *errcode.Error
	if errors.As(err, &structured) {
		return structured
	}
	return errcode.New(errcode.InvocationRejected, phase, err.Error())
}

// Execute runs a plan through the pure-Go direct transport. Plans that
// don't match the supported direct shape fail with a structured error.
func Execute(ctx context.Context, plan Plan, phase string) (Outcome, error) {
	if err := ValidateExecutablePlan(plan, phase); err != nil {
		return Outcome{}, err
	}
	if direct, err := ExecuteDirectIfPossible(ctx, plan, phase); direct.Handled {
		return Outcome{
			Result:      direct.Result,
			Diagnostics: direct.Diagnostics,
		}, err
	}

	return Outcome{}, unsupportedTargetError(phase, plan)
}

func unsupportedTargetError(phase string, plan Plan) *errcode.Error {
	message := fmt.Sprintf("pure-Go invoke supports only direct+bolt+hessian2; got mode=%s protocol=%s serialization=%s",
		plan.Target.Mode, plan.Target.Protocol, plan.Target.Serialization)
	if target.IsTRDirectURL(plan.Target.DirectURL) {
		message = "directUrl uses the tr:// scheme. Pure-Go invoke supports only direct+bolt+hessian2."
	}
	return errcode.New(errcode.InvocationRejected, phase, message).
		WithHint("sofarpc_target", map[string]any{"explain": true},
			"inspect the resolved target protocol and serialization")
}
