package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	sessionadapter "github.com/hex1n/sofarpc-cli/internal/adapters/session"
	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/facadeconfig"
	"github.com/hex1n/sofarpc-cli/internal/facadeindex"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/projectscan"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPServerListsCoreTools(t *testing.T) {
	app := newTestMCPApp(t, t.TempDir())
	ctx := context.Background()
	clientSession, serverSession := connectMCP(t, ctx, app.MCPServer())
	defer clientSession.Close()
	defer serverSession.Close()

	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	want := []string{
		"describe_method",
		"inspect_session",
		"invoke_rpc",
		"list_facade_services",
		"open_workspace_session",
		"plan_invocation",
		"resolve_target",
		"resume_context",
	}
	if got, wantJSON := names, want; len(got) != len(wantJSON) {
		t.Fatalf("tool count = %d, want %d (%v)", len(got), len(wantJSON), got)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("tool names = %v, want %v", names, want)
		}
	}
}

func TestMCPServerOpenWorkspaceSessionAndResolveTarget(t *testing.T) {
	projectRoot := t.TempDir()
	app := newTestMCPApp(t, projectRoot)
	ctx := context.Background()
	clientSession, serverSession := connectMCP(t, ctx, app.MCPServer())
	defer clientSession.Close()
	defer serverSession.Close()

	openRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "open_workspace_session",
	})
	if err != nil {
		t.Fatalf("CallTool(open_workspace_session) error = %v", err)
	}
	if openRes.IsError {
		t.Fatalf("CallTool(open_workspace_session) returned tool error: %+v", openRes.Content)
	}
	var session model.WorkspaceSession
	decodeStructured(t, openRes.StructuredContent, &session)
	if !session.ManifestLoaded {
		t.Fatal("expected manifest to be loaded")
	}
	if session.ActiveContext != "dev" {
		t.Fatalf("session.ActiveContext = %q, want %q", session.ActiveContext, "dev")
	}

	resolveRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "resolve_target",
		Arguments: map[string]any{
			"session_id":         session.ID,
			"service":            "com.example.UserFacade",
			"include_candidates": true,
			"include_explain":    true,
		},
	})
	if err != nil {
		t.Fatalf("CallTool(resolve_target) error = %v", err)
	}
	if resolveRes.IsError {
		t.Fatalf("CallTool(resolve_target) returned tool error: %+v", resolveRes.Content)
	}
	var report targetReport
	decodeStructured(t, resolveRes.StructuredContent, &report)
	if report.Target.Mode != model.ModeDirect {
		t.Fatalf("target mode = %q, want %q", report.Target.Mode, model.ModeDirect)
	}
	if report.Target.DirectURL != "bolt://127.0.0.1:12200" {
		t.Fatalf("target direct url = %q", report.Target.DirectURL)
	}
	if report.Target.UniqueID != "user-facade" {
		t.Fatalf("target uniqueId = %q, want %q", report.Target.UniqueID, "user-facade")
	}
	if !report.ManifestLoaded {
		t.Fatal("expected report.ManifestLoaded = true")
	}
	if len(report.Candidates) == 0 {
		t.Fatal("expected candidate contexts when include_candidates is true")
	}
	if len(report.Explain) == 0 {
		t.Fatal("expected explanation lines when include_explain is true")
	}
	stored, ok, err := app.SessionService().Get(ctx, session.ID)
	if err != nil || !ok {
		t.Fatalf("SessionService().Get() = (%+v, %t, %v)", stored, ok, err)
	}
	if stored.LastTarget == nil {
		t.Fatal("expected session last target snapshot to be recorded")
	}
	if stored.LastTarget.Target.DirectURL != "bolt://127.0.0.1:12200" {
		t.Fatalf("stored.LastTarget.Target.DirectURL = %q", stored.LastTarget.Target.DirectURL)
	}
}

