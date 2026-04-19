package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/facadesemantic"
	"github.com/hex1n/sofarpc-cli/internal/indexer"
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
	if out.Capabilities.Worker {
		t.Fatal("capabilities.worker should be false when no worker is configured")
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

func TestOpen_FacadeBannerReflectsLoadedIndex(t *testing.T) {
	dir := t.TempDir()
	// Seed an on-disk index with one interface and one DTO.
	indexDir := filepath.Join(dir, indexer.DirName, "shards")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustWriteJSON(t, filepath.Join(indexDir, "svc.json"), facadesemantic.Class{
		FQN: "com.foo.Svc", Kind: facadesemantic.KindInterface,
		Methods: []facadesemantic.Method{{Name: "doThing"}},
	})
	mustWriteJSON(t, filepath.Join(indexDir, "dto.json"), facadesemantic.Class{
		FQN: "com.foo.Dto", Kind: facadesemantic.KindClass,
	})
	mustWriteJSON(t, filepath.Join(dir, indexer.DirName, indexer.MetaFilename), indexer.Meta{
		Version: 1,
		Classes: map[string]string{
			"com.foo.Svc": "shards/svc.json",
			"com.foo.Dto": "shards/dto.json",
		},
	})

	idx, err := indexer.Load(dir)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}

	out := callOpen(t, Options{Facade: idx}, map[string]any{"cwd": dir})

	if !out.Facade.Configured {
		t.Fatal("facade.configured should be true when a store is attached")
	}
	if !out.Facade.Indexed {
		t.Fatal("facade.indexed should be true when the index is non-empty")
	}
	if out.Facade.Services != 1 {
		t.Fatalf("facade.services: got %d want 1 (only Svc is an interface)", out.Facade.Services)
	}
	if !out.Capabilities.FacadeIndex {
		t.Fatal("capabilities.facadeIndex should be true")
	}
}

func mustWriteJSON(t *testing.T, path string, v any) {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
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
