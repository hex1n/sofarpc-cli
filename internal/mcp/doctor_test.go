package mcp

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/javamodel"
	"github.com/hex1n/sofarpc-cli/internal/sourcecontract"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDoctor_UnresolvedTargetFails(t *testing.T) {
	out := callDoctor(t, Options{}, nil)
	if out.Ok {
		t.Fatal("doctor.Ok should be false when no target is configured")
	}
	targetCheck := findCheck(t, out, "target")
	if targetCheck.Ok {
		t.Fatal("target check should fail without env/input")
	}
	if targetCheck.NextStep == nil || targetCheck.NextStep.Tool != "sofarpc_target" {
		t.Fatalf("target check should point at sofarpc_target, got %+v", targetCheck.NextStep)
	}
}

func TestDoctor_ReachableTargetPasses(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	t.Setenv(envAllowedServices, "")
	t.Setenv(envAllowedTargetHosts, "")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	opts := Options{
		TargetSources: target.Sources{
			Env: target.Config{
				DirectURL:        "bolt://" + listener.Addr().String(),
				ConnectTimeoutMS: 500,
			},
		},
	}
	out := callDoctor(t, opts, nil)

	targetCheck := findCheck(t, out, "target")
	if !targetCheck.Ok {
		t.Fatalf("target check should pass, got %+v", targetCheck)
	}
	if !out.Ok {
		t.Fatalf("overall Ok should be true when target is reachable, got %+v", out)
	}
}

func TestDoctor_ContractCheckIsInformationalWithoutStore(t *testing.T) {
	out := callDoctor(t, Options{}, nil)
	contractCheck := findCheck(t, out, "contract")
	if !contractCheck.Ok {
		t.Fatalf("contract check should stay informational, got %+v", contractCheck)
	}
	if !strings.Contains(contractCheck.Detail, "trusted-mode invoke") {
		t.Fatalf("detail should mention trusted-mode invoke, got %q", contractCheck.Detail)
	}
}

func TestDoctor_ContractCheckReportsAttachedStore(t *testing.T) {
	store := contract.NewInMemoryStore(javamodel.Class{
		FQN:     "com.foo.Svc",
		Kind:    javamodel.KindInterface,
		Methods: []javamodel.Method{{Name: "doThing"}},
	})
	out := callDoctor(t, Options{Contract: store}, nil)
	contractCheck := findCheck(t, out, "contract")
	if !contractCheck.Ok {
		t.Fatalf("contract check should pass, got %+v", contractCheck)
	}
	if !strings.Contains(contractCheck.Detail, "attached") {
		t.Fatalf("detail should mention attached contract info, got %q", contractCheck.Detail)
	}
}

func TestDoctor_ContractCheckIncludesSourceDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeDoctorJava(t, root, "src/main/java/com/foo/Svc.java", `
package com.foo;
public interface Svc {
    String ping(String request);
}
`)

	store, err := sourcecontract.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if store == nil {
		t.Fatal("Load returned nil store")
	}

	out := callDoctor(t, Options{Contract: store}, nil)
	contractCheck := findCheck(t, out, "contract")
	if !contractCheck.Ok {
		t.Fatalf("contract check should pass, got %+v", contractCheck)
	}
	if contractCheck.Data == nil {
		t.Fatal("contract check should include diagnostics data")
	}
	if got := contractCheck.Data["indexedClasses"]; got != float64(1) {
		t.Fatalf("indexedClasses: got %#v", got)
	}
	if got := contractCheck.Data["parsedClasses"]; got != float64(0) {
		t.Fatalf("parsedClasses: got %#v want 0", got)
	}
	if !strings.Contains(contractCheck.Detail, "parsed on demand") {
		t.Fatalf("detail should mention lazy parsing, got %q", contractCheck.Detail)
	}
}

func TestDoctor_SummaryListsEachCheck(t *testing.T) {
	out := callDoctor(t, Options{}, nil)
	for _, name := range []string{"target", "contract", "sessions", "invoke-policy"} {
		if !strings.Contains(out.Summary, name+"=") {
			t.Fatalf("summary %q missing %s entry", out.Summary, name)
		}
	}
}

func TestDoctor_InvokePolicyReportsDisabledRealInvoke(t *testing.T) {
	t.Setenv(envAllowInvoke, "false")
	out := callDoctor(t, Options{}, nil)
	check := findCheck(t, out, "invoke-policy")
	if check.Ok {
		t.Fatalf("invoke-policy should fail when %s is unset, got %+v", envAllowInvoke, check)
	}
	if !strings.Contains(check.Detail, envAllowInvoke) {
		t.Fatalf("detail should mention %s, got %q", envAllowInvoke, check.Detail)
	}
	if check.NextStep == nil || check.NextStep.Tool != "sofarpc_invoke" {
		t.Fatalf("check should point at dry-run invoke, got %+v", check.NextStep)
	}
}

func TestDoctor_InvokePolicyChecksAllowedServiceWhenProvided(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	t.Setenv(envAllowedServices, "com.foo.AllowedFacade")

	out := callDoctor(t, Options{
		TargetSources: target.Sources{
			Env: target.Config{DirectURL: "bolt://127.0.0.1:1"},
		},
	}, map[string]any{"service": "com.foo.BlockedFacade"})
	check := findCheck(t, out, "invoke-policy")
	if check.Ok {
		t.Fatalf("invoke-policy should fail for disallowed service, got %+v", check)
	}
	if !strings.Contains(check.Detail, envAllowedServices) {
		t.Fatalf("detail should mention %s, got %q", envAllowedServices, check.Detail)
	}
}

func TestDoctor_SessionsReportsSizeAndCap(t *testing.T) {
	store := NewSessionStoreWithLimits(0, 32)
	store.Create(Session{ProjectRoot: "/a"})
	store.Create(Session{ProjectRoot: "/b"})

	out := callDoctor(t, Options{Sessions: store}, nil)
	check := findCheck(t, out, "sessions")
	if !check.Ok {
		t.Fatalf("sessions check should be informational (Ok=true), got %+v", check)
	}
	if !strings.Contains(check.Detail, "2/32") {
		t.Fatalf("detail should carry size/cap (2/32), got %q", check.Detail)
	}
}

func TestDoctor_SessionsUnboundedCapReportsSoftly(t *testing.T) {
	store := NewSessionStoreWithLimits(0, 0)
	out := callDoctor(t, Options{Sessions: store}, nil)
	check := findCheck(t, out, "sessions")
	if !check.Ok {
		t.Fatalf("sessions check should still be Ok=true when cap is 0, got %+v", check)
	}
	if !strings.Contains(check.Detail, "unbounded") {
		t.Fatalf("detail should mention 'unbounded' when cap=0, got %q", check.Detail)
	}
}

func callDoctor(t *testing.T, opts Options, args map[string]any) DoctorOutput {
	t.Helper()
	server := New(opts)
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	if args == nil {
		args = map[string]any{}
	}
	result, err := client.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "sofarpc_doctor",
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("call doctor: %v", err)
	}
	body, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	var out DoctorOutput
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal structured: %v", err)
	}
	return out
}

func findCheck(t *testing.T, out DoctorOutput, name string) DoctorCheck {
	t.Helper()
	for _, c := range out.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("check %q not found in %+v", name, out.Checks)
	return DoctorCheck{}
}

func writeDoctorJava(t *testing.T, root, relative, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