func TestMCPServerPlanInvocation(t *testing.T) {
	projectRoot := t.TempDir()
	app := newTestMCPApp(t, projectRoot)
	ctx := context.Background()
	clientSession, serverSession := connectMCP(t, ctx, app.MCPServer())
	defer clientSession.Close()
	defer serverSession.Close()

	session := openTestMCPWorkspaceSession(t, ctx, clientSession)
	runtimeJar := filepath.Join(t.TempDir(), "sofarpc-worker-5.7.6.jar")
	if err := os.WriteFile(runtimeJar, []byte("jar-bits"), 0o644); err != nil {
		t.Fatalf("WriteFile(runtimeJar) error = %v", err)
	}
	javaBin := filepath.Join(t.TempDir(), "java")
	if err := os.WriteFile(javaBin, []byte("#!/bin/sh\necho 'openjdk version \"17.0.8\"' >&2\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(javaBin) error = %v", err)
	}

	planRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "plan_invocation",
		Arguments: map[string]any{
			"session_id":   session.ID,
			"service":      "com.example.UserFacade",
			"method":       "getUser",
			"args":         []any{123},
			"param_types":  []string{"java.lang.Long"},
			"payload_mode": model.PayloadRaw,
			"runtime_jar":  runtimeJar,
			"java_bin":     javaBin,
		},
	})
	if err != nil {
		t.Fatalf("CallTool(plan_invocation) error = %v", err)
	}
	if planRes.IsError {
		t.Fatalf("CallTool(plan_invocation) returned tool error: %+v", planRes.Content)
	}

	var plan mcpPlanInvocationOutput
	decodeStructured(t, planRes.StructuredContent, &plan)
	if plan.Request.Service != "com.example.UserFacade" {
		t.Fatalf("plan.Request.Service = %q", plan.Request.Service)
	}
	if plan.Request.Method != "getUser" {
		t.Fatalf("plan.Request.Method = %q", plan.Request.Method)
	}
	if !sameParamTypes(plan.Request.ParamTypes, []string{"java.lang.Long"}) {
		t.Fatalf("plan.Request.ParamTypes = %v", plan.Request.ParamTypes)
	}
	if plan.Request.PayloadMode != model.PayloadRaw {
		t.Fatalf("plan.Request.PayloadMode = %q", plan.Request.PayloadMode)
	}
	if plan.Request.Target.DirectURL != "bolt://127.0.0.1:12200" {
		t.Fatalf("plan.Request.Target.DirectURL = %q", plan.Request.Target.DirectURL)
	}
	if plan.Request.Target.UniqueID != "user-facade" {
		t.Fatalf("plan.Request.Target.UniqueID = %q", plan.Request.Target.UniqueID)
	}
	if plan.Runtime.RuntimeJar == "" {
		t.Fatal("expected runtime jar in plan output")
	}
	if plan.Runtime.JavaMajor != "17" {
		t.Fatalf("plan.Runtime.JavaMajor = %q, want %q", plan.Runtime.JavaMajor, "17")
	}
	if plan.Runtime.WorkerClasspath != "runtime-only" {
		t.Fatalf("plan.Runtime.WorkerClasspath = %q", plan.Runtime.WorkerClasspath)
	}
	if plan.Prepared.Request.Service != plan.Request.Service {
		t.Fatalf("plan.Prepared.Request.Service = %q", plan.Prepared.Request.Service)
	}
	if plan.Prepared.Spec.RuntimeJar != runtimeJar {
		t.Fatalf("plan.Prepared.Spec.RuntimeJar = %q, want %q", plan.Prepared.Spec.RuntimeJar, runtimeJar)
	}
	if plan.Prepared.Spec.JavaBin != javaBin {
		t.Fatalf("plan.Prepared.Spec.JavaBin = %q, want %q", plan.Prepared.Spec.JavaBin, javaBin)
	}
	stored, ok, err := app.SessionService().Get(ctx, session.ID)
	if err != nil || !ok {
		t.Fatalf("SessionService().Get() = (%+v, %t, %v)", stored, ok, err)
	}
	if stored.LastPlan == nil {
		t.Fatal("expected session last plan snapshot to be recorded")
	}
	if stored.LastPlan.Method != "getUser" {
		t.Fatalf("stored.LastPlan.Method = %q", stored.LastPlan.Method)
	}
	if stored.LastPlan.Runtime.RuntimeJar != runtimeJar {
		t.Fatalf("stored.LastPlan.Runtime.RuntimeJar = %q", stored.LastPlan.Runtime.RuntimeJar)
	}
}

