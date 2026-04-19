package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	appdescribe "github.com/hex1n/sofarpc-cli/internal/app/describe"
	appfacade "github.com/hex1n/sofarpc-cli/internal/app/facade"
	appinvoke "github.com/hex1n/sofarpc-cli/internal/app/invoke"
	appsession "github.com/hex1n/sofarpc-cli/internal/app/session"
	appshared "github.com/hex1n/sofarpc-cli/internal/app/shared"
	apptarget "github.com/hex1n/sofarpc-cli/internal/app/target"
	"github.com/hex1n/sofarpc-cli/internal/config"
	"github.com/hex1n/sofarpc-cli/internal/facadeschema"
	"github.com/hex1n/sofarpc-cli/internal/model"
	"github.com/hex1n/sofarpc-cli/internal/runtime"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const ServerVersion = "dev"

type Provider interface {
	WorkingDir() string
	ConfigPaths() config.Paths
	SessionService() appsession.Deps
	TargetService() apptarget.Deps
	DescribeService() appdescribe.Deps
	InvokeService() appinvoke.Deps
	FacadeService() appfacade.Deps
}

type OpenWorkspaceSessionInput struct {
	ProjectRoot  string `json:"project_root,omitempty" jsonschema:"optional project root override; defaults to the current workspace"`
	ManifestPath string `json:"manifest_path,omitempty" jsonschema:"optional manifest path override"`
	ContextName  string `json:"context_name,omitempty" jsonschema:"optional context name override"`
}

type InspectSessionInput struct {
	SessionID string `json:"session_id" jsonschema:"workspace session id created by open_workspace_session"`
}

type ResumeContextInput struct {
	SessionID string `json:"session_id" jsonschema:"workspace session id created by open_workspace_session"`
}

type ResolveTargetInput struct {
	SessionID         string `json:"session_id" jsonschema:"workspace session id created by open_workspace_session"`
	Service           string `json:"service,omitempty" jsonschema:"optional service FQCN for service-specific uniqueId resolution; defaults to the most recent session service when omitted"`
	ContextName       string `json:"context_name,omitempty" jsonschema:"optional context override for this resolution"`
	DirectURL         string `json:"direct_url,omitempty" jsonschema:"optional direct target override"`
	RegistryAddress   string `json:"registry_address,omitempty" jsonschema:"optional registry address override"`
	RegistryProtocol  string `json:"registry_protocol,omitempty" jsonschema:"optional registry protocol override"`
	Protocol          string `json:"protocol,omitempty" jsonschema:"optional SOFARPC protocol override"`
	Serialization     string `json:"serialization,omitempty" jsonschema:"optional serialization override"`
	UniqueID          string `json:"unique_id,omitempty" jsonschema:"optional uniqueId override"`
	TimeoutMS         int    `json:"timeout_ms,omitempty" jsonschema:"optional invoke timeout override in milliseconds"`
	ConnectTimeoutMS  int    `json:"connect_timeout_ms,omitempty" jsonschema:"optional connect timeout override in milliseconds"`
	IncludeCandidates bool   `json:"include_candidates,omitempty" jsonschema:"include candidate contexts and layers in the result"`
	IncludeExplain    bool   `json:"include_explain,omitempty" jsonschema:"include human-readable target selection explanation"`
}

type DescribeMethodInput struct {
	SessionID           string   `json:"session_id" jsonschema:"workspace session id created by open_workspace_session"`
	Service             string   `json:"service,omitempty" jsonschema:"optional service FQCN; defaults to the most recent session service when omitted"`
	Method              string   `json:"method" jsonschema:"method name"`
	PreferredParamTypes []string `json:"preferred_param_types,omitempty" jsonschema:"optional parameter types used to disambiguate overloads"`
	Refresh             bool     `json:"refresh,omitempty" jsonschema:"refresh local or legacy schema caches before resolving"`
}

type PlanInvocationInput = InvokeRPCInput

type InvokeRPCInput struct {
	SessionID        string              `json:"session_id,omitempty" jsonschema:"workspace session id created by open_workspace_session; optional when prepared is provided"`
	Prepared         *PreparedInvocation `json:"prepared,omitempty" jsonschema:"optional prepared invocation returned by plan_invocation"`
	Service          string              `json:"service,omitempty" jsonschema:"service FQCN"`
	Method           string              `json:"method,omitempty" jsonschema:"method name"`
	Args             []any               `json:"args,omitempty" jsonschema:"JSON array of invocation arguments"`
	ParamTypes       []string            `json:"param_types,omitempty" jsonschema:"optional parameter types used to disambiguate overloads"`
	PayloadMode      string              `json:"payload_mode,omitempty" jsonschema:"optional payload mode override: raw, generic, or schema"`
	ContextName      string              `json:"context_name,omitempty" jsonschema:"optional context override for this invocation"`
	DirectURL        string              `json:"direct_url,omitempty" jsonschema:"optional direct target override"`
	RegistryAddress  string              `json:"registry_address,omitempty" jsonschema:"optional registry address override"`
	RegistryProtocol string              `json:"registry_protocol,omitempty" jsonschema:"optional registry protocol override"`
	Protocol         string              `json:"protocol,omitempty" jsonschema:"optional SOFARPC protocol override"`
	Serialization    string              `json:"serialization,omitempty" jsonschema:"optional serialization override"`
	UniqueID         string              `json:"unique_id,omitempty" jsonschema:"optional uniqueId override"`
	TimeoutMS        int                 `json:"timeout_ms,omitempty" jsonschema:"optional invoke timeout override in milliseconds"`
	ConnectTimeoutMS int                 `json:"connect_timeout_ms,omitempty" jsonschema:"optional connect timeout override in milliseconds"`
	StubPaths        []string            `json:"stub_paths,omitempty" jsonschema:"optional manual stub paths used only when auto discovery misses"`
	SofaRPCVersion   string              `json:"sofa_rpc_version,omitempty" jsonschema:"optional runtime SOFARPC version override"`
	JavaBin          string              `json:"java_bin,omitempty" jsonschema:"optional java executable override"`
	RuntimeJar       string              `json:"runtime_jar,omitempty" jsonschema:"optional runtime jar override"`
	RefreshContract  bool                `json:"refresh_contract,omitempty" jsonschema:"refresh local contract cache before invoking"`
}

