package mcp

import (
	"os"
	"path/filepath"
	"testing"

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