func TestMCPServerDescribeMethodRecordsSessionState(t *testing.T) {
	original := describeServiceFromProject
	t.Cleanup(func() {
		describeServiceFromProject = original
	})
	describeServiceFromProject = func(projectRoot, service string) (model.ServiceSchema, error) {
		if service != "com.example.UserFacade" {
			return model.ServiceSchema{}, fmt.Errorf("unexpected service %q", service)
		}
		return model.ServiceSchema{
			Service: service,
			Methods: []model.MethodSchema{
				{
					Name:       "getUser",
					ParamTypes: []string{"java.lang.Long"},
					ReturnType: "com.example.UserDTO",
				},
			},
		}, nil
	}

	projectRoot := t.TempDir()
	app := newTestMCPApp(t, projectRoot)
	ctx := context.Background()
	clientSession, serverSession := connectMCP(t, ctx, app.MCPServer())
	defer clientSession.Close()
	defer serverSession.Close()

	session := openTestMCPWorkspaceSession(t, ctx, clientSession)
	targetRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "resolve_target",
		Arguments: map[string]any{
			"session_id": session.ID,
			"service":    "com.example.UserFacade",
			"direct_url": "bolt://127.0.0.1:22334",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(resolve_target) error = %v", err)
	}
	if targetRes.IsError {
		t.Fatalf("CallTool(resolve_target) returned tool error: %+v", targetRes.Content)
	}
	describeRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "describe_method",
		Arguments: map[string]any{
			"session_id":            session.ID,
			"service":               "com.example.UserFacade",
			"method":                "getUser",
			"preferred_param_types": []string{"java.lang.Long"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(describe_method) error = %v", err)
	}
	if describeRes.IsError {
		t.Fatalf("CallTool(describe_method) returned tool error: %+v", describeRes.Content)
	}

	var output mcpDescribeMethodOutput
	decodeStructured(t, describeRes.StructuredContent, &output)
	if output.Selected == nil || !sameParamTypes(output.Selected.ParamTypes, []string{"java.lang.Long"}) {
		t.Fatalf("output.Selected = %+v", output.Selected)
	}
	stored, ok, err := app.SessionService().Get(ctx, session.ID)
	if err != nil || !ok {
		t.Fatalf("SessionService().Get() = (%+v, %t, %v)", stored, ok, err)
	}
	if stored.LastDescribe == nil {
		t.Fatal("expected session last describe snapshot to be recorded")
	}
	if stored.LastDescribe.Method != "getUser" {
		t.Fatalf("stored.LastDescribe.Method = %q", stored.LastDescribe.Method)
	}
	if stored.LastDescribe.Selected == nil || !sameParamTypes(stored.LastDescribe.Selected.ParamTypes, []string{"java.lang.Long"}) {
		t.Fatalf("stored.LastDescribe.Selected = %+v", stored.LastDescribe.Selected)
	}
}

func TestMCPServerDescribeMethodUsesLastSessionServiceWhenOmitted(t *testing.T) {
	original := describeServiceFromProject
	t.Cleanup(func() {
		describeServiceFromProject = original
	})
	describeServiceFromProject = func(projectRoot, service string) (model.ServiceSchema, error) {
		if service != "com.example.UserFacade" {
			return model.ServiceSchema{}, fmt.Errorf("unexpected service %q", service)
		}
		return model.ServiceSchema{
			Service: service,
			Methods: []model.MethodSchema{
				{
					Name:       "getUser",
					ParamTypes: []string{"java.lang.Long"},
					ReturnType: "com.example.UserDTO",
				},
			},
		}, nil
	}

	projectRoot := t.TempDir()
	app := newTestMCPApp(t, projectRoot)
	ctx := context.Background()
	clientSession, serverSession := connectMCP(t, ctx, app.MCPServer())
	defer clientSession.Close()
	defer serverSession.Close()

	session := openTestMCPWorkspaceSession(t, ctx, clientSession)
	firstDescribe, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "describe_method",
		Arguments: map[string]any{
			"session_id":            session.ID,
			"service":               "com.example.UserFacade",
			"method":                "getUser",
			"preferred_param_types": []string{"java.lang.Long"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(describe_method initial) error = %v", err)
	}
	if firstDescribe.IsError {
		t.Fatalf("CallTool(describe_method initial) returned tool error: %+v", firstDescribe.Content)
	}

	describeRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "describe_method",
		Arguments: map[string]any{
			"session_id": session.ID,
			"method":     "getUser",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(describe_method) error = %v", err)
	}
	if describeRes.IsError {
		t.Fatalf("CallTool(describe_method) returned tool error: %+v", describeRes.Content)
	}

	var output mcpDescribeMethodOutput
	decodeStructured(t, describeRes.StructuredContent, &output)
	if output.Service != "com.example.UserFacade" {
		t.Fatalf("output.Service = %q", output.Service)
	}
	if output.Selected == nil || !sameParamTypes(output.Selected.ParamTypes, []string{"java.lang.Long"}) {
		t.Fatalf("output.Selected = %+v", output.Selected)
	}
}

func TestMCPServerResolveTargetUsesLastSessionServiceWhenOmitted(t *testing.T) {
	original := describeServiceFromProject
	t.Cleanup(func() {
		describeServiceFromProject = original
	})
	describeServiceFromProject = func(projectRoot, service string) (model.ServiceSchema, error) {
		if service != "com.example.UserFacade" {
			return model.ServiceSchema{}, fmt.Errorf("unexpected service %q", service)
		}
		return model.ServiceSchema{
			Service: service,
			Methods: []model.MethodSchema{
				{
					Name:       "getUser",
					ParamTypes: []string{"java.lang.Long"},
					ReturnType: "com.example.UserDTO",
				},
			},
		}, nil
	}

	projectRoot := t.TempDir()
	app := newTestMCPApp(t, projectRoot)
	ctx := context.Background()
	clientSession, serverSession := connectMCP(t, ctx, app.MCPServer())
	defer clientSession.Close()
	defer serverSession.Close()

	session := openTestMCPWorkspaceSession(t, ctx, clientSession)
	describeRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "describe_method",
		Arguments: map[string]any{
			"session_id":            session.ID,
			"service":               "com.example.UserFacade",
			"method":                "getUser",
			"preferred_param_types": []string{"java.lang.Long"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(describe_method) error = %v", err)
	}
	if describeRes.IsError {
		t.Fatalf("CallTool(describe_method) returned tool error: %+v", describeRes.Content)
	}

	resolveRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "resolve_target",
		Arguments: map[string]any{
			"session_id": session.ID,
		},
	})
	if err != nil {
		t.Fatalf("CallTool(resolve_target) error = %v", err)
	}
	if resolveRes.IsError {
		t.Fatalf("CallTool(resolve_target) returned tool error: %+v", resolveRes.Content)
	}

	var report targetReport
	decodeStructured(t, resolveRes.StructuredContent, &report)
	if report.Target.UniqueID != "user-facade" {
		t.Fatalf("report.Target.UniqueID = %q", report.Target.UniqueID)
	}
	if report.Target.DirectURL != "bolt://127.0.0.1:12200" {
		t.Fatalf("report.Target.DirectURL = %q", report.Target.DirectURL)
	}

	stored, ok, err := app.SessionService().Get(ctx, session.ID)
	if err != nil || !ok {
		t.Fatalf("SessionService().Get() = (%+v, %t, %v)", stored, ok, err)
	}
	if stored.LastTarget == nil {
		t.Fatal("expected session last target snapshot to be recorded")
	}
	if stored.LastTarget.Service != "com.example.UserFacade" {
		t.Fatalf("stored.LastTarget.Service = %q", stored.LastTarget.Service)
	}
}

func TestMCPServerResumeContextSuggestsInvokeRPCFromLastPlan(t *testing.T) {
	original := describeServiceFromProject
	t.Cleanup(func() {
		describeServiceFromProject = original
	})
	describeServiceFromProject = func(projectRoot, service string) (model.ServiceSchema, error) {
		if service != "com.example.UserFacade" {
			return model.ServiceSchema{}, fmt.Errorf("unexpected service %q", service)
		}
		return model.ServiceSchema{
			Service: service,
			Methods: []model.MethodSchema{
				{
					Name:       "getUser",
					ParamTypes: []string{"java.lang.Long"},
					ReturnType: "com.example.UserDTO",
				},
			},
		}, nil
	}

	projectRoot := t.TempDir()
	app := newTestMCPApp(t, projectRoot)
	ctx := context.Background()
	clientSession, serverSession := connectMCP(t, ctx, app.MCPServer())
	defer clientSession.Close()
	defer serverSession.Close()

	session := openTestMCPWorkspaceSession(t, ctx, clientSession)
	resolveRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "resolve_target",
		Arguments: map[string]any{
			"session_id": session.ID,
			"service":    "com.example.UserFacade",
			"direct_url": "bolt://127.0.0.1:22334",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(resolve_target) error = %v", err)
	}
	if resolveRes.IsError {
		t.Fatalf("CallTool(resolve_target) returned tool error: %+v", resolveRes.Content)
	}

	describeRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "describe_method",
		Arguments: map[string]any{
			"session_id":            session.ID,
			"method":                "getUser",
			"preferred_param_types": []string{"java.lang.Long"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(describe_method) error = %v", err)
	}
	if describeRes.IsError {
		t.Fatalf("CallTool(describe_method) returned tool error: %+v", describeRes.Content)
	}

	runtimeJar := filepath.Join(t.TempDir(), "sofarpc-worker-5.7.6.jar")
	if err := os.WriteFile(runtimeJar, []byte("jar-bits"), 0o644); err != nil {
		t.Fatalf("WriteFile(runtimeJar) error = %v", err)
	}
	javaBin := filepath.Join(t.TempDir(), "java")
	if err := os.WriteFile(javaBin, []byte("#!/bin/sh\necho 'openjdk version \"17.0.8\"' >&2\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(javaBin) error = %v", err)
	}

	planRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "plan_invocation",
		Arguments: map[string]any{
			"session_id":   session.ID,
			"args":         []any{123},
			"payload_mode": model.PayloadRaw,
			"runtime_jar":  runtimeJar,
			"java_bin":     javaBin,
		},
	})
	if err != nil {
		t.Fatalf("CallTool(plan_invocation) error = %v", err)
	}
	if planRes.IsError {
		t.Fatalf("CallTool(plan_invocation) returned tool error: %+v", planRes.Content)
	}

	resumeRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "resume_context",
		Arguments: map[string]any{
			"session_id": session.ID,
		},
	})
	if err != nil {
		t.Fatalf("CallTool(resume_context) error = %v", err)
	}
	if resumeRes.IsError {
		t.Fatalf("CallTool(resume_context) returned tool error: %+v", resumeRes.Content)
	}

	var output mcpResumeContextOutput
	decodeStructured(t, resumeRes.StructuredContent, &output)
	if output.Summary.Service != "com.example.UserFacade" {
		t.Fatalf("output.Summary.Service = %q", output.Summary.Service)
	}
	if output.Summary.Method != "getUser" {
		t.Fatalf("output.Summary.Method = %q", output.Summary.Method)
	}
	if !sameParamTypes(output.Summary.ParamTypes, []string{"java.lang.Long"}) {
		t.Fatalf("output.Summary.ParamTypes = %v", output.Summary.ParamTypes)
	}
	if !output.Summary.HasTarget || !output.Summary.HasDescribe || !output.Summary.HasPlan || !output.Summary.CanInvoke {
		t.Fatalf("output.Summary = %+v", output.Summary)
	}
	if output.SuggestedAction.Tool != "invoke_rpc" {
		t.Fatalf("output.SuggestedAction.Tool = %q", output.SuggestedAction.Tool)
	}
	if output.SuggestedAction.Arguments["session_id"] != session.ID {
		t.Fatalf("output.SuggestedAction.Arguments = %#v", output.SuggestedAction.Arguments)
	}
}