type ListFacadeServicesInput struct {
	SessionID string `json:"session_id" jsonschema:"workspace session id created by open_workspace_session"`
	Filter    string `json:"filter,omitempty" jsonschema:"optional substring filter against service or method names"`
}

type MethodOverload struct {
	ParamTypes          []string `json:"paramTypes,omitempty"`
	ParamTypeSignatures []string `json:"paramTypeSignatures,omitempty"`
	ReturnType          string   `json:"returnType,omitempty"`
}

type DescribeMethodOutput struct {
	Service     string               `json:"service"`
	Method      string               `json:"method"`
	Overloads   []MethodOverload     `json:"overloads"`
	Selected    *MethodOverload      `json:"selected,omitempty"`
	Diagnostics model.DiagnosticInfo `json:"diagnostics,omitempty"`
}

type PreparedRuntimeSpec struct {
	SofaRPCVersion string   `json:"sofaRpcVersion"`
	JavaBin        string   `json:"javaBin"`
	JavaMajor      string   `json:"javaMajor"`
	RuntimeJar     string   `json:"runtimeJar"`
	RuntimeDigest  string   `json:"runtimeDigest,omitempty"`
	DaemonProfile  string   `json:"daemonProfile,omitempty"`
	StubPaths      []string `json:"stubPaths,omitempty"`
	ClasspathHash  string   `json:"classpathHash,omitempty"`
	DaemonKey      string   `json:"daemonKey"`
	MetadataFile   string   `json:"metadataFile,omitempty"`
	StdoutLog      string   `json:"stdoutLog,omitempty"`
	StderrLog      string   `json:"stderrLog,omitempty"`
}

type PreparedInvocation struct {
	Request          model.InvocationRequest `json:"request"`
	Spec             PreparedRuntimeSpec     `json:"spec"`
	ContractSource   string                  `json:"contractSource,omitempty"`
	ContractCacheHit bool                    `json:"contractCacheHit,omitempty"`
	ContractNotes    []string                `json:"contractNotes,omitempty"`
	WorkerClasspath  string                  `json:"workerClasspath,omitempty"`
}

type InvokeRPCResponse struct {
	RequestID   string               `json:"requestId,omitempty"`
	OK          bool                 `json:"ok"`
	Error       *model.RuntimeError  `json:"error,omitempty"`
	Diagnostics model.DiagnosticInfo `json:"diagnostics,omitempty"`
}

type InvokeRPCOutput struct {
	Response InvokeRPCResponse `json:"response"`
	Result   any               `json:"result,omitempty"`
	OKOnly   bool              `json:"okOnly,omitempty"`
}

type PlanInvocationOutput struct {
	Request          model.InvocationRequest `json:"request"`
	Runtime          model.RuntimeSnapshot   `json:"runtime"`
	Selected         *MethodOverload         `json:"selected,omitempty"`
	WrappedSingleArg bool                    `json:"wrappedSingleArg,omitempty"`
	Prepared         PreparedInvocation      `json:"prepared"`
}

type FacadeService struct {
	Service string   `json:"service"`
	File    string   `json:"file,omitempty"`
	Methods []string `json:"methods"`
}

type ListFacadeServicesOutput struct {
	ProjectRoot      string          `json:"projectRoot"`
	FacadeConfigured bool            `json:"facadeConfigured"`
	IndexAvailable   bool            `json:"indexAvailable"`
	SourceRoots      []string        `json:"sourceRoots,omitempty"`
	Services         []FacadeService `json:"services"`
}

type ResumeContextSummary struct {
	Service     string   `json:"service,omitempty"`
	Method      string   `json:"method,omitempty"`
	ContextName string   `json:"contextName,omitempty"`
	ParamTypes  []string `json:"paramTypes,omitempty"`
	HasTarget   bool     `json:"hasTarget"`
	HasDescribe bool     `json:"hasDescribe"`
	HasPlan     bool     `json:"hasPlan"`
	CanInvoke   bool     `json:"canInvoke"`
}

type SuggestedParameter struct {
	Name           string   `json:"name"`
	Type           string   `json:"type,omitempty"`
	RequiredHint   string   `json:"requiredHint,omitempty"`
	RequiredFields []string `json:"requiredFields,omitempty"`
}

type SuggestedDraft struct {
	ArgsSource      string               `json:"argsSource,omitempty"`
	Parameters      []SuggestedParameter `json:"parameters,omitempty"`
	ResponseWarning string               `json:"responseWarning,omitempty"`
	ResponseReason  string               `json:"responseReason,omitempty"`
}

type SuggestedAction struct {
	Tool      string          `json:"tool,omitempty"`
	Reason    string          `json:"reason,omitempty"`
	Arguments map[string]any  `json:"arguments,omitempty"`
	Missing   []string        `json:"missing,omitempty"`
	Draft     *SuggestedDraft `json:"draft,omitempty"`
	Notes     []string        `json:"notes,omitempty"`
}

type ResumeContextOutput struct {
	SessionID       string               `json:"sessionId"`
	Summary         ResumeContextSummary `json:"summary"`
	SuggestedAction SuggestedAction      `json:"suggestedAction"`
	Notes           []string             `json:"notes,omitempty"`
}

type handler struct {
	provider Provider
}

