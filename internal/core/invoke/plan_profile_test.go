package invoke

import (
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/invocationprops"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
)

// TestBuildPlan_ProfileInvocationPropertiesOverlayBase verifies the descending
// precedence input > local:profiles[P] > project:profiles[P] > local > project
// for invocation properties, and that the profile also supplies the endpoint.
func TestBuildPlan_ProfileInvocationPropertiesOverlayBase(t *testing.T) {
	callKey := "from-call"
	sharedProfileTenant := "test-tenant"
	baseTenant := "base-tenant"
	baseAppName := "demo"
	localUserID := "local-user"

	plan, err := BuildPlan(
		Input{
			Service:    "com.foo.Svc",
			Method:     "doThing",
			ParamTypes: []string{"java.lang.String"},
			Args:       []any{"hello"},
			Target:     target.Input{Profile: "test"},
			InvocationProperties: invocationprops.Declarations{
				"callKey": {Value: &callKey},
			},
		},
		nil,
		target.Sources{
			ProjectProfiles: map[string]target.Config{
				"test": {DirectURL: "bolt://test-host:12200"},
			},
			ProjectInvocationProperties: invocationprops.Declarations{
				"appName": {Value: &baseAppName},
				"tenant":  {Value: &baseTenant},
			},
			ProjectProfileInvocationProperties: map[string]invocationprops.Declarations{
				"test": {
					"tenant": {Value: &sharedProfileTenant},
					"userId": {Env: "SHARED_USER_ID"},
				},
			},
			ProjectLocalProfileInvocationProperties: map[string]invocationprops.Declarations{
				"test": {
					"userId": {Value: &localUserID},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	// Endpoint comes from the selected profile.
	if plan.Target.DirectURL != "bolt://test-host:12200" {
		t.Fatalf("DirectURL: got %q", plan.Target.DirectURL)
	}

	props := plan.InvocationProperties
	if got := props["callKey"].Value; got == nil || *got != "from-call" {
		t.Fatalf("callKey should come from input: %#v", props["callKey"])
	}
	if got := props["tenant"].Value; got == nil || *got != "test-tenant" {
		t.Fatalf("tenant should come from the profile, overriding base: %#v", props["tenant"])
	}
	if got := props["userId"].Value; got == nil || *got != "local-user" {
		t.Fatalf("userId should come from local profile, overriding shared profile env: %#v", props["userId"])
	}
	if got := props["appName"].Value; got == nil || *got != "demo" {
		t.Fatalf("appName should be inherited from base: %#v", props["appName"])
	}
}

func TestBuildPlan_UnknownProfileIsProfileNotDefined(t *testing.T) {
	_, err := BuildPlan(
		Input{
			Service:    "com.foo.Svc",
			Method:     "doThing",
			ParamTypes: []string{"java.lang.String"},
			Args:       []any{"hello"},
			Target:     target.Input{Profile: "prod"},
		},
		nil,
		target.Sources{
			ProjectProfiles: map[string]target.Config{
				"test": {DirectURL: "bolt://test-host:12200"},
			},
		},
	)
	if err == nil {
		t.Fatalf("expected error for undefined profile")
	}
	ecerr, ok := err.(*errcode.Error)
	if !ok {
		t.Fatalf("error type: got %T want *errcode.Error", err)
	}
	if ecerr.Code != errcode.ProfileNotDefined {
		t.Fatalf("code: got %q want %q", ecerr.Code, errcode.ProfileNotDefined)
	}
}

// TestBuildPlan_RecordsActiveProfileProvenance verifies the plan carries the
// resolved Active Target Profile (decision 9) for both a per-call selection and
// a DefaultProfile fallback, and stays empty when no profile is active.
func TestBuildPlan_RecordsActiveProfileProvenance(t *testing.T) {
	base := Input{
		Service:    "com.foo.Svc",
		Method:     "doThing",
		ParamTypes: []string{"java.lang.String"},
		Args:       []any{"hello"},
	}
	sources := target.Sources{
		DefaultProfile: "staging",
		ProjectProfiles: map[string]target.Config{
			"staging": {DirectURL: "bolt://staging-host:12200"},
			"test":    {DirectURL: "bolt://test-host:12200"},
		},
	}

	perCall := base
	perCall.Target = target.Input{Profile: "test"}
	plan, err := BuildPlan(perCall, nil, sources)
	if err != nil {
		t.Fatalf("BuildPlan per-call: %v", err)
	}
	if plan.Profile != "test" {
		t.Fatalf("plan.Profile per-call: got %q want test", plan.Profile)
	}

	plan, err = BuildPlan(base, nil, sources)
	if err != nil {
		t.Fatalf("BuildPlan default: %v", err)
	}
	if plan.Profile != "staging" {
		t.Fatalf("plan.Profile default: got %q want staging", plan.Profile)
	}

	plan, err = BuildPlan(base, nil, target.Sources{
		Project: target.Config{DirectURL: "bolt://base-host:12200"},
	})
	if err != nil {
		t.Fatalf("BuildPlan no-profile: %v", err)
	}
	if plan.Profile != "" {
		t.Fatalf("plan.Profile no-profile: got %q want empty", plan.Profile)
	}
}

// TestBuildPlan_DefaultProfileSuppliesInvocationProperties confirms that with no
// per-call profile, DefaultProfile drives both the endpoint and the profile's
// invocation properties.
func TestBuildPlan_DefaultProfileSuppliesInvocationProperties(t *testing.T) {
	tenant := "staging-tenant"

	plan, err := BuildPlan(
		Input{
			Service:    "com.foo.Svc",
			Method:     "doThing",
			ParamTypes: []string{"java.lang.String"},
			Args:       []any{"hello"},
			Target:     target.Input{}, // no per-call profile
		},
		nil,
		target.Sources{
			DefaultProfile: "staging",
			ProjectProfiles: map[string]target.Config{
				"staging": {DirectURL: "bolt://staging-host:12200"},
			},
			ProjectProfileInvocationProperties: map[string]invocationprops.Declarations{
				"staging": {"tenant": {Value: &tenant}},
			},
		},
	)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.Target.DirectURL != "bolt://staging-host:12200" {
		t.Fatalf("DirectURL: got %q", plan.Target.DirectURL)
	}
	if got := plan.InvocationProperties["tenant"].Value; got == nil || *got != "staging-tenant" {
		t.Fatalf("tenant should come from default profile: %#v", plan.InvocationProperties["tenant"])
	}
}