func TestMCPServerResumeContextSuggestsPlanInvocationDraft(t *testing.T) {
	original := describeServiceFromProject
	t.Cleanup(func() {
		describeServiceFromProject = original
	})
	describeServiceFromProject = func(projectRoot, service string) (model.ServiceSchema, error) {
		if service != "com.example.UserFacade" {
			return model.ServiceSchema{}, fmt.Errorf("unexpected service %q", service)
		}
		return model.ServiceSchema{
			Service: service,
			Methods: []model.MethodSchema{
				{
					Name:       "getUser",
					ParamTypes: []string{"java.lang.Long"},
					ReturnType: "com.example.UserDTO",
				},
			},
		}, nil
	}

	projectRoot := t.TempDir()
	app := newTestMCPApp(t, projectRoot)
	ctx := context.Background()
	clientSession, serverSession := connectMCP(t, ctx, app.MCPServer())
	defer clientSession.Close()
	defer serverSession.Close()

	session := openTestMCPWorkspaceSession(t, ctx, clientSession)
	resolveRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "resolve_target",
		Arguments: map[string]any{
			"session_id": session.ID,
			"service":    "com.example.UserFacade",
			"direct_url": "bolt://127.0.0.1:22334",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(resolve_target) error = %v", err)
	}
	if resolveRes.IsError {
		t.Fatalf("CallTool(resolve_target) returned tool error: %+v", resolveRes.Content)
	}

	describeRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "describe_method",
		Arguments: map[string]any{
			"session_id":            session.ID,
			"method":                "getUser",
			"preferred_param_types": []string{"java.lang.Long"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(describe_method) error = %v", err)
	}
	if describeRes.IsError {
		t.Fatalf("CallTool(describe_method) returned tool error: %+v", describeRes.Content)
	}

	resumeRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "resume_context",
		Arguments: map[string]any{
			"session_id": session.ID,
		},
	})
	if err != nil {
		t.Fatalf("CallTool(resume_context) error = %v", err)
	}
	if resumeRes.IsError {
		t.Fatalf("CallTool(resume_context) returned tool error: %+v", resumeRes.Content)
	}

	var output mcpResumeContextOutput
	decodeStructured(t, resumeRes.StructuredContent, &output)
	if output.SuggestedAction.Tool != "plan_invocation" {
		t.Fatalf("output.SuggestedAction.Tool = %q", output.SuggestedAction.Tool)
	}
	if output.SuggestedAction.Arguments["session_id"] != session.ID {
		t.Fatalf("output.SuggestedAction.Arguments = %#v", output.SuggestedAction.Arguments)
	}
	if output.SuggestedAction.Arguments["service"] != "com.example.UserFacade" {
		t.Fatalf("output.SuggestedAction.Arguments = %#v", output.SuggestedAction.Arguments)
	}
	if output.SuggestedAction.Arguments["method"] != "getUser" {
		t.Fatalf("output.SuggestedAction.Arguments = %#v", output.SuggestedAction.Arguments)
	}
	paramTypes, ok := output.SuggestedAction.Arguments["param_types"].([]any)
	if !ok || len(paramTypes) != 1 || paramTypes[0] != "java.lang.Long" {
		t.Fatalf("output.SuggestedAction.Arguments[param_types] = %#v", output.SuggestedAction.Arguments["param_types"])
	}
	args, ok := output.SuggestedAction.Arguments["args"].([]any)
	if !ok || len(args) != 1 || args[0] != float64(0) {
		t.Fatalf("output.SuggestedAction.Arguments[args] = %#v", output.SuggestedAction.Arguments["args"])
	}
	if output.SuggestedAction.Arguments["payload_mode"] != model.PayloadRaw {
		t.Fatalf("output.SuggestedAction.Arguments[payload_mode] = %#v", output.SuggestedAction.Arguments["payload_mode"])
	}
	if output.SuggestedAction.Draft == nil {
		t.Fatal("expected output.SuggestedAction.Draft")
	}
	if output.SuggestedAction.Draft.ArgsSource != "param_types" {
		t.Fatalf("output.SuggestedAction.Draft.ArgsSource = %q", output.SuggestedAction.Draft.ArgsSource)
	}
	if len(output.SuggestedAction.Draft.Parameters) != 1 {
		t.Fatalf("len(output.SuggestedAction.Draft.Parameters) = %d", len(output.SuggestedAction.Draft.Parameters))
	}
	if output.SuggestedAction.Draft.Parameters[0].Name != "arg0" {
		t.Fatalf("output.SuggestedAction.Draft.Parameters[0].Name = %q", output.SuggestedAction.Draft.Parameters[0].Name)
	}
	if output.SuggestedAction.Draft.Parameters[0].Type != "java.lang.Long" {
		t.Fatalf("output.SuggestedAction.Draft.Parameters[0].Type = %q", output.SuggestedAction.Draft.Parameters[0].Type)
	}
}