func New(provider Provider) *sdkmcp.Server {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "sofarpc-cli",
		Version: ServerVersion,
	}, &sdkmcp.ServerOptions{
		Instructions: "Use these tools to open a workspace session, resolve a SOFARPC target, inspect method signatures, plan invocations, and invoke RPCs without shelling out.",
	})
	h := handler{provider: provider}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "open_workspace_session",
		Title:       "Open Workspace Session",
		Description: "Open or refresh a workspace session snapshot for the current sofarpc project.",
	}, h.openWorkspaceSession)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "inspect_session",
		Title:       "Inspect Session",
		Description: "Return the current workspace session snapshot, including the last resolved target, method describe, and invocation plan.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, h.inspectSession)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "resume_context",
		Title:       "Resume Context",
		Description: "Summarize the current workspace session into the active service, method, target, plan readiness, and the most useful next MCP tool to call.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, h.resumeContext)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "resolve_target",
		Title:       "Resolve Target",
		Description: "Resolve the effective SOFARPC target for a workspace session, including optional overrides and session-based service defaults.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, h.resolveTarget)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "describe_method",
		Title:       "Describe Method",
		Description: "Describe a service method and return overload candidates from local contract resolution or runtime fallback, using the session's most recent service when omitted.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, h.describeMethod)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "plan_invocation",
		Title:       "Plan Invocation",
		Description: "Resolve an invocation into a typed request and execution plan without sending the RPC.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, h.planInvocation)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "list_facade_services",
		Title:       "List Facade Services",
		Description: "List facade services discovered for the workspace session from cached index data or source scanning.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, h.listFacadeServices)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "invoke_rpc",
		Title:       "Invoke RPC",
		Description: "Invoke a SOFARPC method using either a workspace session or a prepared plan from plan_invocation.",
	}, h.invokeRPC)
	return server
}

func Run(ctx context.Context, provider Provider) error {
	return New(provider).Run(ctx, &sdkmcp.StdioTransport{})
}

func (h handler) openWorkspaceSession(ctx context.Context, _ *sdkmcp.CallToolRequest, input OpenWorkspaceSessionInput) (*sdkmcp.CallToolResult, model.WorkspaceSession, error) {
	session, err := h.provider.SessionService().Open(ctx, appsession.OpenRequest{
		Cwd:          h.provider.WorkingDir(),
		Paths:        h.provider.ConfigPaths(),
		Project:      input.ProjectRoot,
		ManifestPath: input.ManifestPath,
		ContextName:  input.ContextName,
	})
	return nil, session, err
}

func (h handler) inspectSession(ctx context.Context, _ *sdkmcp.CallToolRequest, input InspectSessionInput) (*sdkmcp.CallToolResult, model.WorkspaceSession, error) {
	session, err := h.requireSession(ctx, input.SessionID)
	if err != nil {
		return nil, model.WorkspaceSession{}, err
	}
	return nil, session, nil
}

func (h handler) resumeContext(ctx context.Context, _ *sdkmcp.CallToolRequest, input ResumeContextInput) (*sdkmcp.CallToolResult, ResumeContextOutput, error) {
	session, err := h.requireSession(ctx, input.SessionID)
	if err != nil {
		return nil, ResumeContextOutput{}, err
	}
	service := sessionServiceName(session)
	method := sessionMethodName(session, service)
	paramTypes := sessionParamTypes(session, service, method)
	summary := ResumeContextSummary{
		Service:     service,
		Method:      method,
		ContextName: firstNonEmpty(sessionContextName(session, service), session.ActiveContext),
		ParamTypes:  append([]string{}, paramTypes...),
		HasTarget:   sessionTarget(session, service) != nil,
		HasDescribe: sessionDescribe(session, service, method) != nil,
		HasPlan:     sessionPlan(session, service, method) != nil,
	}
	summary.CanInvoke = summary.HasPlan
	return nil, ResumeContextOutput{
		SessionID:       session.ID,
		Summary:         summary,
		SuggestedAction: h.suggestedAction(ctx, session, summary),
		Notes:           append([]string{}, session.Notes...),
	}, nil
}

func (h handler) resolveTarget(ctx context.Context, _ *sdkmcp.CallToolRequest, input ResolveTargetInput) (*sdkmcp.CallToolResult, apptarget.Report, error) {
	session, err := h.requireSession(ctx, input.SessionID)
	if err != nil {
		return nil, apptarget.Report{}, err
	}
	sessionService := h.provider.SessionService()
	service := firstNonEmpty(input.Service, sessionServiceName(session))
	contextName := firstNonEmpty(input.ContextName, sessionContextName(session, service), session.ActiveContext)
	report, err := h.provider.TargetService().Execute(apptarget.Request{
		Cwd:     session.ProjectRoot,
		Paths:   h.provider.ConfigPaths(),
		Project: session.ProjectRoot,
		Input: appshared.InvocationInputs{
			ManifestPath:     session.ManifestPath,
			ContextName:      contextName,
			Service:          service,
			DirectURL:        input.DirectURL,
			RegistryAddress:  input.RegistryAddress,
			RegistryProtocol: input.RegistryProtocol,
			Protocol:         input.Protocol,
			Serialization:    input.Serialization,
			UniqueID:         input.UniqueID,
			TimeoutMS:        input.TimeoutMS,
			ConnectTimeoutMS: input.ConnectTimeoutMS,
		},
		ShowAll: input.IncludeCandidates,
		Explain: input.IncludeExplain,
	})
	if err == nil {
		if _, recordErr := sessionService.RecordTarget(ctx, appsession.RecordTargetRequest{
			ID:           session.ID,
			Service:      service,
			ContextName:  contextName,
			Target:       report.Target,
			Reachability: report.Reachability,
		}); recordErr != nil {
			return nil, apptarget.Report{}, recordErr
		}
	}
	return nil, report, err
}

func (h handler) describeMethod(ctx context.Context, _ *sdkmcp.CallToolRequest, input DescribeMethodInput) (*sdkmcp.CallToolResult, DescribeMethodOutput, error) {
	session, err := h.requireSession(ctx, input.SessionID)
	if err != nil {
		return nil, DescribeMethodOutput{}, err
	}
	service := firstNonEmpty(input.Service, sessionServiceName(session))
	if strings.TrimSpace(service) == "" {
		return nil, DescribeMethodOutput{}, fmt.Errorf("describe_method requires a service or a prior session service")
	}
	result, err := h.provider.DescribeService().Execute(ctx, appdescribe.Request{
		Cwd:          session.ProjectRoot,
		ManifestPath: session.ManifestPath,
		Service:      service,
		Refresh:      input.Refresh,
	})
	if err != nil {
		return nil, DescribeMethodOutput{}, err
	}
	overloads := matchingMethodOverloads(*result.Schema, input.Method)
	if len(overloads) == 0 {
		return nil, DescribeMethodOutput{}, fmt.Errorf("method %s not found on %s", input.Method, service)
	}
	selected := selectMethodOverload(overloads, input.PreferredParamTypes)
	output := DescribeMethodOutput{
		Service:     service,
		Method:      input.Method,
		Overloads:   overloads,
		Selected:    selected,
		Diagnostics: result.Diagnostics,
	}
	if _, recordErr := h.provider.SessionService().RecordDescribe(ctx, appsession.RecordDescribeRequest{
		ID:          session.ID,
		Service:     service,
		Method:      input.Method,
		Overloads:   workspaceMethodOverloads(overloads),
		Selected:    workspaceSelectedOverload(selected),
		Diagnostics: result.Diagnostics,
	}); recordErr != nil {
		return nil, DescribeMethodOutput{}, recordErr
	}
	return nil, output, nil
}

