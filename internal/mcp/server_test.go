package mcp

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNew_RegistersSofarpcTools(t *testing.T) {
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
		"sofarpc_init_project",
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

func TestNew_InitProjectSchemaAdvertisesServicesArray(t *testing.T) {
	server := New(Options{})
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	tool := listedTool(t, client, "sofarpc_init_project")
	schema := schemaMap(t, tool.InputSchema)
	assertNoSchemaKeyword(t, schema, "anyOf")
	assertNoSchemaKeyword(t, schema, "oneOf")

	properties := schemaProperties(t, schema)
	services := schemaObjectProperty(t, properties, "services")
	if got := services["type"]; got != "array" {
		t.Fatalf("services type: got %#v want array", got)
	}
}

func TestNew_InvokeSchemaAdvertisesArgsArray(t *testing.T) {
	server := New(Options{})
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	tool := listedTool(t, client, "sofarpc_invoke")
	schema := schemaMap(t, tool.InputSchema)
	assertNoSchemaKeyword(t, schema, "anyOf")
	assertNoSchemaKeyword(t, schema, "oneOf")

	properties := schemaProperties(t, schema)
	args := schemaObjectProperty(t, properties, "args")
	if got := args["type"]; got != "array" {
		t.Fatalf("args type: got %#v want array", got)
	}
}

func TestNew_ReplaySchemaAdvertisesPayloadObject(t *testing.T) {
	server := New(Options{})
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	tool := listedTool(t, client, "sofarpc_replay")
	schema := schemaMap(t, tool.InputSchema)
	assertNoSchemaKeyword(t, schema, "anyOf")
	assertNoSchemaKeyword(t, schema, "oneOf")

	properties := schemaProperties(t, schema)
	payload := schemaObjectProperty(t, properties, "payload")
	if got := payload["type"]; got != "object" {
		t.Fatalf("payload type: got %#v want object", got)
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

func listedTool(t *testing.T, client *sdkmcp.ClientSession, name string) *sdkmcp.Tool {
	t.Helper()
	listed, err := client.ListTools(context.Background(), &sdkmcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range listed.Tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not listed", name)
	return nil
}

func schemaMap(t *testing.T, schema any) map[string]any {
	t.Helper()
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	return out
}

func schemaProperties(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties: got %T", schema["properties"])
	}
	return properties
}

func schemaObjectProperty(t *testing.T, properties map[string]any, name string) map[string]any {
	t.Helper()
	property, ok := properties[name].(map[string]any)
	if !ok {
		t.Fatalf("schema property %q: got %T", name, properties[name])
	}
	return property
}

func assertNoSchemaKeyword(t *testing.T, value any, keyword string) {
	t.Helper()
	switch node := value.(type) {
	case map[string]any:
		if _, ok := node[keyword]; ok {
			t.Fatalf("schema contains unsupported keyword %q", keyword)
		}
		for _, child := range node {
			assertNoSchemaKeyword(t, child, keyword)
		}
	case []any:
		for _, child := range node {
			assertNoSchemaKeyword(t, child, keyword)
		}
	}
}
