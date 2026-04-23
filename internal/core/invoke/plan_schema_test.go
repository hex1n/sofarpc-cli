package invoke

import (
	"errors"
	"testing"

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

func assertPlanVersionUnsupported(t *testing.T, err error) {
	t.Helper()
	var ecerr *errcode.Error
	if !errors.As(err, &ecerr) {
		t.Fatalf("expected *errcode.Error, got %T: %v", err, err)
	}
	if ecerr.Code != errcode.PlanVersionUnsupported {
		t.Fatalf("code = %s, want %s", ecerr.Code, errcode.PlanVersionUnsupported)
	}
	if ecerr.Hint == nil || ecerr.Hint.NextTool != "sofarpc_invoke" {
		t.Fatalf("expected invoke recovery hint, got %#v", ecerr.Hint)
	}
}