func (h handler) planInvocation(ctx context.Context, _ *sdkmcp.CallToolRequest, input PlanInvocationInput) (*sdkmcp.CallToolResult, PlanInvocationOutput, error) {
	req, err := h.buildInvocationRequest(ctx, InvokeRPCInput(input))
	if err != nil {
		return nil, PlanInvocationOutput{}, err
	}
	plan, err := h.provider.InvokeService().Plan(ctx, req)
	if err != nil {
		return nil, PlanInvocationOutput{}, err
	}
	var selected *MethodOverload
	if plan.Method != nil {
		overload := methodSchemaOverload(*plan.Method)
		selected = &overload
	}
	output := PlanInvocationOutput{
		Request: plan.Request,
		Runtime: model.RuntimeSnapshot{
			ContractSource:   plan.ContractSource,
			ContractCacheHit: plan.ContractCacheHit,
			ContractNotes:    append([]string{}, plan.ContractNotes...),
			WorkerClasspath:  plan.WorkerClasspath,
			RuntimeJar:       plan.Spec.RuntimeJar,
			SofaRPCVersion:   plan.Spec.SofaRPCVersion,
			JavaBin:          plan.Spec.JavaBin,
			JavaMajor:        plan.Spec.JavaMajor,
			DaemonKey:        plan.Spec.DaemonKey,
		},
		Selected:         selected,
		WrappedSingleArg: plan.WrappedSingleArg,
		Prepared:         preparedFromPlan(plan),
	}
	if input.SessionID != "" {
		if _, recordErr := h.recordPlan(ctx, input.SessionID, plan, output.Runtime); recordErr != nil {
			return nil, PlanInvocationOutput{}, recordErr
		}
	}
	return nil, output, nil
}

func (h handler) listFacadeServices(ctx context.Context, _ *sdkmcp.CallToolRequest, input ListFacadeServicesInput) (*sdkmcp.CallToolResult, ListFacadeServicesOutput, error) {
	session, err := h.requireSession(ctx, input.SessionID)
	if err != nil {
		return nil, ListFacadeServicesOutput{}, err
	}
	result, err := h.provider.FacadeService().Services(appfacade.ServicesRequest{
		Cwd:     session.ProjectRoot,
		Project: session.ProjectRoot,
		Filter:  input.Filter,
	})
	if err != nil {
		return nil, ListFacadeServicesOutput{}, err
	}
	services := make([]FacadeService, 0, len(result.Summary.Services))
	for _, service := range result.Summary.Services {
		services = append(services, FacadeService{
			Service: service.Service,
			File:    service.File,
			Methods: append([]string{}, service.Methods...),
		})
	}
	return nil, ListFacadeServicesOutput{
		ProjectRoot:      result.ProjectRoot,
		FacadeConfigured: session.FacadeConfigured,
		IndexAvailable:   session.Capabilities.FacadeIndex,
		SourceRoots:      append([]string{}, result.Summary.SourceRoots...),
		Services:         services,
	}, nil
}

func (h handler) invokeRPC(ctx context.Context, _ *sdkmcp.CallToolRequest, input InvokeRPCInput) (*sdkmcp.CallToolResult, InvokeRPCOutput, error) {
	var (
		plan   appinvoke.Plan
		result appinvoke.Result
		err    error
	)
	if input.Prepared != nil {
		plan = planFromPrepared(*input.Prepared)
	} else if wantsSessionPlan(input) {
		session, sessionErr := h.requireSession(ctx, input.SessionID)
		if sessionErr != nil {
			return nil, InvokeRPCOutput{}, sessionErr
		}
		if hasSessionPlanOverrides(input) {
			return nil, InvokeRPCOutput{}, fmt.Errorf("invoke_rpc cannot apply ad-hoc overrides when reusing the last session plan; call plan_invocation or pass service and method explicitly")
		}
		var ok bool
		plan, ok = planFromSession(session)
		if !ok {
			return nil, InvokeRPCOutput{}, fmt.Errorf("workspace session %q has no last plan; call plan_invocation first", session.ID)
		}
	} else {
		req, reqErr := h.buildInvocationRequest(ctx, input)
		if reqErr != nil {
			return nil, InvokeRPCOutput{}, reqErr
		}
		plan, err = h.provider.InvokeService().Plan(ctx, req)
		if err != nil {
			return nil, InvokeRPCOutput{}, err
		}
	}
	if input.SessionID != "" {
		runtimeSnapshot := model.RuntimeSnapshot{
			ContractSource:   plan.ContractSource,
			ContractCacheHit: plan.ContractCacheHit,
			ContractNotes:    append([]string{}, plan.ContractNotes...),
			WorkerClasspath:  plan.WorkerClasspath,
			RuntimeJar:       plan.Spec.RuntimeJar,
			SofaRPCVersion:   plan.Spec.SofaRPCVersion,
			JavaBin:          plan.Spec.JavaBin,
			JavaMajor:        plan.Spec.JavaMajor,
			DaemonKey:        plan.Spec.DaemonKey,
		}
		if _, recordErr := h.recordPlan(ctx, input.SessionID, plan, runtimeSnapshot); recordErr != nil {
			return nil, InvokeRPCOutput{}, recordErr
		}
	}
	result, err = h.provider.InvokeService().ExecutePlan(ctx, plan)
	if err != nil {
		return nil, InvokeRPCOutput{}, err
	}
	output := InvokeRPCOutput{
		Response: InvokeRPCResponse{
			RequestID:   result.Response.RequestID,
			OK:          result.Response.OK,
			Error:       result.Response.Error,
			Diagnostics: result.Response.Diagnostics,
		},
		OKOnly: result.OKOnly,
	}
	if !result.OKOnly && result.Pretty != nil {
		output.Result = result.Pretty
	}
	return nil, output, nil
}

