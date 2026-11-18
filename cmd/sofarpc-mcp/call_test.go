package main

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
)

func TestRunCall_DryRunPrintsPlan(t *testing.T) {
	root := t.TempDir()
	t.Setenv(invoke.EnvAllowInvoke, "false")

	var buf bytes.Buffer
	err := runCall(&buf, []string{
		"-project", root,
		"-service", "com.foo.UserFacade",
		"-method", "query",
		"-types", "java.lang.String",
		"-args", `["id-1"]`,
		"-direct-url", "bolt://127.0.0.1:12200",
		"-dry-run",
	})
	if err != nil {
		t.Fatalf("dry-run call failed: %v\noutput: %s", err, buf.String())
	}
	out := decodeCallOutput(t, buf.Bytes())
	if !out.Ok || out.Error != nil {
		t.Fatalf("dry-run should succeed: %+v", out)
	}
	if out.Plan == nil || out.Plan.Service != "com.foo.UserFacade" || out.Plan.Method != "query" {
		t.Fatalf("plan mismatch: %+v", out.Plan)
	}
	if out.Plan.SchemaVersion != invoke.PlanSchemaVersion {
		t.Fatalf("schemaVersion: got %q want %q", out.Plan.SchemaVersion, invoke.PlanSchemaVersion)
	}
	if out.Plan.Target.DirectURL != "bolt://127.0.0.1:12200" {
		t.Fatalf("target: %+v", out.Plan.Target)
	}
	if out.Result != nil {
		t.Fatalf("dry-run must not execute: %+v", out.Result)
	}
	if out.Preflight == nil || out.Preflight.Ready || out.Preflight.PolicyError == nil || out.Preflight.PolicyError.Code != errcode.InvocationRejected {
		t.Fatalf("expected separate disabled-invoke check: %+v", out.Preflight)
	}
}

func TestRunCall_RealInvokeRequiresOptIn(t *testing.T) {
	// "TRUe" locks opt-in parsing to the MCP server's strconv.ParseBool
	// semantics: values the MCP tools refuse must not enable the CLI.
	for _, optIn := range []string{"false", "TRUe"} {
		t.Run(optIn, func(t *testing.T) {
			t.Setenv(invoke.EnvAllowInvoke, optIn)
			root := t.TempDir()

			var buf bytes.Buffer
			err := runCall(&buf, []string{
				"-project", root,
				"-service", "com.foo.UserFacade",
				"-method", "query",
				"-types", "java.lang.String",
				"-args", `["id-1"]`,
				"-direct-url", "bolt://127.0.0.1:12200",
			})
			if err == nil {
				t.Fatalf("real invoke without opt-in should fail\noutput: %s", buf.String())
			}
			out := decodeCallOutput(t, buf.Bytes())
			if out.Ok || out.Error == nil {
				t.Fatalf("output should carry the rejection: %+v", out)
			}
			if out.Error.Code != errcode.InvocationRejected {
				t.Fatalf("code: got %q want %q", out.Error.Code, errcode.InvocationRejected)
			}
			if !strings.Contains(out.Error.Message, invoke.EnvAllowInvoke) {
				t.Fatalf("rejection should name the opt-in env: %+v", out.Error)
			}
			if out.Plan == nil {
				t.Fatal("rejected call should still print the plan for inspection")
			}
		})
	}
}

func TestRunCall_RejectsTRURLBeforeDial(t *testing.T) {
	t.Setenv(invoke.EnvAllowInvoke, "true")
	t.Setenv(invoke.EnvAllowTargetOverride, "false")
	t.Setenv(invoke.EnvAllowedTargetHosts, "127.0.0.1")

	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	for _, tc := range []struct {
		name     string
		scheme   string
		protocol string
	}{
		{name: "default-protocol", scheme: "tr"},
		{name: "explicit-bolt", scheme: "TR", protocol: "bolt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, ".sofarpc")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			config := map[string]string{"directUrl": tc.scheme + "://" + listener.Addr().String()}
			if tc.protocol != "" {
				config["protocol"] = tc.protocol
			}
			data, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{
				"-project", root, "-service", "com.foo.UserFacade",
				"-method", "ping", "-types=", "-args", "[]", "-timeout-ms", "100",
			}

			var buf bytes.Buffer
			if err := runCall(&buf, append(args, "-dry-run")); err != nil {
				t.Fatalf("dry-run should preserve the plan: %v", err)
			}
			preview := decodeCallOutput(t, buf.Bytes())
			if !preview.Ok || preview.Plan == nil || preview.Preflight == nil {
				t.Fatalf("dry-run should report the plan and preflight: %+v", preview)
			}
			check := preview.Preflight
			if check.Ready || check.PolicyError != nil || check.ExecutionError == nil ||
				check.ExecutionError.Code != errcode.InvocationRejected ||
				!strings.Contains(check.ExecutionError.Message, "tr://") {
				t.Errorf("expected a TR transport rejection in preflight: %s", buf.String())
			}

			buf.Reset()
			err = runCall(&buf, args)
			live := decodeCallOutput(t, buf.Bytes())
			if err == nil || live.Error == nil || live.Error.Code != errcode.InvocationRejected ||
				!strings.Contains(live.Error.Message, "tr://") {
				t.Errorf("expected a TR transport rejection for a live call: %s", buf.String())
			}
			if err := listener.SetDeadline(time.Now().Add(25 * time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			conn, err := listener.AcceptTCP()
			if err == nil {
				conn.Close()
				t.Fatal("the CLI dialed a TR address")
			}
			if !os.IsTimeout(err) {
				t.Fatalf("unexpected accept error: %v", err)
			}
		})
	}
}

func TestRunCall_PlanFileDryRunUnwrapsInvokeEnvelope(t *testing.T) {
	root := t.TempDir()

	var first bytes.Buffer
	if err := runCall(&first, []string{
		"-project", root,
		"-service", "com.foo.UserFacade",
		"-method", "query",
		"-types", "java.lang.String",
		"-args", `["id-1"]`,
		"-direct-url", "bolt://127.0.0.1:12200",
		"-dry-run",
	}); err != nil {
		t.Fatalf("seed dry-run failed: %v", err)
	}
	planPath := filepath.Join(root, "plan.json")
	if err := os.WriteFile(planPath, first.Bytes(), 0o644); err != nil {
		t.Fatalf("write plan file: %v", err)
	}

	var buf bytes.Buffer
	err := runCall(&buf, []string{"-project", root, "-plan", planPath, "-dry-run"})
	if err != nil {
		t.Fatalf("plan-file dry-run failed: %v\noutput: %s", err, buf.String())
	}
	out := decodeCallOutput(t, buf.Bytes())
	if !out.Ok || out.Plan == nil || out.Plan.Method != "query" {
		t.Fatalf("plan-file replay mismatch: %+v", out)
	}
}

func TestRunCall_PlanFileConflictsWithServiceFlags(t *testing.T) {
	var buf bytes.Buffer
	err := runCall(&buf, []string{"-plan", "x.json", "-service", "com.foo.UserFacade"})
	if err == nil {
		t.Fatal("plan file combined with service flags should fail")
	}
	out := decodeCallOutput(t, buf.Bytes())
	if out.Error == nil || out.Error.Code != errcode.ArgsInvalid {
		t.Fatalf("expected input.args-invalid, got %+v", out.Error)
	}
}

func decodeCallOutput(t *testing.T, body []byte) callOutput {
	t.Helper()
	var out callOutput
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("call output is not JSON: %v\n%s", err, body)
	}
	return out
}
