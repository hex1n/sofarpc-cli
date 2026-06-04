package mcp

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

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

func TestNew_ToolsAdvertiseBestPracticeMetadata(t *testing.T) {
	server := New(Options{})
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	want := map[string]struct {
		title           string
		readOnly        bool
		openWorld       bool
		destructive     bool
		wantDestructive bool
	}{
		"sofarpc_describe":     {title: "Describe SOFARPC Method", readOnly: true, openWorld: false},
		"sofarpc_doctor":       {title: "Diagnose SOFARPC Workspace", readOnly: true, openWorld: true},
		"sofarpc_init_project": {title: "Initialize SOFARPC Project", readOnly: false, openWorld: false, destructive: true, wantDestructive: true},
		"sofarpc_invoke":       {title: "Invoke SOFARPC Method", readOnly: false, openWorld: true, destructive: true, wantDestructive: true},
		"sofarpc_open":         {title: "Open SOFARPC Workspace", readOnly: true, openWorld: false},
		"sofarpc_replay":       {title: "Replay SOFARPC Invocation", readOnly: false, openWorld: true, destructive: true, wantDestructive: true},
		"sofarpc_target":       {title: "Resolve SOFARPC Target", readOnly: true, openWorld: true},
	}
	for name, expected := range want {
		tool := listedTool(t, client, name)
		if tool.Title != expected.title {
			t.Fatalf("%s title: got %q want %q", name, tool.Title, expected.title)
		}
		if tool.Annotations == nil {
			t.Fatalf("%s annotations are missing", name)
		}
		if tool.Annotations.Title != expected.title {
			t.Fatalf("%s annotation title: got %q want %q", name, tool.Annotations.Title, expected.title)
		}
		if tool.Annotations.ReadOnlyHint != expected.readOnly {
			t.Fatalf("%s readOnlyHint: got %v want %v", name, tool.Annotations.ReadOnlyHint, expected.readOnly)
		}
		assertBoolHint(t, name, "openWorldHint", tool.Annotations.OpenWorldHint, expected.openWorld)
		if expected.wantDestructive {
			assertBoolHint(t, name, "destructiveHint", tool.Annotations.DestructiveHint, expected.destructive)
		} else if tool.Annotations.DestructiveHint != nil {
			t.Fatalf("%s destructiveHint: got explicit %v, want omitted", name, *tool.Annotations.DestructiveHint)
		}
		if tool.Annotations.IdempotentHint {
			t.Fatalf("%s idempotentHint: got true want false", name)
		}
	}
}

func TestNew_ToolsAdvertiseOutputSchemas(t *testing.T) {
	server := New(Options{})
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	listed, err := client.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range listed.Tools {
		schema := schemaMap(t, tool.OutputSchema)
		if got := schema["type"]; got != "object" {
			t.Fatalf("%s output schema type: got %#v want object", tool.Name, got)
		}
	}

	for _, name := range []string{"sofarpc_invoke", "sofarpc_replay"} {
		properties := schemaProperties(t, schemaMap(t, listedTool(t, client, name).OutputSchema))
		for _, property := range []string{"ok", "plan", "result", "diagnostics", "error"} {
			if _, ok := properties[property].(map[string]any); !ok {
				t.Fatalf("%s output schema property %q: got %T", name, property, properties[property])
			}
		}
	}
}