func (h handler) recordPlan(ctx context.Context, sessionID string, plan appinvoke.Plan, runtimeSnapshot model.RuntimeSnapshot) (model.WorkspaceSession, error) {
	return h.provider.SessionService().RecordPlan(ctx, appsession.RecordPlanRequest{
		ID:               sessionID,
		Service:          plan.Request.Service,
		Method:           plan.Request.Method,
		Request:          plan.Request,
		Spec:             runtimePlanSpec(plan.Spec),
		Runtime:          runtimeSnapshot,
		WrappedSingleArg: plan.WrappedSingleArg,
	})
}

func (h handler) buildInvocationRequest(ctx context.Context, input InvokeRPCInput) (appinvoke.Request, error) {
	session, err := h.requireSession(ctx, input.SessionID)
	if err != nil {
		return appinvoke.Request{}, err
	}
	argsJSON, err := marshalToolArgs(input.Args)
	if err != nil {
		return appinvoke.Request{}, err
	}
	service := firstNonEmpty(input.Service, sessionServiceName(session))
	method := firstNonEmpty(input.Method, sessionMethodName(session, service))
	paramTypes := append([]string{}, input.ParamTypes...)
	if len(paramTypes) == 0 {
		paramTypes = sessionParamTypes(session, service, method)
	}
	lastTarget := sessionTarget(session, service)
	lastPlan := sessionPlan(session, service, method)
	contextName := firstNonEmpty(input.ContextName, sessionContextName(session, service), session.ActiveContext)
	timeoutMS := input.TimeoutMS
	if timeoutMS <= 0 && lastTarget != nil {
		timeoutMS = lastTarget.Target.TimeoutMS
	}
	connectTimeoutMS := input.ConnectTimeoutMS
	if connectTimeoutMS <= 0 && lastTarget != nil {
		connectTimeoutMS = lastTarget.Target.ConnectTimeoutMS
	}
	stubPaths := append([]string{}, input.StubPaths...)
	if len(stubPaths) == 0 && lastPlan != nil {
		stubPaths = append([]string{}, lastPlan.Spec.StubPaths...)
	}
	return appinvoke.Request{
		Input: appshared.InvocationInputs{
			ManifestPath:     session.ManifestPath,
			ContextName:      contextName,
			Service:          service,
			Method:           method,
			TypesCSV:         strings.Join(paramTypes, ","),
			ArgsJSON:         argsJSON,
			PayloadMode:      input.PayloadMode,
			DirectURL:        firstNonEmpty(input.DirectURL, sessionTargetField(lastTarget, func(target model.TargetConfig) string { return target.DirectURL })),
			RegistryAddress:  firstNonEmpty(input.RegistryAddress, sessionTargetField(lastTarget, func(target model.TargetConfig) string { return target.RegistryAddress })),
			RegistryProtocol: firstNonEmpty(input.RegistryProtocol, sessionTargetField(lastTarget, func(target model.TargetConfig) string { return target.RegistryProtocol })),
			Protocol:         firstNonEmpty(input.Protocol, sessionTargetField(lastTarget, func(target model.TargetConfig) string { return target.Protocol })),
			Serialization:    firstNonEmpty(input.Serialization, sessionTargetField(lastTarget, func(target model.TargetConfig) string { return target.Serialization })),
			UniqueID:         firstNonEmpty(input.UniqueID, sessionTargetField(lastTarget, func(target model.TargetConfig) string { return target.UniqueID })),
			TimeoutMS:        timeoutMS,
			ConnectTimeoutMS: connectTimeoutMS,
			StubPathCSV:      strings.Join(stubPaths, ","),
			SofaRPCVersion:   firstNonEmpty(input.SofaRPCVersion, sessionPlanField(lastPlan, func(plan *model.WorkspaceInvocationPlan) string { return plan.Spec.SofaRPCVersion }), session.SofaRPCVersion),
			JavaBin:          firstNonEmpty(input.JavaBin, sessionPlanField(lastPlan, func(plan *model.WorkspaceInvocationPlan) string { return plan.Spec.JavaBin })),
			RuntimeJar:       firstNonEmpty(input.RuntimeJar, sessionPlanField(lastPlan, func(plan *model.WorkspaceInvocationPlan) string { return plan.Spec.RuntimeJar })),
			RefreshContract:  input.RefreshContract,
		},
	}, nil
}

func (h handler) requireSession(ctx context.Context, id string) (model.WorkspaceSession, error) {
	session, ok, err := h.provider.SessionService().Get(ctx, id)
	if err != nil {
		return model.WorkspaceSession{}, err
	}
	if !ok {
		return model.WorkspaceSession{}, fmt.Errorf("workspace session %q not found; call open_workspace_session first", id)
	}
	return session, nil
}

func preparedFromPlan(plan appinvoke.Plan) PreparedInvocation {
	return PreparedInvocation{
		Request: plan.Request,
		Spec: PreparedRuntimeSpec{
			SofaRPCVersion: plan.Spec.SofaRPCVersion,
			JavaBin:        plan.Spec.JavaBin,
			JavaMajor:      plan.Spec.JavaMajor,
			RuntimeJar:     plan.Spec.RuntimeJar,
			RuntimeDigest:  plan.Spec.RuntimeDigest,
			DaemonProfile:  plan.Spec.DaemonProfile,
			StubPaths:      append([]string{}, plan.Spec.StubPaths...),
			ClasspathHash:  plan.Spec.ClasspathHash,
			DaemonKey:      plan.Spec.DaemonKey,
			MetadataFile:   plan.Spec.MetadataFile,
			StdoutLog:      plan.Spec.StdoutLog,
			StderrLog:      plan.Spec.StderrLog,
		},
		ContractSource:   plan.ContractSource,
		ContractCacheHit: plan.ContractCacheHit,
		ContractNotes:    append([]string{}, plan.ContractNotes...),
		WorkerClasspath:  plan.WorkerClasspath,
	}
}

