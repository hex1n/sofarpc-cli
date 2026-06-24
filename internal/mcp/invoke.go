package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/core/invocationprops"
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
	contractModeAuto       = "auto"
	contractModeStrict     = "strict"
	contractModeTrusted    = "trusted"
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
	addRawTool(server, &sdkmcp.Tool{
		Name:         "sofarpc_invoke",
		Title:        "Invoke SOFARPC Method",
		Description:  "Plan and execute a SOFARPC generic invocation. args is a JSON array argument vector; single-parameter methods still use a one-item array. dryRun=true returns the plan without executing the request. Real invokes require SOFARPC_ALLOW_INVOKE=true.",
		Annotations:  remoteInvokeAnnotations("Invoke SOFARPC Method"),
		InputSchema:  invokeInputSchema(),
		OutputSchema: invokeOutputSchema(),
	}, "invoke", func(ecerr *errcode.Error) *sdkmcp.CallToolResult {
		return invokeToolResult(InvokeOutput{Error: ecerr}, errorText("invoke failed", ecerr), true)
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		notifyToolProgress(ctx, req, 0, 5, "decoding invoke input")
		decoded, args, err := decodeInvokeInput(req)
		if err != nil {
			return invokeToolResult(InvokeOutput{Error: asErrcodeError(err)}, errorText("invoke failed", err), true), nil
		}
		contractMode, err := resolveContractMode(decoded)
		if err != nil {
			ecerr := errcode.New(errcode.ArgsInvalid, "invoke", err.Error())
			return invokeToolResult(InvokeOutput{Error: ecerr}, errorText("invoke failed", ecerr), true), nil
		}
		notifyToolProgress(ctx, req, 1, 5, "loading invoke context")
		toolCtx, err := resolveToolContext(sources, sessions, holder, decoded.SessionID, decoded.Cwd, decoded.Project)
		if err != nil {
			ecerr := errcode.New(errcode.ArgsInvalid, "invoke", err.Error())
			return invokeToolResult(InvokeOutput{Error: ecerr}, errorText("invoke failed", ecerr), true), nil
		}
		toolSources := toolCtx.Sources
		// Effective profile: per-call wins over the session's profile; an
		// empty result lets target.Resolve fall back to defaultProfile.
		profile := effectiveProfile(decoded.Profile, toolCtx.SessionProfile)
		notifyToolProgress(ctx, req, 2, 5, "normalizing arguments")
		args, err = normalizeArgs(decoded.Service, decoded.Method, args, toolSources.ProjectRoot)
		if err != nil {
			return invokeToolResult(InvokeOutput{Error: asErrcodeError(err)}, errorText("invoke failed", err), true), nil
		}
		contractSnapshot := toolCtx.Contract
		store := contractSnapshot.store
		if contractMode == contractModeStrict && store == nil {
			ecerr := strictContractUnavailableError(contractSnapshot)
			return invokeToolResult(InvokeOutput{Error: ecerr}, errorText("invoke failed", ecerr), true), nil
		}
		if contractMode == contractModeTrusted {
			store = nil
		}

		notifyToolProgress(ctx, req, 3, 5, "building invoke plan")
		plan, err := invoke.BuildPlan(invoke.Input{
			Service:              decoded.Service,
			Method:               decoded.Method,
			ParamTypes:           decoded.Types,
			Args:                 args,
			Version:              decoded.Version,
			TargetAppName:        decoded.TargetAppName,
			InvocationProperties: decoded.InvocationProperties,
			Target: target.Input{
				Service:          decoded.Service,
				Profile:          profile,
				DirectURL:        decoded.DirectURL,
				RegistryAddress:  decoded.RegistryAddress,
				RegistryProtocol: decoded.RegistryProtocol,
				TimeoutMS:        decoded.TimeoutMS,
			},
		}, store, toolSources)
		if err != nil && shouldAutoTrustedFallback(decoded, args, err, contractMode) {
			plan, err = invoke.BuildPlan(invoke.Input{
				Service:              decoded.Service,
				Method:               decoded.Method,
				ParamTypes:           decoded.Types,
				Args:                 args,
				Version:              decoded.Version,
				TargetAppName:        decoded.TargetAppName,
				InvocationProperties: decoded.InvocationProperties,
				Target: target.Input{
					Service:          decoded.Service,
					Profile:          profile,
					DirectURL:        decoded.DirectURL,
					RegistryAddress:  decoded.RegistryAddress,
					RegistryProtocol: decoded.RegistryProtocol,
					TimeoutMS:        decoded.TimeoutMS,
				},
			}, nil, toolSources)
		}
		if err != nil {
			out := InvokeOutput{Error: asErrcodeError(err)}
			return invokeToolResult(out, errorText("invoke failed", err), true), nil
		}

		capture := capturePlanForSession(sessions, decoded.SessionID, plan)
		links := planCaptureResourceLinks(decoded.SessionID, capture)

		if decoded.DryRun {
			out := InvokeOutput{Ok: true, Plan: &plan, Diagnostics: diagnosticsWithCapture(nil, capture)}
			notifyToolProgress(ctx, req, 5, 5, "invoke dry-run complete")
			return invokeToolResultWithLinks(out, summarizeInvokePlan(plan, true), false, links...), nil
		}

		notifyToolProgress(ctx, req, 4, 5, "executing invoke plan")
		execution := executePlanWithPolicy(ctx, plan, "invoke", toolSources, capture, opts.InvokeLimiter)
		if execution.Err != nil {
			out := InvokeOutput{Plan: &plan, Diagnostics: execution.Outcome.Diagnostics, Error: asErrcodeError(execution.Err)}
			return invokeToolResultWithLinks(out, planExecutionErrorText("invoke", execution), true, links...), nil
		}
		out := InvokeOutput{
			Ok:          true,
			Plan:        &plan,
			Result:      execution.Outcome.Result,
			Diagnostics: execution.Outcome.Diagnostics,
		}
		notifyToolProgress(ctx, req, 5, 5, "invoke complete")
		return invokeToolResultWithLinks(out, summarizeInvokePlan(plan, false), false, links...), nil
	})
}