func TestMCPServerPlanInvocationUsesLastDescribeSelectionWhenTypesOmitted(t *testing.T) {
	original := describeServiceFromProject
	t.Cleanup(func() {
		describeServiceFromProject = original
	})
	describeServiceFromProject = func(projectRoot, service string) (model.ServiceSchema, error) {
		return model.ServiceSchema{
			Service: service,
			Methods: []model.MethodSchema{
				{
					Name:       "getUser",
					ParamTypes: []string{"java.lang.Long"},
					ReturnType: "com.example.UserDTO",
				},
			},
		}, nil
	}

	projectRoot := t.TempDir()
	app := newTestMCPApp(t, projectRoot)
	ctx := context.Background()
	clientSession, serverSession := connectMCP(t, ctx, app.MCPServer())
	defer clientSession.Close()
	defer serverSession.Close()

	session := openTestMCPWorkspaceSession(t, ctx, clientSession)
	targetRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "resolve_target",
		Arguments: map[string]any{
			"session_id": session.ID,
			"service":    "com.example.UserFacade",
			"direct_url": "bolt://127.0.0.1:22334",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(resolve_target) error = %v", err)
	}
	if targetRes.IsError {
		t.Fatalf("CallTool(resolve_target) returned tool error: %+v", targetRes.Content)
	}

	describeRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "describe_method",
		Arguments: map[string]any{
			"session_id":            session.ID,
			"service":               "com.example.UserFacade",
			"method":                "getUser",
			"preferred_param_types": []string{"java.lang.Long"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(describe_method) error = %v", err)
	}
	if describeRes.IsError {
		t.Fatalf("CallTool(describe_method) returned tool error: %+v", describeRes.Content)
	}

	describeServiceFromProject = func(projectRoot, service string) (model.ServiceSchema, error) {
		return model.ServiceSchema{}, fmt.Errorf("describe should not be needed after session selection")
	}

	runtimeJar := filepath.Join(t.TempDir(), "sofarpc-worker-5.7.6.jar")
	if err := os.WriteFile(runtimeJar, []byte("jar-bits"), 0o644); err != nil {
		t.Fatalf("WriteFile(runtimeJar) error = %v", err)
	}
	javaBin := filepath.Join(t.TempDir(), "java")
	if err := os.WriteFile(javaBin, []byte("#!/bin/sh\necho 'openjdk version \"17.0.8\"' >&2\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(javaBin) error = %v", err)
	}

	planRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "plan_invocation",
		Arguments: map[string]any{
			"session_id":   session.ID,
			"args":         []any{123},
			"payload_mode": model.PayloadRaw,
			"runtime_jar":  runtimeJar,
			"java_bin":     javaBin,
		},
	})
	if err != nil {
		t.Fatalf("CallTool(plan_invocation) error = %v", err)
	}
	if planRes.IsError {
		t.Fatalf("CallTool(plan_invocation) returned tool error: %+v", planRes.Content)
	}

	var plan mcpPlanInvocationOutput
	decodeStructured(t, planRes.StructuredContent, &plan)
	if !sameParamTypes(plan.Request.ParamTypes, []string{"java.lang.Long"}) {
		t.Fatalf("plan.Request.ParamTypes = %v", plan.Request.ParamTypes)
	}
	if plan.Request.Service != "com.example.UserFacade" || plan.Request.Method != "getUser" {
		t.Fatalf("plan request = %+v", plan.Request)
	}
	if plan.Request.Target.DirectURL != "bolt://127.0.0.1:22334" {
		t.Fatalf("plan.Request.Target.DirectURL = %q", plan.Request.Target.DirectURL)
	}
}