func planFromSession(session model.WorkspaceSession) (appinvoke.Plan, bool) {
	if session.LastPlan == nil {
		return appinvoke.Plan{}, false
	}
	lastPlan := session.LastPlan
	return appinvoke.Plan{
		Request: lastPlan.Request,
		Spec: runtime.Spec{
			SofaRPCVersion: lastPlan.Spec.SofaRPCVersion,
			JavaBin:        lastPlan.Spec.JavaBin,
			JavaMajor:      lastPlan.Spec.JavaMajor,
			RuntimeJar:     lastPlan.Spec.RuntimeJar,
			RuntimeDigest:  lastPlan.Spec.RuntimeDigest,
			DaemonProfile:  lastPlan.Spec.DaemonProfile,
			StubPaths:      append([]string{}, lastPlan.Spec.StubPaths...),
			ClasspathHash:  lastPlan.Spec.ClasspathHash,
			DaemonKey:      lastPlan.Spec.DaemonKey,
			MetadataFile:   lastPlan.Spec.MetadataFile,
			StdoutLog:      lastPlan.Spec.StdoutLog,
			StderrLog:      lastPlan.Spec.StderrLog,
		},
		ContractSource:   lastPlan.Runtime.ContractSource,
		ContractCacheHit: lastPlan.Runtime.ContractCacheHit,
		ContractNotes:    append([]string{}, lastPlan.Runtime.ContractNotes...),
		WorkerClasspath:  lastPlan.Runtime.WorkerClasspath,
		WrappedSingleArg: lastPlan.WrappedSingleArg,
	}, true
}

func planFromPrepared(prepared PreparedInvocation) appinvoke.Plan {
	return appinvoke.Plan{
		Request: prepared.Request,
		Spec: runtime.Spec{
			SofaRPCVersion: prepared.Spec.SofaRPCVersion,
			JavaBin:        prepared.Spec.JavaBin,
			JavaMajor:      prepared.Spec.JavaMajor,
			RuntimeJar:     prepared.Spec.RuntimeJar,
			RuntimeDigest:  prepared.Spec.RuntimeDigest,
			DaemonProfile:  prepared.Spec.DaemonProfile,
			StubPaths:      append([]string{}, prepared.Spec.StubPaths...),
			ClasspathHash:  prepared.Spec.ClasspathHash,
			DaemonKey:      prepared.Spec.DaemonKey,
			MetadataFile:   prepared.Spec.MetadataFile,
			StdoutLog:      prepared.Spec.StdoutLog,
			StderrLog:      prepared.Spec.StderrLog,
		},
		ContractSource:   prepared.ContractSource,
		ContractCacheHit: prepared.ContractCacheHit,
		ContractNotes:    append([]string{}, prepared.ContractNotes...),
		WorkerClasspath:  prepared.WorkerClasspath,
	}
}

func runtimePlanSpec(spec runtime.Spec) model.WorkspaceRuntimePlanSpec {
	return model.WorkspaceRuntimePlanSpec{
		SofaRPCVersion: spec.SofaRPCVersion,
		JavaBin:        spec.JavaBin,
		JavaMajor:      spec.JavaMajor,
		RuntimeJar:     spec.RuntimeJar,
		RuntimeDigest:  spec.RuntimeDigest,
		DaemonProfile:  spec.DaemonProfile,
		StubPaths:      append([]string{}, spec.StubPaths...),
		ClasspathHash:  spec.ClasspathHash,
		DaemonKey:      spec.DaemonKey,
		MetadataFile:   spec.MetadataFile,
		StdoutLog:      spec.StdoutLog,
		StderrLog:      spec.StderrLog,
	}
}

