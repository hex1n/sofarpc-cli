package mcp

import (
	"context"
	"sort"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNew_RegistersSixTools(t *testing.T) {
	server := New(Options{})
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	listed, err := client.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	want := []string{
		"sofarpc_describe",
		"sofarpc_doctor",
		"sofarpc_invoke",
		"sofarpc_open",
		"sofarpc_replay",
		"sofarpc_target",
	}
	if len(names) != len(want) {
		t.Fatalf("tool count: got %d (%v), want %d", len(names), names, len(want))
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("tool %d: got %q want %q", i, names[i], want[i])
		}
	}
}

func connect(t *testing.T, ctx context.Context, server *sdkmcp.Server) *sdkmcp.ClientSession {
	t.Helper()
	serverSide, clientSide := sdkmcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverSide, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	return session
}
