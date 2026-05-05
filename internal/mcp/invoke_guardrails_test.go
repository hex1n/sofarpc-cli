package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
)

func init() {
	// Existing invoke tests exercise real loopback calls and @file fixtures.
	// Keep those focused on transport behavior while dedicated guardrail tests
	// below assert the deny paths explicitly.
	_ = os.Setenv(envAllowInvoke, "true")
	_ = os.Setenv(envArgsFileRoot, os.TempDir())
}

func TestValidateRealInvokeRequiresAllowEnv(t *testing.T) {
	t.Setenv(envAllowInvoke, "false")
	err := validateRealInvoke("com.foo.Svc")
	if err == nil {
		t.Fatal("expected real invoke to be rejected")
	}
	ecerr, ok := err.(*errcode.Error)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if ecerr.Code != errcode.InvocationRejected {
		t.Fatalf("code = %s, want %s", ecerr.Code, errcode.InvocationRejected)
	}
}

func TestValidateRealInvokeRespectsAllowedServices(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	t.Setenv(envAllowedServices, "com.foo.AllowedFacade,com.foo.OtherFacade")

	if err := validateRealInvoke("com.foo.AllowedFacade"); err != nil {
		t.Fatalf("allowed service rejected: %v", err)
	}

	err := validateRealInvoke("com.foo.BlockedFacade")
	if err == nil {
		t.Fatal("expected disallowed service to be rejected")
	}
	ecerr, ok := err.(*errcode.Error)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if ecerr.Code != errcode.InvocationRejected {
		t.Fatalf("code = %s, want %s", ecerr.Code, errcode.InvocationRejected)
	}
}

func TestValidateExecutionPolicyRejectsDirectTargetOverrideByDefault(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	t.Setenv(envAllowedServices, "")
	t.Setenv(envAllowTargetOverride, "false")

	err := validateExecutionPolicy(invoke.Plan{
		Service: "com.foo.Svc",
		Target: target.Config{
			Mode:      target.ModeDirect,
			DirectURL: "bolt://override.example:12200",
		},
	}, "invoke", target.Sources{})
	if err == nil {
		t.Fatal("expected direct target override to be rejected")
	}
	ecerr, ok := err.(*errcode.Error)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if ecerr.Code != errcode.InvocationRejected {
		t.Fatalf("code = %s, want %s", ecerr.Code, errcode.InvocationRejected)
	}
}

func TestValidateExecutionPolicyAllowsEnvDirectTargetByDefault(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	t.Setenv(envAllowedServices, "")
	t.Setenv(envAllowTargetOverride, "false")

	err := validateExecutionPolicy(invoke.Plan{
		Service: "com.foo.Svc",
		Target: target.Config{
			Mode:      target.ModeDirect,
			DirectURL: "env.example:12200",
		},
	}, "invoke", target.Sources{
		Env: target.Config{DirectURL: "bolt://env.example:12200"},
	})
	if err != nil {
		t.Fatalf("env direct target should be allowed: %v", err)
	}
}

func TestValidateExecutionPolicyAllowsProjectDirectTargetByDefault(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	t.Setenv(envAllowedServices, "")
	t.Setenv(envAllowTargetOverride, "false")

	err := validateExecutionPolicy(invoke.Plan{
		Service: "com.foo.Svc",
		Target: target.Config{
			Mode:      target.ModeDirect,
			DirectURL: "bolt://project.example:12200",
		},
	}, "invoke", target.Sources{
		ProjectLocal: target.Config{DirectURL: "bolt://project.example:12200"},
	})
	if err != nil {
		t.Fatalf("project direct target should be allowed: %v", err)
	}
}

func TestValidateExecutionPolicyRejectsProjectConfigErrors(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	t.Setenv(envAllowedServices, "")
	t.Setenv(envAllowTargetOverride, "false")

	err := validateExecutionPolicy(invoke.Plan{
		Service: "com.foo.Svc",
		Target: target.Config{
			Mode:      target.ModeDirect,
			DirectURL: "bolt://project.example:12200",
		},
	}, "invoke", target.Sources{
		ProjectLocal: target.Config{DirectURL: "bolt://project.example:12200"},
		ConfigErrors: []target.ConfigError{{Path: ".sofarpc/config.json", Error: "bad json"}},
	})
	if err == nil {
		t.Fatal("expected project config errors to reject real invoke")
	}
	ecerr, ok := err.(*errcode.Error)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if ecerr.Code != errcode.InvocationRejected {
		t.Fatalf("code = %s, want %s", ecerr.Code, errcode.InvocationRejected)
	}
}

func TestValidateExecutionPolicyRespectsAllowedTargetHosts(t *testing.T) {
	t.Setenv(envAllowInvoke, "true")
	t.Setenv(envAllowedServices, "")
	t.Setenv(envAllowTargetOverride, "true")
	t.Setenv(envAllowedTargetHosts, "allowed.example,127.0.0.1:12200")

	err := validateExecutionPolicy(invoke.Plan{
		Service: "com.foo.Svc",
		Target: target.Config{
			Mode:      target.ModeDirect,
			DirectURL: "bolt://blocked.example:12200",
		},
	}, "replay", target.Sources{})
	if err == nil {
		t.Fatal("expected blocked target host to be rejected")
	}
	ecerr, ok := err.(*errcode.Error)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if ecerr.Code != errcode.InvocationRejected {
		t.Fatalf("code = %s, want %s", ecerr.Code, errcode.InvocationRejected)
	}
	if ecerr.Phase != "replay" {
		t.Fatalf("phase = %q, want replay", ecerr.Phase)
	}

	err = validateExecutionPolicy(invoke.Plan{
		Service: "com.foo.Svc",
		Target: target.Config{
			Mode:      target.ModeDirect,
			DirectURL: "bolt://allowed.example:12200",
		},
	}, "invoke", target.Sources{})
	if err != nil {
		t.Fatalf("allowed target host rejected: %v", err)
	}
}

func TestResolveArgsFilePathRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envArgsFileRoot, root)
	outside := t.TempDir()
	path := filepath.Join(outside, "args.json")
	if err := os.WriteFile(path, []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	_, err := resolveArgsFilePath(path, root)
	if err == nil {
		t.Fatal("expected path outside root to be rejected")
	}
}

func TestReadArgsFileRejectsOversizedFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv(envArgsFileRoot, root)
	t.Setenv(envArgsFileMaxBytes, "4")

	path := filepath.Join(root, "args.json")
	if err := os.WriteFile(path, []byte(`{"too":"large"}`), 0o644); err != nil {
		t.Fatalf("write args file: %v", err)
	}

	_, err := readArgsFile("com.foo.Svc", "doThing", "args.json", root)
	if err == nil {
		t.Fatal("expected oversized args file to be rejected")
	}
	ecerr, ok := err.(*errcode.Error)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if ecerr.Code != errcode.ArgsInvalid {
		t.Fatalf("code = %s, want %s", ecerr.Code, errcode.ArgsInvalid)
	}
}