func TestNew_AdvertisesResourceTemplates(t *testing.T) {
	server := New(Options{})
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	listed, err := client.ListResourceTemplates(ctx, &sdkmcp.ListResourceTemplatesParams{})
	if err != nil {
		t.Fatalf("list resource templates: %v", err)
	}
	got := map[string]*sdkmcp.ResourceTemplate{}
	for _, template := range listed.ResourceTemplates {
		got[template.URITemplate] = template
	}
	want := []string{
		"sofarpc://session/{sessionId}",
		"sofarpc://session/{sessionId}/contract",
		"sofarpc://session/{sessionId}/plan",
	}
	for _, uriTemplate := range want {
		template := got[uriTemplate]
		if template == nil {
			t.Fatalf("resource template %q not listed; got %v", uriTemplate, keys(got))
		}
		if template.MIMEType != resourceMIMEJSON {
			t.Fatalf("%s MIME type: got %q want %q", uriTemplate, template.MIMEType, resourceMIMEJSON)
		}
		if template.Title == "" || template.Description == "" {
			t.Fatalf("%s should advertise title and description: %+v", uriTemplate, template)
		}
	}
}

func TestNew_AdvertisesWorkflowPrompts(t *testing.T) {
	server := New(Options{})
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	listed, err := client.ListPrompts(ctx, &sdkmcp.ListPromptsParams{})
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	got := map[string]*sdkmcp.Prompt{}
	for _, prompt := range listed.Prompts {
		got[prompt.Name] = prompt
	}
	want := []string{
		promptBootstrapProject,
		promptDiagnoseFailure,
		promptDryRunFacadeCall,
	}
	for _, name := range want {
		prompt := got[name]
		if prompt == nil {
			t.Fatalf("prompt %q not listed; got %v", name, keys(got))
		}
		if prompt.Title == "" || prompt.Description == "" {
			t.Fatalf("%s should advertise title and description: %+v", name, prompt)
		}
		if len(prompt.Arguments) == 0 {
			t.Fatalf("%s should advertise workflow arguments", name)
		}
	}

	dryRun := got[promptDryRunFacadeCall]
	dryRunArgs := map[string]bool{}
	required := map[string]bool{}
	for _, arg := range dryRun.Arguments {
		dryRunArgs[arg.Name] = true
		if arg.Required {
			required[arg.Name] = true
		}
	}
	for _, name := range []string{"service", "method"} {
		if !required[name] {
			t.Fatalf("dry-run prompt argument %q should be required; args=%+v", name, dryRun.Arguments)
		}
	}
	for _, name := range []string{"args", "argsFile", "invocationProperties"} {
		if !dryRunArgs[name] {
			t.Fatalf("dry-run prompt argument %q should be advertised; args=%+v", name, dryRun.Arguments)
		}
	}

	diagnoseArgs := map[string]bool{}
	for _, arg := range got[promptDiagnoseFailure].Arguments {
		diagnoseArgs[arg.Name] = true
	}
	for _, name := range []string{"message", "nextTool", "nextArgs"} {
		if !diagnoseArgs[name] {
			t.Fatalf("diagnose prompt argument %q should be advertised; args=%+v", name, got[promptDiagnoseFailure].Arguments)
		}
	}
}

func TestPrompts_ReturnWorkflowMessages(t *testing.T) {
	server := New(Options{})
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	result, err := client.GetPrompt(ctx, &sdkmcp.GetPromptParams{
		Name: promptDryRunFacadeCall,
		Arguments: map[string]string{
			"service":              "com.foo.OrderFacade",
			"method":               "query",
			"sessionId":            "ws_1",
			"args":                 `[{"orderId":42}]`,
			"argsFile":             "payloads/order-query.json",
			"invocationProperties": `{"tenant":{"value":"dev"},"authToken":{"env":"SOFARPC_AUTH_TOKEN"}}`,
		},
	})
	if err != nil {
		t.Fatalf("get dry-run prompt: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("prompt message count: got %d want 1", len(result.Messages))
	}
	if result.Messages[0].Role != "user" {
		t.Fatalf("prompt role: got %q want user", result.Messages[0].Role)
	}
	text := contentText(t, result.Messages[0].Content)
	for _, want := range []string{
		"com.foo.OrderFacade",
		"query",
		"sofarpc_open",
		"sofarpc_describe",
		"sofarpc_invoke",
		"dryRun=true",
		"real invoke",
		`[{"orderId":42}]`,
		"payloads/order-query.json",
		"SOFARPC_ARGS_FILE_ROOT",
		"invocationProperties",
		"rpc_req_baggage",
		"plan.invocationProperties",
		"invoke.baggage.enable",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("prompt text missing %q:\n%s", want, text)
		}
	}
}

