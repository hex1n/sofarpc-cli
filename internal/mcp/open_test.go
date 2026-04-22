package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/javamodel"
	"github.com/hex1n/sofarpc-cli/internal/sourcecontract"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestOpen_EmptyProjectSucceeds(t *testing.T) {
	dir := t.TempDir()
	out := callOpen(t, Options{}, map[string]any{"cwd": dir})

	if out.SessionID == "" {
		t.Fatal("sessionId should be generated")
	}
	if out.ProjectRoot != dir {
		t.Fatalf("projectRoot: got %q want %q", out.ProjectRoot, dir)
	}
	if out.Target.Mode != "" {
		t.Fatalf("target.mode should be empty when no layer supplies it; got %q", out.Target.Mode)
	}
	if !out.Capabilities.DirectInvoke {
		t.Fatal("capabilities.directInvoke should be true")
	}
	if out.Capabilities.Describe {
		t.Fatal("capabilities.describe should be false when no contract store is attached")
	}
	if !out.Capabilities.Replay {
		t.Fatal("capabilities.replay should be true when a session store is attached")
	}
	if out.Contract.Attached {
		t.Fatal("contract.attached should be false when no contract store is attached")
	}
}

func TestOpen_SessionIsStoredAndRetrievable(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionStore()
	opts := Options{Sessions: store}
	out := callOpen(t, opts, map[string]any{"cwd": dir})

	session, ok := store.Get(out.SessionID)
	if !ok {
		t.Fatalf("session %q should be retrievable after open", out.SessionID)
	}
	if session.ProjectRoot != dir {
		t.Fatalf("stored session projectRoot: got %q want %q", session.ProjectRoot, dir)
	}
}

func TestOpen_DescribeCapabilityTracksFacadeStore(t *testing.T) {
	dir := t.TempDir()

	out := callOpen(t, Options{}, map[string]any{"cwd": dir})
	if out.Capabilities.Describe {
		t.Fatal("capabilities.describe should be false when no contract store is attached")
	}

	store := contract.NewInMemoryStore(javamodel.Class{
		FQN:     "com.foo.Svc",
		Kind:    javamodel.KindInterface,
		Methods: []javamodel.Method{{Name: "doThing"}},
	})
	out = callOpen(t, Options{Contract: store}, map[string]any{"cwd": dir})
	if !out.Capabilities.Describe {
		t.Fatal("capabilities.describe should be true when a contract store is attached")
	}
	if !out.Contract.Attached {
		t.Fatal("contract.attached should be true when a contract store is attached")
	}
	if out.Contract.Source != "contract-store" {
		t.Fatalf("contract.source: got %q want contract-store", out.Contract.Source)
	}
}

func TestOpen_ContractBannerIncludesSourceDiagnostics(t *testing.T) {
	dir := t.TempDir()
	writeOpenJava(t, dir, "src/main/java/com/foo/Svc.java", `
package com.foo;
public interface Svc {
    String ping(String request);
}
`)

	store, err := sourcecontract.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if store == nil {
		t.Fatal("Load returned nil store")
	}

	out := callOpen(t, Options{Contract: store}, map[string]any{"cwd": dir})
	if !out.Contract.Attached {
		t.Fatal("contract.attached should be true")
	}
	if out.Contract.Source != "sourcecontract" {
		t.Fatalf("contract.source: got %q want sourcecontract", out.Contract.Source)
	}
	if out.Contract.IndexedClasses != 1 || out.Contract.IndexedFiles != 1 || out.Contract.ParsedClasses != 0 {
		t.Fatalf("contract banner: %+v", out.Contract)
	}
}

func TestOpen_InvalidProjectReturnsError(t *testing.T) {
	server := New(Options{})
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	result, err := client.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "sofarpc_open",
		Arguments: map[string]any{"project": "/definitely/does/not/exist"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !result.IsError {
		t.Fatal("IsError should be true for a missing project root")
	}
}

func TestSummarizeOpen_RendersTargetMode(t *testing.T) {
	out := OpenOutput{
		SessionID:   "ws_x",
		ProjectRoot: "/p",
		Target:      target.Config{Mode: target.ModeDirect, DirectURL: "bolt://h:12200"},
	}
	text := summarizeOpen(out)
	if text == "" {
		t.Fatal("summary should not be empty")
	}
}

func callOpen(t *testing.T, opts Options, args map[string]any) OpenOutput {
	t.Helper()
	server := New(opts)
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	result, err := client.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "sofarpc_open",
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("call open: %v", err)
	}
	body, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	var out OpenOutput
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal structured: %v", err)
	}
	return out
}

func writeOpenJava(t *testing.T, root, relative, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