type rawInvokeInput struct {
	Cwd                  string                       `json:"cwd,omitempty"`
	Project              string                       `json:"project,omitempty"`
	Service              string                       `json:"service,omitempty"`
	Method               string                       `json:"method,omitempty"`
	Types                []string                     `json:"types,omitempty"`
	Args                 json.RawMessage              `json:"args,omitempty"`
	Version              string                       `json:"version,omitempty"`
	TargetAppName        string                       `json:"targetAppName,omitempty"`
	InvocationProperties invocationprops.Declarations `json:"invocationProperties,omitempty"`
	Profile              string                       `json:"profile,omitempty"`
	DirectURL            string                       `json:"directUrl,omitempty"`
	RegistryAddress      string                       `json:"registryAddress,omitempty"`
	RegistryProtocol     string                       `json:"registryProtocol,omitempty"`
	TimeoutMS            int                          `json:"timeoutMs,omitempty"`
	DryRun               bool                         `json:"dryRun,omitempty"`
	Trusted              bool                         `json:"trusted,omitempty"`
	ContractMode         string                       `json:"contractMode,omitempty"`
	SessionID            string                       `json:"sessionId,omitempty"`
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
				"send args as a JSON array")
	}
	return InvokeInput{
		Cwd:                  raw.Cwd,
		Project:              raw.Project,
		Service:              raw.Service,
		Method:               raw.Method,
		Types:                raw.Types,
		Version:              raw.Version,
		TargetAppName:        raw.TargetAppName,
		InvocationProperties: raw.InvocationProperties,
		Profile:              raw.Profile,
		DirectURL:            raw.DirectURL,
		RegistryAddress:      raw.RegistryAddress,
		RegistryProtocol:     raw.RegistryProtocol,
		TimeoutMS:            raw.TimeoutMS,
		DryRun:               raw.DryRun,
		Trusted:              raw.Trusted,
		ContractMode:         raw.ContractMode,
		SessionID:            raw.SessionID,
	}, args, nil
}

func resolveContractMode(in InvokeInput) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(in.ContractMode))
	if mode == "" {
		mode = contractModeAuto
	}
	switch mode {
	case contractModeAuto, contractModeStrict, contractModeTrusted:
	default:
		return "", fmt.Errorf("contractMode must be one of auto, strict, or trusted; got %q", in.ContractMode)
	}
	if in.Trusted {
		if mode == contractModeStrict {
			return "", fmt.Errorf("trusted=true conflicts with contractMode=strict")
		}
		return contractModeTrusted, nil
	}
	return mode, nil
}

func shouldAutoTrustedFallback(in InvokeInput, args any, err error, mode string) bool {
	if mode != contractModeAuto {
		return false
	}
	if !hasCompleteTrustedTuple(in, args) {
		return false
	}
	var ecerr *errcode.Error
	if !errors.As(err, &ecerr) {
		return false
	}
	return ecerr.Code == errcode.ContractUnresolvable
}

func hasCompleteTrustedTuple(in InvokeInput, args any) bool {
	return strings.TrimSpace(in.Service) != "" &&
		strings.TrimSpace(in.Method) != "" &&
		len(in.Types) > 0 &&
		args != nil
}

func strictContractUnavailableError(snapshot contractSnapshot) *errcode.Error {
	msg := "contractMode=strict requires contract information for this workspace"
	if strings.TrimSpace(snapshot.root) != "" {
		msg = fmt.Sprintf("%s (contractRoot=%s)", msg, snapshot.root)
	}
	if strings.TrimSpace(snapshot.loadError) != "" {
		msg = fmt.Sprintf("%s: %s", msg, snapshot.loadError)
	}
	err := errcode.New(errcode.FacadeNotConfigured, "invoke", msg)
	args := map[string]any{}
	if strings.TrimSpace(snapshot.root) != "" {
		args["project"] = snapshot.root
	}
	return err.WithHint("sofarpc_doctor", args, "doctor reports contract availability for the selected project/session")
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
	return structuredToolResult(out, text, isError)
}

func invokeToolResultWithLinks(out any, text string, isError bool, links ...sdkmcp.Content) *sdkmcp.CallToolResult {
	return structuredToolResultWithLinks(out, text, isError, links...)
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
	summary := fmt.Sprintf("%s: %s.%s target=%s overload=%d/%d argSource=%s",
		prefix, plan.Service, plan.Method, targetAddr(plan.Target),
		plan.Selected+1, len(plan.Overloads), plan.ArgSource)
	if plan.Profile != "" {
		summary += " profile=" + plan.Profile
	}
	return summary
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