func TestMCPServerListFacadeServices(t *testing.T) {
	projectRoot := t.TempDir()
	writeFacadeIndexFixture(t, projectRoot)
	app := newTestMCPApp(t, projectRoot)
	ctx := context.Background()
	clientSession, serverSession := connectMCP(t, ctx, app.MCPServer())
	defer clientSession.Close()
	defer serverSession.Close()

	session := openTestMCPWorkspaceSession(t, ctx, clientSession)
	listRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "list_facade_services",
		Arguments: map[string]any{
			"session_id": session.ID,
		},
	})
	if err != nil {
		t.Fatalf("CallTool(list_facade_services) error = %v", err)
	}
	if listRes.IsError {
		t.Fatalf("CallTool(list_facade_services) returned tool error: %+v", listRes.Content)
	}

	var output mcpListFacadeServicesOutput
	decodeStructured(t, listRes.StructuredContent, &output)
	if !output.FacadeConfigured {
		t.Fatal("expected facade to be configured")
	}
	if !output.IndexAvailable {
		t.Fatal("expected facade index to be available")
	}
	if len(output.SourceRoots) != 1 || output.SourceRoots[0] != "user-facade/src/main/java" {
		t.Fatalf("output.SourceRoots = %v", output.SourceRoots)
	}
	if len(output.Services) != 1 {
		t.Fatalf("len(output.Services) = %d, want 1", len(output.Services))
	}
	if output.Services[0].Service != "com.example.UserFacade" {
		t.Fatalf("output.Services[0].Service = %q", output.Services[0].Service)
	}
	if !sameParamTypes(output.Services[0].Methods, []string{"getUser", "listUsers"}) {
		t.Fatalf("output.Services[0].Methods = %v", output.Services[0].Methods)
	}
}

func TestMCPServerInspectSessionReturnsRecordedState(t *testing.T) {
	original := describeServiceFromProject
	t.Cleanup(func() {
		describeServiceFromProject = original
	})
	describeServiceFromProject = func(projectRoot, service string) (model.ServiceSchema, error) {
		return model.ServiceSchema{
			Service: service,
			Methods: []model.MethodSchema{
				{
					Name:       "getUser",
					ParamTypes: []string{"java.lang.Long"},
					ReturnType: "com.example.UserDTO",
				},
			},
		}, nil
	}

	projectRoot := t.TempDir()
	app := newTestMCPApp(t, projectRoot)
	ctx := context.Background()
	clientSession, serverSession := connectMCP(t, ctx, app.MCPServer())
	defer clientSession.Close()
	defer serverSession.Close()

	session := openTestMCPWorkspaceSession(t, ctx, clientSession)
	runtimeJar := filepath.Join(t.TempDir(), "sofarpc-worker-5.7.6.jar")
	if err := os.WriteFile(runtimeJar, []byte("jar-bits"), 0o644); err != nil {
		t.Fatalf("WriteFile(runtimeJar) error = %v", err)
	}
	javaBin := filepath.Join(t.TempDir(), "java")
	if err := os.WriteFile(javaBin, []byte("#!/bin/sh\necho 'openjdk version \"17.0.8\"' >&2\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(javaBin) error = %v", err)
	}

	resolveRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "resolve_target",
		Arguments: map[string]any{
			"session_id": session.ID,
			"service":    "com.example.UserFacade",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(resolve_target) error = %v", err)
	}
	if resolveRes.IsError {
		t.Fatalf("CallTool(resolve_target) returned tool error: %+v", resolveRes.Content)
	}
	describeRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "describe_method",
		Arguments: map[string]any{
			"session_id":            session.ID,
			"service":               "com.example.UserFacade",
			"method":                "getUser",
			"preferred_param_types": []string{"java.lang.Long"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(describe_method) error = %v", err)
	}
	if describeRes.IsError {
		t.Fatalf("CallTool(describe_method) returned tool error: %+v", describeRes.Content)
	}
	planRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "plan_invocation",
		Arguments: map[string]any{
			"session_id":   session.ID,
			"service":      "com.example.UserFacade",
			"method":       "getUser",
			"args":         []any{123},
			"param_types":  []string{"java.lang.Long"},
			"payload_mode": model.PayloadRaw,
			"runtime_jar":  runtimeJar,
			"java_bin":     javaBin,
		},
	})
	if err != nil {
		t.Fatalf("CallTool(plan_invocation) error = %v", err)
	}
	if planRes.IsError {
		t.Fatalf("CallTool(plan_invocation) returned tool error: %+v", planRes.Content)
	}

	inspectRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "inspect_session",
		Arguments: map[string]any{
			"session_id": session.ID,
		},
	})
	if err != nil {
		t.Fatalf("CallTool(inspect_session) error = %v", err)
	}
	if inspectRes.IsError {
		t.Fatalf("CallTool(inspect_session) returned tool error: %+v", inspectRes.Content)
	}

	var inspected model.WorkspaceSession
	decodeStructured(t, inspectRes.StructuredContent, &inspected)
	if inspected.ID != session.ID {
		t.Fatalf("inspected.ID = %q, want %q", inspected.ID, session.ID)
	}
	if inspected.LastTarget == nil {
		t.Fatal("expected inspected.LastTarget")
	}
	if inspected.LastTarget.Target.DirectURL != "bolt://127.0.0.1:12200" {
		t.Fatalf("inspected.LastTarget.Target.DirectURL = %q", inspected.LastTarget.Target.DirectURL)
	}
	if inspected.LastPlan == nil {
		t.Fatal("expected inspected.LastPlan")
	}
	if inspected.LastPlan.Method != "getUser" {
		t.Fatalf("inspected.LastPlan.Method = %q", inspected.LastPlan.Method)
	}
	if inspected.LastPlan.Runtime.RuntimeJar != runtimeJar {
		t.Fatalf("inspected.LastPlan.Runtime.RuntimeJar = %q", inspected.LastPlan.Runtime.RuntimeJar)
	}
	if inspected.LastDescribe == nil {
		t.Fatal("expected inspected.LastDescribe")
	}
	if inspected.LastDescribe.Method != "getUser" {
		t.Fatalf("inspected.LastDescribe.Method = %q", inspected.LastDescribe.Method)
	}
	if inspected.LastDescribe.Selected == nil || !sameParamTypes(inspected.LastDescribe.Selected.ParamTypes, []string{"java.lang.Long"}) {
		t.Fatalf("inspected.LastDescribe.Selected = %+v", inspected.LastDescribe.Selected)
	}
}

