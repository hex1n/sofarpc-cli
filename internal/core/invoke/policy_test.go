package invoke

import (
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
)

func TestExecutionPolicyRejectsDisabledRealInvoke(t *testing.T) {
	t.Parallel()

	err := ExecutionPolicy{}.ValidateRealInvoke("com.foo.Svc", "invoke")
	assertErrcode(t, err, errcode.InvocationRejected)
}

func TestExecutionPolicyRespectsAllowedServices(t *testing.T) {
	t.Parallel()

	policy := ExecutionPolicy{
		AllowInvoke:     true,
		AllowedServices: []string{"com.foo.AllowedFacade", "com.foo.OtherFacade"},
	}
	if err := policy.ValidateRealInvoke("com.foo.AllowedFacade", "invoke"); err != nil {
		t.Fatalf("allowed service rejected: %v", err)
	}

	err := policy.ValidateRealInvoke("com.foo.BlockedFacade", "invoke")
	assertErrcode(t, err, errcode.InvocationRejected)
}

func TestExecutionPolicyRejectsDirectTargetOverrideByDefault(t *testing.T) {
	t.Parallel()

	err := ExecutionPolicy{AllowInvoke: true}.Validate(samplePolicyPlan("bolt://override.example:12200"), "invoke")
	assertErrcode(t, err, errcode.InvocationRejected)
}

func TestExecutionPolicyAllowsResolvedProjectTargetByDefault(t *testing.T) {
	t.Parallel()

	policy := ExecutionPolicy{
		AllowInvoke: true,
		Sources: target.Sources{
			ProjectLocal: target.Config{DirectURL: "bolt://project.example:12200"},
		},
	}
	err := policy.Validate(samplePolicyPlan("bolt://project.example:12200"), "invoke")
	if err != nil {
		t.Fatalf("project target should be allowed: %v", err)
	}
}

func TestExecutionPolicyRejectsProjectConfigErrors(t *testing.T) {
	t.Parallel()

	policy := ExecutionPolicy{
		AllowInvoke: true,
		Sources: target.Sources{
			ProjectLocal: target.Config{DirectURL: "bolt://project.example:12200"},
			ConfigErrors: []target.ConfigError{{Path: ".sofarpc/config.json", Error: "bad json"}},
		},
	}
	err := policy.Validate(samplePolicyPlan("bolt://project.example:12200"), "invoke")
	assertErrcode(t, err, errcode.InvocationRejected)
}

func TestExecutionPolicyRespectsAllowedTargetHosts(t *testing.T) {
	t.Parallel()

	policy := ExecutionPolicy{
		AllowInvoke:         true,
		AllowTargetOverride: true,
		AllowedTargetHosts:  []string{"allowed.example", "127.0.0.1:12200"},
	}
	err := policy.Validate(samplePolicyPlan("bolt://blocked.example:12200"), "replay")
	ecerr := assertErrcode(t, err, errcode.InvocationRejected)
	if ecerr.Phase != "replay" {
		t.Fatalf("phase = %q, want replay", ecerr.Phase)
	}
	if !strings.Contains(ecerr.Message, EnvAllowedTargetHosts) {
		t.Fatalf("message should mention %s: %q", EnvAllowedTargetHosts, ecerr.Message)
	}

	err = policy.Validate(samplePolicyPlan("bolt://allowed.example:12200"), "invoke")
	if err != nil {
		t.Fatalf("allowed target host rejected: %v", err)
	}
}

func samplePolicyPlan(directURL string) Plan {
	return Plan{
		SchemaVersion: PlanSchemaVersion,
		Service:       "com.foo.Svc",
		Method:        "doThing",
		ParamTypes:    []string{"java.lang.String"},
		Args:          []any{"hello"},
		Target: target.Config{
			Mode:      target.ModeDirect,
			DirectURL: directURL,
		},
	}
}