func marshalToolArgs(args []any) (string, error) {
	if args == nil {
		return "[]", nil
	}
	body, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func methodSchemaOverload(candidate model.MethodSchema) MethodOverload {
	return MethodOverload{
		ParamTypes:          append([]string{}, candidate.ParamTypes...),
		ParamTypeSignatures: append([]string{}, candidate.ParamTypeSignatures...),
		ReturnType:          candidate.ReturnType,
	}
}

func matchingMethodOverloads(schema model.ServiceSchema, method string) []MethodOverload {
	overloads := make([]MethodOverload, 0, len(schema.Methods))
	for _, candidate := range schema.Methods {
		if candidate.Name != method {
			continue
		}
		overloads = append(overloads, methodSchemaOverload(candidate))
	}
	sort.Slice(overloads, func(i, j int) bool {
		left := strings.Join(overloads[i].ParamTypes, ",")
		right := strings.Join(overloads[j].ParamTypes, ",")
		if left == right {
			return overloads[i].ReturnType < overloads[j].ReturnType
		}
		return left < right
	})
	return overloads
}

func selectMethodOverload(overloads []MethodOverload, preferred []string) *MethodOverload {
	if len(overloads) == 1 {
		selected := overloads[0]
		return &selected
	}
	if len(preferred) == 0 {
		return nil
	}
	for _, overload := range overloads {
		if sameParamTypes(overload.ParamTypes, preferred) {
			selected := overload
			return &selected
		}
	}
	return nil
}

func sameParamTypes(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func sessionParamTypes(session model.WorkspaceSession, service, method string) []string {
	if session.LastDescribe != nil &&
		session.LastDescribe.Service == service &&
		session.LastDescribe.Method == method &&
		session.LastDescribe.Selected != nil &&
		len(session.LastDescribe.Selected.ParamTypes) > 0 {
		return append([]string{}, session.LastDescribe.Selected.ParamTypes...)
	}
	if session.LastPlan != nil &&
		session.LastPlan.Service == service &&
		session.LastPlan.Method == method &&
		len(session.LastPlan.Request.ParamTypes) > 0 {
		return append([]string{}, session.LastPlan.Request.ParamTypes...)
	}
	return nil
}

func sessionDescribe(session model.WorkspaceSession, service, method string) *model.WorkspaceMethodDescription {
	if session.LastDescribe == nil {
		return nil
	}
	if service != "" && session.LastDescribe.Service != service {
		return nil
	}
	if method != "" && session.LastDescribe.Method != method {
		return nil
	}
	return session.LastDescribe
}

func sessionServiceName(session model.WorkspaceSession) string {
	if session.LastDescribe != nil && strings.TrimSpace(session.LastDescribe.Service) != "" {
		return session.LastDescribe.Service
	}
	if session.LastPlan != nil && strings.TrimSpace(session.LastPlan.Service) != "" {
		return session.LastPlan.Service
	}
	if session.LastTarget != nil && strings.TrimSpace(session.LastTarget.Service) != "" {
		return session.LastTarget.Service
	}
	return ""
}

func sessionMethodName(session model.WorkspaceSession, service string) string {
	if session.LastDescribe != nil && session.LastDescribe.Service == service && strings.TrimSpace(session.LastDescribe.Method) != "" {
		return session.LastDescribe.Method
	}
	if session.LastPlan != nil && session.LastPlan.Service == service && strings.TrimSpace(session.LastPlan.Method) != "" {
		return session.LastPlan.Method
	}
	return ""
}

func sessionContextName(session model.WorkspaceSession, service string) string {
	if session.LastTarget != nil && session.LastTarget.Service == service {
		return session.LastTarget.ContextName
	}
	return ""
}

func sessionTarget(session model.WorkspaceSession, service string) *model.WorkspaceResolvedTarget {
	if session.LastTarget == nil {
		return nil
	}
	if service == "" || session.LastTarget.Service == service {
		return session.LastTarget
	}
	return nil
}

func sessionPlan(session model.WorkspaceSession, service, method string) *model.WorkspaceInvocationPlan {
	if session.LastPlan == nil {
		return nil
	}
	if service != "" && session.LastPlan.Service != service {
		return nil
	}
	if method != "" && session.LastPlan.Method != method {
		return nil
	}
	return session.LastPlan
}

func sessionTargetField(target *model.WorkspaceResolvedTarget, pick func(model.TargetConfig) string) string {
	if target == nil {
		return ""
	}
	return pick(target.Target)
}

func sessionPlanField(plan *model.WorkspaceInvocationPlan, pick func(*model.WorkspaceInvocationPlan) string) string {
	if plan == nil {
		return ""
	}
	return pick(plan)
}

func wantsSessionPlan(input InvokeRPCInput) bool {
	return input.Prepared == nil &&
		strings.TrimSpace(input.Service) == "" &&
		strings.TrimSpace(input.Method) == ""
}

func hasSessionPlanOverrides(input InvokeRPCInput) bool {
	return len(input.Args) > 0 ||
		len(input.ParamTypes) > 0 ||
		strings.TrimSpace(input.PayloadMode) != "" ||
		strings.TrimSpace(input.ContextName) != "" ||
		strings.TrimSpace(input.DirectURL) != "" ||
		strings.TrimSpace(input.RegistryAddress) != "" ||
		strings.TrimSpace(input.RegistryProtocol) != "" ||
		strings.TrimSpace(input.Protocol) != "" ||
		strings.TrimSpace(input.Serialization) != "" ||
		strings.TrimSpace(input.UniqueID) != "" ||
		input.TimeoutMS > 0 ||
		input.ConnectTimeoutMS > 0 ||
		len(input.StubPaths) > 0 ||
		strings.TrimSpace(input.SofaRPCVersion) != "" ||
		strings.TrimSpace(input.JavaBin) != "" ||
		strings.TrimSpace(input.RuntimeJar) != "" ||
		input.RefreshContract
}

func (h handler) suggestedAction(ctx context.Context, session model.WorkspaceSession, summary ResumeContextSummary) SuggestedAction {
	if summary.HasPlan {
		return SuggestedAction{
			Tool:      "invoke_rpc",
			Reason:    "workspace session already has a prepared invocation plan",
			Arguments: map[string]any{"session_id": session.ID},
		}
	}
	if summary.Service == "" {
		action := SuggestedAction{
			Tool:      "list_facade_services",
			Reason:    "workspace session has no active service yet",
			Arguments: map[string]any{"session_id": session.ID},
		}
		if !session.FacadeConfigured || !session.Capabilities.FacadeIndex {
			action.Missing = []string{"service"}
			action.Reason = "workspace session has no active service and no cached facade index to browse from"
		}
		return action
	}
	if !summary.HasTarget {
		args := map[string]any{
			"session_id": session.ID,
			"service":    summary.Service,
		}
		if summary.ContextName != "" {
			args["context_name"] = summary.ContextName
		}
		return SuggestedAction{
			Tool:      "resolve_target",
			Reason:    "workspace session has an active service but no resolved target snapshot yet",
			Arguments: args,
		}
	}
	if summary.Method == "" {
		return SuggestedAction{
			Tool:      "describe_method",
			Reason:    "workspace session has a service and target but no active method yet",
			Arguments: map[string]any{"session_id": session.ID, "service": summary.Service},
			Missing:   []string{"method"},
		}
	}
	arguments, draft, notes := h.planInvocationDraft(ctx, session, summary)
	return SuggestedAction{
		Tool:      "plan_invocation",
		Reason:    "workspace session has service and method context but no prepared invocation plan yet",
		Arguments: arguments,
		Draft:     draft,
		Notes:     notes,
	}
}

func (h handler) planInvocationDraft(ctx context.Context, session model.WorkspaceSession, summary ResumeContextSummary) (map[string]any, *SuggestedDraft, []string) {
	draft := &SuggestedDraft{
		ArgsSource: "param_types",
		Parameters: suggestedParametersFromParamTypes(summary.ParamTypes),
	}
	args := map[string]any{
		"session_id": session.ID,
		"service":    summary.Service,
		"method":     summary.Method,
		"args":       draftArgsFromParamTypes(summary.ParamTypes),
	}
	if summary.ContextName != "" {
		args["context_name"] = summary.ContextName
	}
	if len(summary.ParamTypes) > 0 {
		args["param_types"] = append([]string{}, summary.ParamTypes...)
	}
	args["payload_mode"] = sessionPayloadMode(session, summary.Service, summary.Method)

	schema, err := h.resumeMethodSchema(ctx, session, summary)
	if err != nil {
		return args, draft, []string{"args use placeholder values inferred from param types; replace them before invoking"}
	}
	if len(schema.Method.ParamsSkeleton) > 0 {
		args["args"] = cloneInterfaces(schema.Method.ParamsSkeleton)
	}
	draft.ArgsSource = "facade_schema"
	draft.Parameters = suggestedParametersFromSchema(schema.Method.ParamsFieldInfo)
	draft.ResponseWarning = schema.Method.ResponseWarning
	draft.ResponseReason = schema.Method.ResponseWarningReason
	return args, draft, nil
}

func (h handler) resumeMethodSchema(ctx context.Context, session model.WorkspaceSession, summary ResumeContextSummary) (facadeschema.MethodSchemaEnvelope, error) {
	result, err := h.provider.FacadeService().Schema(appfacade.SchemaRequest{
		Cwd:        session.ProjectRoot,
		Project:    session.ProjectRoot,
		Service:    summary.Service,
		Method:     summary.Method,
		ParamTypes: summary.ParamTypes,
	})
	if err != nil {
		return facadeschema.MethodSchemaEnvelope{}, err
	}
	return result.Schema, nil
}

func requiredFieldNames(fields []facadeschema.FieldSchema) []string {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.Required {
			names = append(names, field.Name)
		}
	}
	return names
}

func suggestedParametersFromSchema(params []facadeschema.ParameterSchema) []SuggestedParameter {
	items := make([]SuggestedParameter, 0, len(params))
	for _, param := range params {
		items = append(items, SuggestedParameter{
			Name:           param.Name,
			Type:           param.Type,
			RequiredHint:   param.RequiredHint,
			RequiredFields: requiredFieldNames(param.Fields),
		})
	}
	return items
}

func suggestedParametersFromParamTypes(paramTypes []string) []SuggestedParameter {
	items := make([]SuggestedParameter, 0, len(paramTypes))
	for idx, paramType := range paramTypes {
		items = append(items, SuggestedParameter{
			Name: fmt.Sprintf("arg%d", idx),
			Type: paramType,
		})
	}
	return items
}

func cloneInterfaces(items []interface{}) []interface{} {
	cloned := make([]interface{}, 0, len(items))
	for _, item := range items {
		cloned = append(cloned, cloneInterface(item))
	}
	return cloned
}

func cloneInterface(value interface{}) interface{} {
	switch typed := value.(type) {
	case []interface{}:
		return cloneInterfaces(typed)
	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			out[key] = cloneInterface(item)
		}
		return out
	default:
		return typed
	}
}

