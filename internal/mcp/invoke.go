package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	envAllowInvoke         = invoke.EnvAllowInvoke
	envAllowedServices     = invoke.EnvAllowedServices
	envAllowTargetOverride = invoke.EnvAllowTargetOverride
	envAllowedTargetHosts  = invoke.EnvAllowedTargetHosts
)

// InvokeOutput is the structured payload for sofarpc_invoke. Ok=true
// means the invocation (dry-run or real) produced a usable outcome —
// either a Plan to inspect or a Result from the direct transport.
type InvokeOutput struct {
	Ok          bool           `json:"ok"`
	Plan        *invoke.Plan   `json:"plan,omitempty"`
	Result      any            `json:"result,omitempty"`
	Diagnostics map[string]any `json:"diagnostics,omitempty"`
	Error       *errcode.Error `json:"error,omitempty"`
}

func registerInvoke(server *sdkmcp.Server, opts Options, holder *contractHolder) {
	sources := opts.TargetSources
	sessions := opts.Sessions
	inputSchema, err := jsonschema.For[InvokeInput](nil)
	if err != nil {
		panic(fmt.Sprintf("infer invoke input schema: %v", err))
	}
	server.AddTool(&sdkmcp.Tool{
		Name:        "sofarpc_invoke",
		Description: "Plan and execute a SOFARPC generic invocation. args accepts inline JSON or an @<path> JSON file rooted at SOFARPC_ARGS_FILE_ROOT or the project root. Single-parameter methods may pass the value directly; multi-parameter methods must pass a JSON array. dryRun=true returns the plan without executing the request. Real invokes require SOFARPC_ALLOW_INVOKE=true.",
		InputSchema: inputSchema,
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		store := holder.Get()
		decoded, args, err := decodeInvokeInput(req)
		if err != nil {
			return invokeToolResult(InvokeOutput{Error: asErrcodeError(err)}, errorText("invoke failed", err), true), nil
		}
		toolSources := sourcesForSession(sources, sessions, decoded.SessionID)
		args, err = normalizeArgs(decoded.Service, decoded.Method, args, toolSources.ProjectRoot)
		if err != nil {
			return invokeToolResult(InvokeOutput{Error: asErrcodeError(err)}, errorText("invoke failed", err), true), nil
		}

		plan, err := invoke.BuildPlan(invoke.Input{
			Service:       decoded.Service,
			Method:        decoded.Method,
			ParamTypes:    decoded.Types,
			Args:          args,
			Version:       decoded.Version,
			TargetAppName: decoded.TargetAppName,
			Target: target.Input{
				Service:          decoded.Service,
				DirectURL:        decoded.DirectURL,
				RegistryAddress:  decoded.RegistryAddress,
				RegistryProtocol: decoded.RegistryProtocol,
				TimeoutMS:        decoded.TimeoutMS,
			},
		}, store, toolSources)
		if err != nil {
			out := InvokeOutput{Error: asErrcodeError(err)}
			return invokeToolResult(out, errorText("invoke failed", err), true), nil
		}

		capture := capturePlanForSession(sessions, decoded.SessionID, plan)

		if decoded.DryRun {
			out := InvokeOutput{Ok: true, Plan: &plan, Diagnostics: diagnosticsWithCapture(nil, capture)}
			return invokeToolResult(out, summarizeInvokePlan(plan, true), false), nil
		}

		if err := validateExecutionPolicy(plan, "invoke", toolSources); err != nil {
			out := InvokeOutput{Plan: &plan, Diagnostics: diagnosticsWithCapture(targetConfigDiagnostics(toolSources), capture), Error: asErrcodeError(err)}
			return invokeToolResult(out, errorText("invoke rejected", err), true), nil
		}

		outcome, execErr := invoke.Execute(ctx, plan, "invoke")
		if execErr != nil {
			out := InvokeOutput{Plan: &plan, Diagnostics: diagnosticsWithCapture(outcome.Diagnostics, capture), Error: asErrcodeError(execErr)}
			return invokeToolResult(out, errorText("invoke failed", execErr), true), nil
		}
		out := InvokeOutput{
			Ok:          true,
			Plan:        &plan,
			Result:      outcome.Result,
			Diagnostics: diagnosticsWithCapture(outcome.Diagnostics, capture),
		}
		return invokeToolResult(out, summarizeInvokePlan(plan, false), false), nil
	})
}

type rawInvokeInput struct {
	Service          string          `json:"service,omitempty"`
	Method           string          `json:"method,omitempty"`
	Types            []string        `json:"types,omitempty"`
	Args             json.RawMessage `json:"args,omitempty"`
	Version          string          `json:"version,omitempty"`
	TargetAppName    string          `json:"targetAppName,omitempty"`
	DirectURL        string          `json:"directUrl,omitempty"`
	RegistryAddress  string          `json:"registryAddress,omitempty"`
	RegistryProtocol string          `json:"registryProtocol,omitempty"`
	TimeoutMS        int             `json:"timeoutMs,omitempty"`
	DryRun           bool            `json:"dryRun,omitempty"`
	SessionID        string          `json:"sessionId,omitempty"`
}

