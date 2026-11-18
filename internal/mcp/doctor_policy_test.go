package mcp

import (
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
)

func policyDoctorSources() target.Sources {
	return target.Sources{
		Env: target.Config{DirectURL: "bolt://127.0.0.1:1"},
	}
}

func invokePolicyCheck(t *testing.T, sources target.Sources, args map[string]any) DoctorCheck {
	t.Helper()
	out := callDoctor(t, Options{TargetSources: sources}, args)
	return findCheck(t, out, "invoke-policy")
}

// projectDoctorCheck runs doctor against an on-disk project whose
// .sofarpc/config.json holds body, with env as the ambient target layer.
func projectDoctorCheck(t *testing.T, body string, env target.Config, args map[string]any) DoctorCheck {
	t.Helper()
	dir := t.TempDir()
	writeOpenFile(t, dir, ".sofarpc/config.json", body)
	if args == nil {
		args = map[string]any{}
	}
	args["cwd"] = dir
	return invokePolicyCheck(t, target.Sources{Env: env}, args)
}

func requireProjectHint(t *testing.T, check DoctorCheck, tool string) {
	t.Helper()
	if check.NextStep == nil || check.NextStep.Tool != tool {
		t.Fatalf("check should point at %s, got %+v", tool, check.NextStep)
	}
	root, _ := check.Data["targetRoot"].(string)
	if root == "" {
		t.Fatalf("check data should carry targetRoot, got %+v", check.Data)
	}
	if check.NextStep.Args["project"] != root {
		t.Fatalf("%s hint should carry the project root %q, got %+v", tool, root, check.NextStep.Args)
	}
}

// Conserved: project config errors fail the check and point at sofarpc_target for the project.
func TestDoctor_InvokePolicyReportsProjectConfigErrors(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	check := projectDoctorCheck(t, `{ not json`, target.Config{DirectURL: "bolt://127.0.0.1:1"}, nil)
	if check.Ok {
		t.Fatalf("invoke-policy should fail on project config errors, got %+v", check)
	}
	requireProjectHint(t, check, "sofarpc_target")
	if check.NextStep.Args["explain"] != true {
		t.Fatalf("target hint should explain, got %+v", check.NextStep.Args)
	}
}

// Conserved: a target the pure-Go path cannot serve fails and points at sofarpc_target, never at doctor itself.
func TestDoctor_InvokePolicyUnsupportedTargetPointsAtTarget(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	t.Setenv(envAllowedTargetHosts, "")
	sources := policyDoctorSources()
	sources.Env.Protocol = "tr"
	check := invokePolicyCheck(t, sources, nil)
	if check.Ok {
		t.Fatalf("invoke-policy should fail for an unsupported target, got %+v", check)
	}
	if !strings.Contains(check.Detail, "pure-Go invoke supports only") {
		t.Fatalf("detail should name the pure-Go transport rule, got %q", check.Detail)
	}
	if check.NextStep == nil || check.NextStep.Tool != "sofarpc_target" {
		t.Fatalf("check should point at sofarpc_target, got %+v", check.NextStep)
	}
}

// Conserved: a direct target host outside the allowlist fails the check.
func TestDoctor_InvokePolicyRejectsDisallowedTargetHost(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	t.Setenv(envAllowedTargetHosts, "other.example.internal")
	check := invokePolicyCheck(t, policyDoctorSources(), nil)
	if check.Ok {
		t.Fatalf("invoke-policy should fail for a disallowed target host, got %+v", check)
	}
	if !strings.Contains(check.Detail, "host") {
		t.Fatalf("detail should name the host rule, got %q", check.Detail)
	}
	if check.NextStep == nil || check.NextStep.Tool != "sofarpc_target" {
		t.Fatalf("check should point at sofarpc_target, got %+v", check.NextStep)
	}
}