func draftArgsFromParamTypes(paramTypes []string) []interface{} {
	args := make([]interface{}, 0, len(paramTypes))
	for _, paramType := range paramTypes {
		args = append(args, draftValueForParamType(paramType))
	}
	return args
}

func draftValueForParamType(paramType string) interface{} {
	typeName := strings.TrimSpace(strings.Split(strings.Split(paramType, "<")[0], "[")[0])
	switch {
	case strings.HasSuffix(strings.TrimSpace(paramType), "[]"):
		return []interface{}{}
	case strings.Contains(paramType, "List<"),
		strings.Contains(paramType, "Set<"),
		strings.Contains(paramType, "Collection<"),
		typeName == "java.util.List",
		typeName == "java.util.Set",
		typeName == "java.util.Collection",
		typeName == "List",
		typeName == "Set",
		typeName == "Collection":
		return []interface{}{}
	case strings.Contains(paramType, "Map<"),
		typeName == "java.util.Map",
		typeName == "Map":
		return map[string]interface{}{}
	case strings.HasSuffix(typeName, "String"),
		typeName == "java.lang.String",
		typeName == "String",
		typeName == "java.util.UUID",
		typeName == "UUID":
		return ""
	case typeName == "boolean",
		typeName == "java.lang.Boolean",
		typeName == "Boolean":
		return false
	case strings.Contains(typeName, "BigDecimal"),
		strings.Contains(typeName, "BigInteger"):
		return "0"
	case strings.Contains(typeName, "Date"),
		strings.Contains(typeName, "Time"),
		strings.Contains(typeName, "Instant"):
		return nil
	case typeName == "byte",
		typeName == "short",
		typeName == "int",
		typeName == "long",
		typeName == "float",
		typeName == "double",
		typeName == "Byte",
		typeName == "Short",
		typeName == "Integer",
		typeName == "Long",
		typeName == "Float",
		typeName == "Double",
		typeName == "Number",
		typeName == "java.lang.Byte",
		typeName == "java.lang.Short",
		typeName == "java.lang.Integer",
		typeName == "java.lang.Long",
		typeName == "java.lang.Float",
		typeName == "java.lang.Double",
		typeName == "java.lang.Number":
		return 0
	default:
		return map[string]interface{}{}
	}
}

func sessionPayloadMode(session model.WorkspaceSession, service, method string) string {
	if plan := sessionPlan(session, service, method); plan != nil && strings.TrimSpace(plan.Request.PayloadMode) != "" {
		return plan.Request.PayloadMode
	}
	return model.PayloadRaw
}

func workspaceMethodOverloads(overloads []MethodOverload) []model.WorkspaceMethodOverload {
	items := make([]model.WorkspaceMethodOverload, 0, len(overloads))
	for _, overload := range overloads {
		items = append(items, model.WorkspaceMethodOverload{
			ParamTypes:          append([]string{}, overload.ParamTypes...),
			ParamTypeSignatures: append([]string{}, overload.ParamTypeSignatures...),
			ReturnType:          overload.ReturnType,
		})
	}
	return items
}

func workspaceSelectedOverload(selected *MethodOverload) *model.WorkspaceMethodOverload {
	if selected == nil {
		return nil
	}
	item := model.WorkspaceMethodOverload{
		ParamTypes:          append([]string{}, selected.ParamTypes...),
		ParamTypeSignatures: append([]string{}, selected.ParamTypeSignatures...),
		ReturnType:          selected.ReturnType,
	}
	return &item
}