func decodeInvokeInput(req *sdkmcp.CallToolRequest) (InvokeInput, any, error) {
	if req == nil || len(req.Params.Arguments) == 0 {
		return InvokeInput{}, nil, nil
	}
	var raw rawInvokeInput
	dec := json.NewDecoder(bytes.NewReader(req.Params.Arguments))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return InvokeInput{}, nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("parse tool arguments: %v", err))
	}
	args, err := decodeJSONValue(raw.Args)
	if err != nil {
		return InvokeInput{}, nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("parse args as JSON: %v", err)).
			WithHint("sofarpc_describe", describeHintArgs(raw.Service, raw.Method),
				"send args as valid JSON or use @<path>")
	}
	return InvokeInput{
		Service:          raw.Service,
		Method:           raw.Method,
		Types:            raw.Types,
		Version:          raw.Version,
		TargetAppName:    raw.TargetAppName,
		DirectURL:        raw.DirectURL,
		RegistryAddress:  raw.RegistryAddress,
		RegistryProtocol: raw.RegistryProtocol,
		TimeoutMS:        raw.TimeoutMS,
		DryRun:           raw.DryRun,
		SessionID:        raw.SessionID,
	}, args, nil
}

func validateExecutionPolicy(plan invoke.Plan, phase string, sources target.Sources) error {
	return executionPolicyFromEnv(sources).Validate(plan, phase)
}

func validateRealInvoke(service string) error {
	return executionPolicyFromEnv(target.Sources{}).ValidateRealInvoke(service, "invoke")
}

func executionPolicyFromEnv(sources target.Sources) invoke.ExecutionPolicy {
	return invoke.ExecutionPolicy{
		AllowInvoke:         envBool(envAllowInvoke),
		AllowedServices:     envCSV(envAllowedServices),
		AllowTargetOverride: envBool(envAllowTargetOverride),
		AllowedTargetHosts:  envCSV(envAllowedTargetHosts),
		Sources:             sources,
	}
}

func envBool(name string) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return false
	}
	value, err := strconv.ParseBool(raw)
	return err == nil && value
}

func envCSV(name string) []string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil
	}
	var out []string
	for _, item := range strings.Split(raw, ",") {
		value := strings.TrimSpace(item)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func capturePlanForSession(sessions *SessionStore, sessionID string, plan invoke.Plan) *PlanCaptureResult {
	if sessions == nil || sessionID == "" {
		return nil
	}
	capture := sessions.CapturePlan(sessionID, plan)
	return &capture
}

func sourcesForSession(base target.Sources, sessions *SessionStore, sessionID string) target.Sources {
	if sessions == nil || strings.TrimSpace(sessionID) == "" {
		return base
	}
	session, ok := sessions.Get(sessionID)
	if !ok || strings.TrimSpace(session.ProjectRoot) == "" {
		return base
	}
	return target.ProjectSources(session.ProjectRoot, base.Env)
}

func diagnosticsWithCapture(base map[string]any, capture *PlanCaptureResult) map[string]any {
	if capture == nil || capture.Captured {
		return base
	}
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	out["sessionPlanCapture"] = capture
	return out
}

// describeHintArgs builds the NextArgs payload for a describe hint. We
// only include fields that are non-empty so the agent never receives a
// hint it can't follow verbatim (an empty required field is worse than a
// nil NextArgs — it looks runnable but isn't).
func describeHintArgs(service, method string) map[string]any {
	if service == "" && method == "" {
		return nil
	}
	args := map[string]any{}
	if service != "" {
		args["service"] = service
	}
	if method != "" {
		args["method"] = method
	}
	return args
}

func invokeToolResult(out any, text string, isError bool) *sdkmcp.CallToolResult {
	result := &sdkmcp.CallToolResult{
		IsError: isError,
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: text}},
	}
	if body, err := json.Marshal(out); err == nil {
		result.StructuredContent = json.RawMessage(body)
	}
	return result
}

func errorText(prefix string, err error) string {
	text := prefix
	var ecerr *errcode.Error
	if errors.As(err, &ecerr) {
		return fmt.Sprintf("%s: %s", ecerr.Code, ecerr.Message)
	}
	if err != nil {
		return err.Error()
	}
	return text
}

func summarizeInvokePlan(plan invoke.Plan, dryRun bool) string {
	prefix := "plan"
	if dryRun {
		prefix = "dry-run plan"
	}
	return fmt.Sprintf("%s: %s.%s target=%s overload=%d/%d argSource=%s",
		prefix, plan.Service, plan.Method, targetAddr(plan.Target),
		plan.Selected+1, len(plan.Overloads), plan.ArgSource)
}

func targetAddr(cfg target.Config) string {
	if cfg.DirectURL != "" {
		return cfg.DirectURL
	}
	if cfg.RegistryAddress != "" {
		return cfg.RegistryAddress
	}
	return string(cfg.Mode)
}
