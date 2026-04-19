package mcp

import (
	"context"
	"encoding/json"
	"net"
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/core/target"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTargetHandler_UsesInputDirectURL(t *testing.T) {
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

	out := callTargetTool(t, Options{}, map[string]any{
		"directUrl":        "bolt://" + listener.Addr().String(),
		"connectTimeoutMs": 500,
	})

	if out.Target.Mode != target.ModeDirect {
		t.Fatalf("mode: got %q want direct", out.Target.Mode)
	}
	if !out.Reachability.Reachable {
		t.Fatalf("reachability should be true, got %+v", out.Reachability)
	}
}

func TestTargetHandler_FallsThroughToEnv(t *testing.T) {
	opts := Options{
		TargetSources: target.Sources{
			Env: target.Config{DirectURL: "bolt://127.0.0.1:1", Protocol: "bolt"},
		},
	}
	out := callTargetTool(t, opts, map[string]any{
		"connectTimeoutMs": 200,
	})

	if out.Target.Mode != target.ModeDirect {
		t.Fatalf("mode should come from env, got %q", out.Target.Mode)
	}
	if out.Target.DirectURL != "bolt://127.0.0.1:1" {
		t.Fatalf("directUrl should come from env, got %q", out.Target.DirectURL)
	}
	// :1 is reserved/refused — reachable must be false, and the message
	// must be populated so the agent can surface why.
	if out.Reachability.Reachable {
		t.Fatal("reachability should be false for :1")
	}
	if out.Reachability.Message == "" {
		t.Fatal("reachability message should be populated on failure")
	}
}

func TestTargetHandler_UnresolvedIsError(t *testing.T) {
	server := New(Options{})
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	result, err := client.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "sofarpc_target",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !result.IsError {
		t.Fatal("IsError should be true when no layer supplies a target mode")
	}
	out := decodeTargetOutput(t, result)
	if out.Target.Mode != "" {
		t.Fatalf("mode should be empty, got %q", out.Target.Mode)
	}
}

func TestTargetHandler_ReportsLayers(t *testing.T) {
	opts := Options{
		TargetSources: target.Sources{
			Env: target.Config{Serialization: "fastjson2"},
		},
	}
	out := callTargetTool(t, opts, map[string]any{
		"directUrl":        "bolt://127.0.0.1:1",
		"connectTimeoutMs": 200,
		"explain":          true,
	})

	if out.Target.Serialization != "fastjson2" {
		t.Fatalf("serialization should come from env, got %q", out.Target.Serialization)
	}
	if len(out.Layers) == 0 {
		t.Fatal("layers should be populated")
	}
	if len(out.Explain) == 0 {
		t.Fatal("explain should be populated when explain=true")
	}
}

func callTargetTool(t *testing.T, opts Options, args map[string]any) TargetOutput {
	t.Helper()
	server := New(opts)
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	result, err := client.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "sofarpc_target",
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("call target: %v", err)
	}
	return decodeTargetOutput(t, result)
}

func decodeTargetOutput(t *testing.T, result *sdkmcp.CallToolResult) TargetOutput {
	t.Helper()
	body, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	var out TargetOutput
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal structured: %v", err)
	}
	return out
}
