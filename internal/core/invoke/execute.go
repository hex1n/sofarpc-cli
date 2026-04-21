package invoke

import (
	"context"
	"fmt"

	"github.com/hex1n/sofarpc-cli/internal/errcode"
)

type Outcome struct {
	Result      any
	Diagnostics map[string]any
}

// Execute runs a plan through the pure-Go direct transport. Plans that
// don't match the supported direct shape fail with a structured error.
func Execute(ctx context.Context, plan Plan, phase string) (Outcome, error) {
	if direct, err := ExecuteDirectIfPossible(ctx, plan, phase); direct.Handled {
		return Outcome{
			Result:      direct.Result,
			Diagnostics: direct.Diagnostics,
		}, err
	}

	return Outcome{}, unsupportedTargetError(phase, plan)
}

func unsupportedTargetError(phase string, plan Plan) *errcode.Error {
	return errcode.New(errcode.InvocationRejected, phase,
		fmt.Sprintf("pure-Go invoke supports only direct+bolt+hessian2; got mode=%s protocol=%s serialization=%s",
			plan.Target.Mode, plan.Target.Protocol, plan.Target.Serialization)).
		WithHint("sofarpc_doctor", nil,
			"doctor shows whether the resolved target fits the direct invoke path")
}