func TestDiagnosePrompt_GuidesResourcesAndInvocationPropertiesRecovery(t *testing.T) {
	server := New(Options{})
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	result, err := client.GetPrompt(ctx, &sdkmcp.GetPromptParams{
		Name: promptDiagnoseFailure,
		Arguments: map[string]string{
			"sessionId": "ws_1",
			"code":      "input.args-invalid",
			"phase":     "invoke",
			"message":   "invalid invocationProperties: invocation property authToken env SOFARPC_AUTH_TOKEN is missing or empty",
			"nextTool":  "sofarpc_doctor",
			"nextArgs":  `{"sessionId":"ws_1"}`,
		},
	})
	if err != nil {
		t.Fatalf("get diagnose prompt: %v", err)
	}
	text := contentText(t, result.Messages[0].Content)
	for _, want := range []string{
		"hint.nextTool",
		"sofarpc_doctor",
		`{"sessionId":"ws_1"}`,
		"sofarpc://session/{sessionId}",
		"sofarpc://session/{sessionId}/plan",
		"contract.method-ambiguous",
		"invocationProperties",
		"request baggage",
		"invocation-properties check",
		"Missing or empty env references",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("diagnose prompt text missing %q:\n%s", want, text)
		}
	}
}

func TestPrompts_ValidateRequiredArguments(t *testing.T) {
	server := New(Options{})
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	_, err := client.GetPrompt(ctx, &sdkmcp.GetPromptParams{
		Name:      promptDryRunFacadeCall,
		Arguments: map[string]string{"method": "query"},
	})
	if err == nil || !strings.Contains(err.Error(), "service argument is required") {
		t.Fatalf("missing service error: %v", err)
	}
}

func TestResources_ReadSessionAndPlan(t *testing.T) {
	sessions := NewSessionStoreWithLimits(0, 32).WithIDFunc(func() string { return "sess-1" })
	plan := samplePlan()
	session := sessions.Create(Session{
		ProjectRoot: "/tmp/project",
		Target:      plan.Target,
	})
	if capture := sessions.CapturePlan(session.ID, plan); !capture.Captured {
		t.Fatalf("capture plan: %+v", capture)
	}

	server := New(Options{Sessions: sessions})
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	sessionBody := readResourceText(t, ctx, client, sessionResourceURI(session.ID))
	var sessionOut sessionResource
	if err := json.Unmarshal([]byte(sessionBody), &sessionOut); err != nil {
		t.Fatalf("unmarshal session resource: %v", err)
	}
	if sessionOut.ID != session.ID || sessionOut.LastPlan == nil {
		t.Fatalf("session resource: %+v", sessionOut)
	}
	if sessionOut.LastPlan.URI != sessionPlanResourceURI(session.ID) {
		t.Fatalf("session last plan URI: got %q want %q", sessionOut.LastPlan.URI, sessionPlanResourceURI(session.ID))
	}

	planBody := readResourceText(t, ctx, client, sessionPlanResourceURI(session.ID))
	var planOut planResource
	if err := json.Unmarshal([]byte(planBody), &planOut); err != nil {
		t.Fatalf("unmarshal plan resource: %v", err)
	}
	if planOut.SessionID != session.ID || planOut.Plan.Service != plan.Service || planOut.Plan.Method != plan.Method {
		t.Fatalf("plan resource: %+v", planOut)
	}
}