func TestMCPServerInvokeRPCUsesLastPlanWhenServiceAndMethodAreOmitted(t *testing.T) {
	projectRoot := t.TempDir()
	app := newTestMCPApp(t, projectRoot)
	ctx := context.Background()
	clientSession, serverSession := connectMCP(t, ctx, app.MCPServer())
	defer clientSession.Close()
	defer serverSession.Close()

	session := openTestMCPWorkspaceSession(t, ctx, clientSession)
	runtimeJar := filepath.Join(t.TempDir(), "sofarpc-worker-5.7.6.jar")
	if err := os.WriteFile(runtimeJar, []byte("jar-bits"), 0o644); err != nil {
		t.Fatalf("WriteFile(runtimeJar) error = %v", err)
	}
	javaBin := filepath.Join(t.TempDir(), "java")
	if err := os.WriteFile(javaBin, []byte("#!/bin/sh\necho 'openjdk version \"17.0.8\"' >&2\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(javaBin) error = %v", err)
	}

	planRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "plan_invocation",
		Arguments: map[string]any{
			"session_id":   session.ID,
			"service":      "com.example.UserFacade",
			"method":       "getUser",
			"args":         []any{123},
			"param_types":  []string{"java.lang.Long"},
			"payload_mode": model.PayloadRaw,
			"runtime_jar":  runtimeJar,
			"java_bin":     javaBin,
		},
	})
	if err != nil {
		t.Fatalf("CallTool(plan_invocation) error = %v", err)
	}
	if planRes.IsError {
		t.Fatalf("CallTool(plan_invocation) returned tool error: %+v", planRes.Content)
	}
	var plan mcpPlanInvocationOutput
	decodeStructured(t, planRes.StructuredContent, &plan)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()
	requests := make(chan model.InvocationRequest, 1)
	serverErrs := make(chan error, 1)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				serverErrs <- err
				return
			}
			func() {
				defer conn.Close()
				var request model.InvocationRequest
				if err := json.NewDecoder(conn).Decode(&request); err != nil {
					if err == io.EOF {
						return
					}
					serverErrs <- err
					return
				}
				requests <- request
				serverErrs <- json.NewEncoder(conn).Encode(model.InvocationResponse{
					RequestID: request.RequestID,
					OK:        true,
					Result:    json.RawMessage(`{"user":"ok"}`),
				})
			}()
			select {
			case err := <-serverErrs:
				serverErrs <- err
				return
			default:
			}
		}
	}()

	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}
	port, err := net.LookupPort("tcp", portText)
	if err != nil {
		t.Fatalf("LookupPort() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.Prepared.Spec.MetadataFile), 0o755); err != nil {
		t.Fatalf("MkdirAll(metadata dir) error = %v", err)
	}
	metadataBody, err := json.Marshal(model.DaemonMetadata{
		Host:           host,
		Port:           port,
		StartedAt:      "2026-04-18T12:00:00Z",
		RuntimeVersion: plan.Prepared.Spec.SofaRPCVersion,
		JavaMajor:      plan.Prepared.Spec.JavaMajor,
		DaemonProfile:  plan.Prepared.Spec.DaemonProfile,
		RuntimeDigest:  plan.Prepared.Spec.RuntimeDigest,
	})
	if err != nil {
		t.Fatalf("json.Marshal(metadata) error = %v", err)
	}
	if err := os.WriteFile(plan.Prepared.Spec.MetadataFile, metadataBody, 0o644); err != nil {
		t.Fatalf("WriteFile(metadata) error = %v", err)
	}

	invokeRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "invoke_rpc",
		Arguments: map[string]any{
			"session_id": session.ID,
		},
	})
	if err != nil {
		t.Fatalf("CallTool(invoke_rpc) error = %v", err)
	}
	if invokeRes.IsError {
		t.Fatalf("CallTool(invoke_rpc) returned tool error: %+v", invokeRes.Content)
	}

	select {
	case request := <-requests:
		if request.Service != "com.example.UserFacade" {
			t.Fatalf("request.Service = %q", request.Service)
		}
		if request.Method != "getUser" {
			t.Fatalf("request.Method = %q", request.Method)
		}
		if !sameParamTypes(request.ParamTypes, []string{"java.lang.Long"}) {
			t.Fatalf("request.ParamTypes = %v", request.ParamTypes)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected invoke_rpc to use the stored last plan")
	}

	select {
	case err := <-serverErrs:
		if err != nil {
			t.Fatalf("mock server error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for mock server completion")
	}

	var output mcpInvokeRPCOutput
	decodeStructured(t, invokeRes.StructuredContent, &output)
	if !output.Response.OK {
		t.Fatalf("output.Response.OK = false, response=%+v", output.Response)
	}
	resultMap, ok := output.Result.(map[string]any)
	if !ok || resultMap["user"] != "ok" {
		t.Fatalf("output.Result = %#v", output.Result)
	}
}

func newTestMCPApp(t *testing.T, cwd string) *App {
	t.Helper()
	configDir := filepath.Join(t.TempDir(), "config")
	cacheDir := filepath.Join(t.TempDir(), "cache")
	paths := config.Paths{
		ConfigDir:           configDir,
		CacheDir:            cacheDir,
		ContextsFile:        filepath.Join(configDir, "contexts.json"),
		RuntimeSourcesFile:  filepath.Join(configDir, "runtime-sources.json"),
		ContextTemplateFile: filepath.Join(configDir, "contexts.template.json"),
	}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("paths.Ensure() error = %v", err)
	}
	if err := config.SaveContextStore(paths, model.ContextStore{
		Active: "dev",
		Contexts: map[string]model.Context{
			"dev": {
				Name:             "dev",
				ProjectRoot:      cwd,
				Mode:             model.ModeDirect,
				DirectURL:        "bolt://127.0.0.1:12200",
				Protocol:         "bolt",
				Serialization:    "hessian2",
				TimeoutMS:        3000,
				ConnectTimeoutMS: 1000,
			},
		},
	}); err != nil {
		t.Fatalf("SaveContextStore() error = %v", err)
	}
	if err := config.SaveManifest(filepath.Join(cwd, "sofarpc.manifest.json"), model.Manifest{
		SchemaVersion:  "v1alpha1",
		SofaRPCVersion: defaultSofaRPCVersion,
		DefaultContext: "dev",
		Services: map[string]model.ServiceConfig{
			"com.example.UserFacade": {
				UniqueID: "user-facade",
			},
		},
	}); err != nil {
		t.Fatalf("SaveManifest() error = %v", err)
	}
	return &App{
		Stdin:    nil,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Cwd:      cwd,
		Paths:    paths,
		Runtime:  runtime.NewManager(paths, cwd),
		Metadata: nil,
		Sessions: sessionadapter.NewMemoryStore(),
	}
}