// Conserved: a disabled real invoke names the env var and offers a dry run.
// Intended: the detail is the validator's message, not doctor's own wording.
func TestDoctor_InvokePolicyReportsDisabledRealInvoke(t *testing.T) {
	t.Setenv(envAllowInvoke, "false")
	check := invokePolicyCheck(t, target.Sources{}, nil)
	if check.Ok {
		t.Fatalf("invoke-policy should fail when real invoke is disabled, got %+v", check)
	}
	if !strings.Contains(check.Detail, "real invoke is disabled") {
		t.Fatalf("detail should be the execution policy message, got %q", check.Detail)
	}
	if !strings.Contains(check.Detail, envAllowInvoke) {
		t.Fatalf("detail should name %s, got %q", envAllowInvoke, check.Detail)
	}
	if check.NextStep == nil || check.NextStep.Tool != "sofarpc_invoke" {
		t.Fatalf("check should point at dry-run invoke, got %+v", check.NextStep)
	}
}

// Intended: a missing target points at sofarpc_target with the project root when known.
func TestDoctor_InvokePolicyMissingTargetCarriesProject(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	check := projectDoctorCheck(t, `{}`, target.Config{}, nil)
	if check.Ok {
		t.Fatalf("invoke-policy should fail without an executable target, got %+v", check)
	}
	if !strings.Contains(check.Detail, "no target resolved") {
		t.Fatalf("detail should report the missing target, got %q", check.Detail)
	}
	requireProjectHint(t, check, "sofarpc_target")
	if check.NextStep.Args["explain"] != true {
		t.Fatalf("target hint should explain, got %+v", check.NextStep.Args)
	}
}

// Intended: the validators' order decides which failure is reported first.
func TestDoctor_InvokePolicyConfigErrorsPrecedeDisabledInvoke(t *testing.T) {
	t.Setenv(envAllowInvoke, "false")
	check := projectDoctorCheck(t, `{ not json`, target.Config{DirectURL: "bolt://127.0.0.1:1"}, nil)
	if check.Ok {
		t.Fatalf("invoke-policy should fail, got %+v", check)
	}
	if !strings.Contains(check.Detail, "config") {
		t.Fatalf("config errors should be reported before the disabled real invoke, got %q", check.Detail)
	}
}

// Intended: the host rule is reported before the transport rule, matching the
// order a real invoke reaches them (policy first, wire path last).
func TestDoctor_InvokePolicyHostRulePrecedesTransportRule(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	t.Setenv(envAllowedTargetHosts, "other.example.internal")
	sources := policyDoctorSources()
	sources.Env.Protocol = "tr"
	check := invokePolicyCheck(t, sources, nil)
	if check.Ok {
		t.Fatalf("invoke-policy should fail, got %+v", check)
	}
	if !strings.Contains(check.Detail, "host") {
		t.Fatalf("the host rule should be reported before the transport rule, got %q", check.Detail)
	}
}

func TestDoctorActionFromHint(t *testing.T) {
	sources := target.Sources{ProjectRoot: "/tmp/project"}
	if got := doctorActionFromHint(nil, sources); got != nil {
		t.Fatalf("a nil hint yields no next step, got %+v", got)
	}
	if got := doctorActionFromHint(&errcode.Hint{Reason: "no tool"}, sources); got != nil {
		t.Fatalf("a hint without a tool yields no next step, got %+v", got)
	}

	got := doctorActionFromHint(&errcode.Hint{NextTool: "sofarpc_target", NextArgs: map[string]any{"explain": true}}, sources)
	if got == nil || got.Args["project"] != "/tmp/project" {
		t.Fatalf("a project-aware tool should be scoped to the project root, got %+v", got)
	}

	got = doctorActionFromHint(&errcode.Hint{NextTool: "sofarpc_target", NextArgs: map[string]any{"project": "/explicit"}}, sources)
	if got == nil || got.Args["project"] != "/explicit" {
		t.Fatalf("an explicit project must win over the ambient root, got %+v", got)
	}

	got = doctorActionFromHint(&errcode.Hint{NextTool: "sofarpc_invoke", NextArgs: map[string]any{"dryRun": true}}, sources)
	if got == nil || got.Args["project"] != nil {
		t.Fatalf("a tool that takes no project must stay unscoped, got %+v", got)
	}

	got = doctorActionFromHint(&errcode.Hint{NextTool: "sofarpc_sessions"}, target.Sources{})
	if got == nil || got.Tool != "sofarpc_sessions" || got.Args != nil {
		t.Fatalf("an argument-free hint should carry no args map, got %+v", got)
	}
}