func TestInvokeDryRunReturnsCapturedPlanResourceLink(t *testing.T) {
	sessions := NewSessionStoreWithLimits(0, 32).WithIDFunc(func() string { return "sess-1" })
	session := sessions.Create(Session{ProjectRoot: t.TempDir()})
	server := New(Options{Sessions: sessions})
	ctx := context.Background()
	client := connect(t, ctx, server)
	defer client.Close()

	result, err := client.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "sofarpc_invoke",
		Arguments: map[string]any{
			"sessionId":    session.ID,
			"contractMode": "trusted",
			"service":      "com.foo.Svc",
			"method":       "doThing",
			"types":        []string{"java.lang.String"},
			"args":         []any{"hello"},
			"directUrl":    "bolt://h:1",
			"dryRun":       true,
		},
	})
	if err != nil {
		t.Fatalf("call invoke: %v", err)
	}
	link := findResourceLink(result.Content, sessionPlanResourceURI(session.ID))
	if link == nil {
		t.Fatalf("invoke result did not include plan resource link; content=%#v", result.Content)
	}

	planBody := readResourceText(t, ctx, client, link.URI)
	var planOut planResource
	if err := json.Unmarshal([]byte(planBody), &planOut); err != nil {
		t.Fatalf("unmarshal plan resource: %v", err)
	}
	if planOut.Plan.Service != "com.foo.Svc" || planOut.Plan.Method != "doThing" {
		t.Fatalf("plan resource: %+v", planOut.Plan)
	}
}

func TestToolProgressNotificationsWhenTokenProvided(t *testing.T) {
	server := New(Options{})
	ctx := context.Background()
	progress := make(chan string, 8)
	client := connectWithOptions(t, ctx, server, &sdkmcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *sdkmcp.ProgressNotificationClientRequest) {
			progress <- req.Params.Message
		},
	})
	defer client.Close()

	params := &sdkmcp.CallToolParams{Name: "sofarpc_target"}
	params.SetProgressToken("progress-1")
	if _, err := client.CallTool(ctx, params); err != nil {
		t.Fatalf("call target: %v", err)
	}
	messages := collectProgressMessages(t, progress, 2)
	if !contains(messages, "resolving target scope") {
		t.Fatalf("progress messages missing scope notification: %v", messages)
	}
}