func connectMCP(t *testing.T, ctx context.Context, server *mcp.Server) (*mcp.ClientSession, *mcp.ServerSession) {
	t.Helper()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	return clientSession, serverSession
}

func openTestMCPWorkspaceSession(t *testing.T, ctx context.Context, clientSession *mcp.ClientSession) model.WorkspaceSession {
	t.Helper()
	openRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "open_workspace_session",
	})
	if err != nil {
		t.Fatalf("CallTool(open_workspace_session) error = %v", err)
	}
	if openRes.IsError {
		t.Fatalf("CallTool(open_workspace_session) returned tool error: %+v", openRes.Content)
	}
	var session model.WorkspaceSession
	decodeStructured(t, openRes.StructuredContent, &session)
	return session
}

func writeFacadeIndexFixture(t *testing.T, projectRoot string) {
	t.Helper()
	cfg := facadeconfig.DefaultConfig()
	cfg.FacadeModules = []projectscan.FacadeModule{
		{
			Name:            "user-facade",
			SourceRoot:      "user-facade/src/main/java",
			MavenModulePath: "user-facade",
			JarGlob:         "user-facade/target/user-facade-*.jar",
			DepsDir:         "user-facade/target/facade-deps",
		},
	}
	if err := facadeconfig.SaveJSON(filepath.Join(projectRoot, ".sofarpc", "config.json"), cfg); err != nil {
		t.Fatalf("SaveJSON(config) error = %v", err)
	}
	summary := facadeindex.IndexSummary{
		SourceRoots:       []string{"user-facade/src/main/java"},
		InterfaceSuffixes: append([]string{}, cfg.InterfaceSuffixes...),
		Services: []facadeindex.IndexSummaryService{
			{
				Service: "com.example.UserFacade",
				File:    "user-facade/src/main/java/com/example/UserFacade.java",
				Methods: []string{"getUser", "listUsers"},
			},
		},
	}
	if err := facadeconfig.SaveJSON(filepath.Join(projectRoot, ".sofarpc", "index", "_index.json"), summary); err != nil {
		t.Fatalf("SaveJSON(index) error = %v", err)
	}
}

func decodeStructured(t *testing.T, structured any, out any) {
	t.Helper()
	body, err := json.Marshal(structured)
	if err != nil {
		t.Fatalf("json.Marshal(structured) error = %v", err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("json.Unmarshal(structured) error = %v\nbody=%s", err, string(body))
	}
}
