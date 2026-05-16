package invoke

import (
	"errors"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
)

func TestValidatePlanSchemaAcceptsCurrentVersion(t *testing.T) {
	t.Parallel()

	if err := ValidatePlanSchema(Plan{SchemaVersion: PlanSchemaVersion}, "replay"); err != nil {
		t.Fatalf("ValidatePlanSchema: %v", err)
	}
}

func TestValidatePlanSchemaRejectsMissingVersion(t *testing.T) {
	t.Parallel()

	err := ValidatePlanSchema(Plan{}, "replay")
	assertPlanVersionUnsupported(t, err)
}

func TestValidatePlanSchemaRejectsUnsupportedVersion(t *testing.T) {
	t.Parallel()

	err := ValidatePlanSchema(Plan{SchemaVersion: "sofarpc.invoke.plan/v999"}, "replay")
	assertPlanVersionUnsupported(t, err)
}

func TestValidateReplayPlanAcceptsReplayablePlan(t *testing.T) {
	t.Parallel()

	err := ValidateReplayPlan(replayablePlan(), "replay")
	if err != nil {
		t.Fatalf("ValidateReplayPlan: %v", err)
	}
}

func TestValidateReplayPlanRejectsMissingServiceOrMethod(t *testing.T) {
	t.Parallel()

	plan := replayablePlan()
	plan.Service = ""
	err := ValidateReplayPlan(plan, "replay")
	assertErrcode(t, err, errcode.ArgsInvalid)
}

func TestValidateReplayPlanRejectsArityMismatch(t *testing.T) {
	t.Parallel()

	plan := replayablePlan()
	plan.Args = nil
	err := ValidateReplayPlan(plan, "replay")
	ecerr := assertErrcode(t, err, errcode.ArgsInvalid)
	if !strings.Contains(ecerr.Message, "arity mismatch") {
		t.Fatalf("message should explain arity mismatch: %q", ecerr.Message)
	}
}

func TestValidateReplayPlanRejectsMissingTargetMode(t *testing.T) {
	t.Parallel()

	plan := replayablePlan()
	plan.Target.Mode = ""
	err := ValidateReplayPlan(plan, "replay")
	assertErrcode(t, err, errcode.TargetMissing)
}

func TestValidateExecutablePlanRejectsUnsupportedTarget(t *testing.T) {
	t.Parallel()

	plan := replayablePlan()
	plan.Target = target.Config{
		Mode:            target.ModeRegistry,
		RegistryAddress: "zookeeper://h:1",
	}
	err := ValidateExecutablePlan(plan, "invoke")
	assertErrcode(t, err, errcode.InvocationRejected)
}

func replayablePlan() Plan {
	return Plan{
		SchemaVersion: PlanSchemaVersion,
		Service:       "com.foo.Svc",
		Method:        "doThing",
		ParamTypes:    []string{"java.lang.String"},
		Args:          []any{"hello"},
		Target: target.Config{
			Mode:      target.ModeDirect,
			DirectURL: "bolt://h:1",
		},
	}
}

func assertPlanVersionUnsupported(t *testing.T, err error) {
	t.Helper()
	ecerr := assertErrcode(t, err, errcode.PlanVersionUnsupported)
	if ecerr.Hint == nil || ecerr.Hint.NextTool != "sofarpc_invoke" {
		t.Fatalf("expected invoke recovery hint, got %#v", ecerr.Hint)
	}
}

func assertErrcode(t *testing.T, err error, want errcode.Code) *errcode.Error {
	t.Helper()
	var ecerr *errcode.Error
	if !errors.As(err, &ecerr) {
		t.Fatalf("expected *errcode.Error, got %T: %v", err, err)
	}
	if ecerr.Code != want {
		t.Fatalf("code = %s, want %s", ecerr.Code, want)
	}
	return ecerr
}