func TestToolLoggingNotificationsWhenLevelProvided(t *testing.T) {
	server := New(Options{})
	ctx := context.Background()
	logs := make(chan map[string]any, 8)
	client := connectWithOptions(t, ctx, server, &sdkmcp.ClientOptions{
		LoggingMessageHandler: func(_ context.Context, req *sdkmcp.LoggingMessageRequest) {
			if req.Params.Logger != toolLogLogger {
				return
			}
			if data, ok := req.Params.Data.(map[string]any); ok {
				logs <- data
			}
		},
	})
	defer client.Close()

	if err := client.SetLoggingLevel(ctx, &sdkmcp.SetLoggingLevelParams{Level: "info"}); err != nil {
		t.Fatalf("set logging level: %v", err)
	}
	if _, err := client.CallTool(ctx, &sdkmcp.CallToolParams{Name: "sofarpc_target"}); err != nil {
		t.Fatalf("call target: %v", err)
	}
	entries := collectLogEntries(t, logs, 2)
	if !hasLogEntry(entries, "sofarpc_target", "resolving target scope") {
		t.Fatalf("logging entries missing target scope notification: %v", entries)
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
	invocationProperties := schemaObjectProperty(t, properties, "invocationProperties")
	if got := invocationProperties["type"]; got != "object" {
		t.Fatalf("invocationProperties type: got %#v want object", got)
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

func TestToolResultIncludesStructuredJSONTextMirror(t *testing.T) {
	out := InvokeOutput{Ok: true, Diagnostics: map[string]any{"capture": "session"}}
	result := invokeToolResult(out, "summary", false)
	if result.IsError {
		t.Fatal("result should not be marked as error")
	}
	if len(result.Content) != 2 {
		t.Fatalf("content count: got %d want 2", len(result.Content))
	}
	if got := contentText(t, result.Content[0]); got != "summary" {
		t.Fatalf("summary text: got %q want summary", got)
	}
	structured, ok := result.StructuredContent.(json.RawMessage)
	if !ok {
		t.Fatalf("structured content type: got %T want json.RawMessage", result.StructuredContent)
	}
	if got := contentText(t, result.Content[1]); got != string(structured) {
		t.Fatalf("json mirror: got %q want %q", got, string(structured))
	}
	var decoded InvokeOutput
	if err := json.Unmarshal(structured, &decoded); err != nil {
		t.Fatalf("unmarshal structured: %v", err)
	}
	if !decoded.Ok || decoded.Diagnostics["capture"] != "session" {
		t.Fatalf("decoded output: %+v", decoded)
	}
}

func connect(t *testing.T, ctx context.Context, server *sdkmcp.Server) *sdkmcp.ClientSession {
	t.Helper()
	return connectWithOptions(t, ctx, server, nil)
}

func connectWithOptions(t *testing.T, ctx context.Context, server *sdkmcp.Server, opts *sdkmcp.ClientOptions) *sdkmcp.ClientSession {
	t.Helper()
	serverSide, clientSide := sdkmcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverSide, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0.0.0"}, opts)
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

func readResourceText(t *testing.T, ctx context.Context, client *sdkmcp.ClientSession, uri string) string {
	t.Helper()
	result, err := client.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("read resource %s: %v", uri, err)
	}
	if len(result.Contents) != 1 {
		t.Fatalf("resource %s contents: got %d want 1", uri, len(result.Contents))
	}
	content := result.Contents[0]
	if content.MIMEType != resourceMIMEJSON {
		t.Fatalf("resource %s MIME type: got %q want %q", uri, content.MIMEType, resourceMIMEJSON)
	}
	if content.Text == "" {
		t.Fatalf("resource %s text is empty", uri)
	}
	return content.Text
}

func findResourceLink(content []sdkmcp.Content, uri string) *sdkmcp.ResourceLink {
	for _, item := range content {
		link, ok := item.(*sdkmcp.ResourceLink)
		if ok && link.URI == uri {
			return link
		}
	}
	return nil
}

func collectProgressMessages(t *testing.T, progress <-chan string, min int) []string {
	t.Helper()
	deadline := time.After(time.Second)
	var messages []string
	for len(messages) < min {
		select {
		case message := <-progress:
			messages = append(messages, message)
		case <-deadline:
			t.Fatalf("timed out waiting for progress notifications; got %v", messages)
		}
	}
	for {
		select {
		case message := <-progress:
			messages = append(messages, message)
		default:
			return messages
		}
	}
}

func collectLogEntries(t *testing.T, logs <-chan map[string]any, min int) []map[string]any {
	t.Helper()
	deadline := time.After(time.Second)
	var entries []map[string]any
	for len(entries) < min {
		select {
		case entry := <-logs:
			entries = append(entries, entry)
		case <-deadline:
			t.Fatalf("timed out waiting for logging notifications; got %v", entries)
		}
	}
	for {
		select {
		case entry := <-logs:
			entries = append(entries, entry)
		default:
			return entries
		}
	}
}

func hasLogEntry(entries []map[string]any, tool, message string) bool {
	for _, entry := range entries {
		if entry["tool"] == tool && entry["message"] == message {
			return true
		}
	}
	return false
}

func assertBoolHint(t *testing.T, toolName, field string, got *bool, want bool) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s %s: got nil want %v", toolName, field, want)
	}
	if *got != want {
		t.Fatalf("%s %s: got %v want %v", toolName, field, *got, want)
	}
}

func contentText(t *testing.T, content sdkmcp.Content) string {
	t.Helper()
	text, ok := content.(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content type: got %T want *mcp.TextContent", content)
	}
	return text.Text
}

func keys[V any](values map[string]V) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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
