package invoke

import (
	"errors"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
)

func policyErr(t *testing.T, err error) *errcode.Error {
	t.Helper()
	if err == nil {
		t.Fatal("expected a rejection, got nil")
	}
	var ecerr *errcode.Error
	if !errors.As(err, &ecerr) {
		t.Fatalf("every policy rejection must be an *errcode.Error, got %T", err)
	}
	return ecerr
}

// ValidateTargetResolved judges the normalized target, so a config that only
// carries a direct URL counts as resolved and a bare mode does not.
func TestValidateTargetResolvedUsesNormalizedTarget(t *testing.T) {
	if err := ValidateTargetResolved(target.Config{DirectURL: "bolt://127.0.0.1:12200"}, "invoke"); err != nil {
		t.Fatalf("a direct URL alone resolves a target: %v", err)
	}
	ecerr := policyErr(t, ValidateTargetResolved(target.Config{Mode: target.ModeDirect}, "invoke"))
	if ecerr.Code != errcode.TargetMissing {
		t.Fatalf("a mode without a direct URL is not resolved, got %s", ecerr.Code)
	}
	if ecerr.Hint == nil || ecerr.Hint.NextTool != "sofarpc_target" {
		t.Fatalf("the missing-target hint should point at sofarpc_target, got %+v", ecerr.Hint)
	}
}

func TestValidateTargetResolvedDefaultsPhase(t *testing.T) {
	if got := policyErr(t, ValidateTargetResolved(target.Config{}, "  ")).Phase; got != "invoke" {
		t.Fatalf("a blank phase defaults to invoke, got %q", got)
	}
	if got := policyErr(t, ValidateTargetResolved(target.Config{}, "doctor")).Phase; got != "doctor" {
		t.Fatalf("an explicit phase is preserved, got %q", got)
	}
}

func TestValidateTargetTransportRejectsNonBolt(t *testing.T) {
	supported := target.Config{DirectURL: "bolt://127.0.0.1:12200"}
	if err := ValidateTargetTransport(supported, "invoke"); err != nil {
		t.Fatalf("direct+bolt+hessian2 is executable: %v", err)
	}
	unsupported := supported
	unsupported.Protocol = "tr"
	ecerr := policyErr(t, ValidateTargetTransport(unsupported, "invoke"))
	if !strings.Contains(ecerr.Message, "pure-Go invoke supports only") {
		t.Fatalf("the message should name the transport rule, got %q", ecerr.Message)
	}
	if ecerr.Hint == nil || ecerr.Hint.NextTool == "sofarpc_doctor" {
		t.Fatalf("the transport hint must be actionable, not a round trip to doctor, got %+v", ecerr.Hint)
	}
}

// ValidateExecutablePlan layers the replay schema rules ahead of the
// transport rule. The plan-shape check owns "no target mode" for a replay
// payload, so a plan missing it never reaches the transport judgement.
// ValidateTargetResolved is deliberately not in this path: it judges a
// resolved config, while this gate judges a plan handed to it verbatim.
func TestValidateExecutablePlanLayersSchemaBeforeTransport(t *testing.T) {
	plan := replayablePlan()
	plan.Target = target.Config{Protocol: "tr"}
	ecerr := policyErr(t, ValidateExecutablePlan(plan, "invoke"))
	if ecerr.Code != errcode.TargetMissing {
		t.Fatalf("resolution is judged before transport, got %s", ecerr.Code)
	}

	plan.Target = target.Config{Mode: target.ModeDirect, DirectURL: "bolt://127.0.0.1:12200", Protocol: "tr"}
	if got := policyErr(t, ValidateExecutablePlan(plan, "invoke")).Message; !strings.Contains(got, "pure-Go invoke supports only") {
		t.Fatalf("a resolved target is judged by the transport rule, got %q", got)
	}

	plan.Target = target.Config{Mode: target.ModeDirect, DirectURL: "bolt://127.0.0.1:12200"}
	if err := ValidateExecutablePlan(plan, "invoke"); err != nil {
		t.Fatalf("a supported direct plan passes both rules: %v", err)
	}
}

// Every rejection must hand the agent a tool that moves it forward.
func TestPolicyHintsAreActionable(t *testing.T) {
	sources := target.Sources{ProjectRoot: "/tmp/project"}
	policy := ExecutionPolicy{
		AllowInvoke:         true,
		AllowedTargetHosts:  []string{"other.example.internal"},
		AllowTargetOverride: true,
		Sources:             sources,
	}
	direct := target.Config{DirectURL: "bolt://127.0.0.1:12200"}
	rejections := map[string]error{
		"disabled":  ExecutionPolicy{Sources: sources}.ValidateRealInvokeEnabled("invoke"),
		"host":      policy.ValidateTarget(Plan{Target: direct}, "invoke"),
		"transport": ValidateTargetTransport(target.Config{DirectURL: "bolt://127.0.0.1:12200", Protocol: "tr"}, "invoke"),
		"resolved":  ValidateTargetResolved(target.Config{}, "invoke"),
	}
	for name, err := range rejections {
		hint := policyErr(t, err).Hint
		if hint == nil || hint.NextTool == "" {
			t.Fatalf("%s: a rejection must carry an actionable hint, got %+v", name, hint)
		}
		if hint.NextTool == "sofarpc_doctor" {
			t.Fatalf("%s: doctor cannot resolve its own rejection", name)
		}
	}
}

func TestHintArgsCarryProjectRootOnlyWhenKnown(t *testing.T) {
	scoped := target.Sources{ProjectRoot: "/tmp/project"}
	if got := TargetHintArgs(scoped); got["explain"] != true || got["project"] != "/tmp/project" {
		t.Fatalf("TargetHintArgs should explain within the project, got %+v", got)
	}
	if got := TargetHintArgs(target.Sources{ProjectRoot: "  "}); got["project"] != nil {
		t.Fatalf("a blank project root should be omitted, got %+v", got)
	}
	first := TargetHintArgs(scoped)
	first["explain"] = false
	if TargetHintArgs(scoped)["explain"] != true {
		t.Fatal("each call must return a fresh args map")
	}
}
