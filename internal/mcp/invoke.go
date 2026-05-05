package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	envAllowInvoke         = "SOFARPC_ALLOW_INVOKE"
	envAllowedServices     = "SOFARPC_ALLOWED_SERVICES"
	envAllowTargetOverride = "SOFARPC_ALLOW_TARGET_OVERRIDE"
	envAllowedTargetHosts  = "SOFARPC_ALLOWED_TARGET_HOSTS"
	envArgsFileRoot        = "SOFARPC_ARGS_FILE_ROOT"
	envArgsFileMaxBytes    = "SOFARPC_ARGS_FILE_MAX_BYTES"
	defaultArgsFileMax     = int64(1 << 20)
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
		args, err = normalizeArgs(decoded.Service, decoded.Method, args, sources.ProjectRoot)
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
		}, store, sources)
		if err != nil {
			out := InvokeOutput{Error: asErrcodeError(err)}
			return invokeToolResult(out, errorText("invoke failed", err), true), nil
		}

		capture := capturePlanForSession(sessions, decoded.SessionID, plan)

		if decoded.DryRun {
			out := InvokeOutput{Ok: true, Plan: &plan, Diagnostics: diagnosticsWithCapture(nil, capture)}
			return invokeToolResult(out, summarizeInvokePlan(plan, true), false), nil
		}

		if err := validateExecutionPolicy(plan, "invoke", sources); err != nil {
			out := InvokeOutput{Plan: &plan, Diagnostics: diagnosticsWithCapture(nil, capture), Error: asErrcodeError(err)}
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

func decodeJSONValue(raw json.RawMessage) (any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var parsed any
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	if err := dec.Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

// normalizeArgs normalizes the loosely-typed args field:
//   - nil              → nil (plan renders a skeleton).
//   - "@<path>" string → read the file, parse as JSON with UseNumber.
//   - anything else    → pass through verbatim for BuildPlan to shape-check.
//
// Relative @file paths are resolved against SOFARPC_ARGS_FILE_ROOT when set,
// otherwise against the MCP project root. Absolute paths must still remain
// inside that root after symlink resolution.
//
// service/method are threaded in only to pre-fill the describe hint on
// failure — empty values are dropped so the agent never sees a hint it
// can't follow verbatim.
func normalizeArgs(service, method string, raw any, projectRoot string) (any, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case string:
		if !strings.HasPrefix(v, "@") {
			return v, nil
		}
		path := strings.TrimPrefix(v, "@")
		if path == "" {
			return nil, errcode.New(errcode.ArgsInvalid, "invoke",
				"args '@' prefix requires a file path").
				WithHint("sofarpc_describe", describeHintArgs(service, method),
					"use @<path> rooted at SOFARPC_ARGS_FILE_ROOT or the project root")
		}
		return readArgsFile(service, method, path, projectRoot)
	default:
		return raw, nil
	}
}

// readArgsFile loads path and decodes it as JSON with UseNumber. Errors are
// wrapped into input.args-invalid so the agent sees one shape regardless
// of whether args came inline or from disk.
func readArgsFile(service, method, path, projectRoot string) (any, error) {
	resolved, err := resolveArgsFilePath(path, projectRoot)
	if err != nil {
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("resolve args file %q: %v", path, err)).
			WithHint("sofarpc_doctor", nil, "check SOFARPC_ARGS_FILE_ROOT and the file path")
	}
	maxBytes := argsFileMaxBytes()
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("stat args file %q: %v", resolved, err)).
			WithHint("sofarpc_doctor", nil, "check that the mcp process can read the file")
	}
	if info.IsDir() {
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("args file %q is a directory", resolved)).
			WithHint("sofarpc_describe", describeHintArgs(service, method), "use a JSON file")
	}
	if info.Size() > maxBytes {
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("args file %q is %d bytes, over limit %d", resolved, info.Size(), maxBytes)).
			WithHint("sofarpc_describe", describeHintArgs(service, method), "use a smaller JSON file or raise SOFARPC_ARGS_FILE_MAX_BYTES")
	}
	body, err := os.ReadFile(resolved)
	if err != nil {
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("read args file %q: %v", resolved, err)).
			WithHint("sofarpc_doctor", nil, "check that the mcp process can read the file")
	}
	if int64(len(body)) > maxBytes {
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("args file %q grew over limit %d", resolved, maxBytes)).
			WithHint("sofarpc_describe", describeHintArgs(service, method), "use a smaller JSON file or raise SOFARPC_ARGS_FILE_MAX_BYTES")
	}
	parsed, err := decodeJSONValue(body)
	if err != nil {
		return nil, errcode.New(errcode.ArgsInvalid, "invoke",
			fmt.Sprintf("parse args file %q as JSON: %v", resolved, err)).
			WithHint("sofarpc_describe", describeHintArgs(service, method),
				"the file must contain valid JSON matching paramTypes")
	}
	return parsed, nil
}

func resolveArgsFilePath(path, projectRoot string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("empty file path")
	}
	root := strings.TrimSpace(os.Getenv(envArgsFileRoot))
	if root == "" {
		root = strings.TrimSpace(projectRoot)
	}
	if root == "" {
		return "", fmt.Errorf("args file root is not configured")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absRoot, err = filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("resolve args file root: %w", err)
	}

	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(absRoot, candidate)
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absCandidate)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absRoot, resolved)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path escapes args file root %q", absRoot)
	}
	return resolved, nil
}

func argsFileMaxBytes() int64 {
	raw := strings.TrimSpace(os.Getenv(envArgsFileMaxBytes))
	if raw == "" {
		return defaultArgsFileMax
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return defaultArgsFileMax
	}
	return value
}

func validateExecutionPolicy(plan invoke.Plan, phase string, sources target.Sources) error {
	if err := validateRealInvokeForPhase(plan.Service, phase); err != nil {
		return err
	}
	return validateTargetPolicy(plan, phase, sources)
}

func validateRealInvoke(service string) error {
	return validateRealInvokeForPhase(service, "invoke")
}

func validateRealInvokeForPhase(service, phase string) error {
	if strings.TrimSpace(phase) == "" {
		phase = "invoke"
	}
	if !envBool(envAllowInvoke) {
		return errcode.New(errcode.InvocationRejected, phase,
			"real invoke is disabled; set SOFARPC_ALLOW_INVOKE=true to enable non-dry-run calls").
			WithHint("sofarpc_invoke", map[string]any{"dryRun": true}, "inspect the plan safely first")
	}
	if !serviceAllowed(service) {
		return errcode.New(errcode.InvocationRejected, phase,
			fmt.Sprintf("service %q is not allowed by SOFARPC_ALLOWED_SERVICES", service)).
			WithHint("sofarpc_doctor", nil, "inspect invoke safety configuration")
	}
	return nil
}

func validateTargetPolicy(plan invoke.Plan, phase string, sources target.Sources) error {
	cfg := target.Normalize(plan.Target)
	if cfg.Mode != target.ModeDirect || cfg.DirectURL == "" {
		return nil
	}

	envCfg := target.Normalize(sources.Env)
	if !envBool(envAllowTargetOverride) {
		switch {
		case envCfg.DirectURL == "":
			return errcode.New(errcode.InvocationRejected, phase,
				fmt.Sprintf("direct target %q is not allowed; set SOFARPC_DIRECT_URL on the MCP server env or set SOFARPC_ALLOW_TARGET_OVERRIDE=true", cfg.DirectURL)).
				WithHint("sofarpc_target", map[string]any{"explain": true},
					"inspect the resolved target before enabling real invoke")
		default:
			same, err := sameDirectTarget(cfg.DirectURL, envCfg.DirectURL)
			if err != nil {
				return errcode.New(errcode.TargetInvalid, phase, err.Error()).
					WithHint("sofarpc_target", map[string]any{"explain": true},
						"inspect the resolved direct target address")
			}
			if !same {
				return errcode.New(errcode.InvocationRejected, phase,
					fmt.Sprintf("direct target %q does not match SOFARPC_DIRECT_URL; set SOFARPC_ALLOW_TARGET_OVERRIDE=true to allow per-call target overrides", cfg.DirectURL)).
					WithHint("sofarpc_target", map[string]any{"explain": true},
						"inspect the resolved target layers")
			}
		}
	}

	allowed, host, err := directTargetHostAllowed(cfg.DirectURL)
	if err != nil {
		return errcode.New(errcode.TargetInvalid, phase, err.Error()).
			WithHint("sofarpc_target", map[string]any{"explain": true},
				"inspect the resolved direct target address")
	}
	if !allowed {
		return errcode.New(errcode.InvocationRejected, phase,
			fmt.Sprintf("direct target host %q is not allowed by SOFARPC_ALLOWED_TARGET_HOSTS", host)).
			WithHint("sofarpc_doctor", nil, "inspect invoke safety configuration")
	}
	return nil
}

func sameDirectTarget(left, right string) (bool, error) {
	leftDial, err := target.ParseDirectDialAddress(left)
	if err != nil {
		return false, err
	}
	rightDial, err := target.ParseDirectDialAddress(right)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(leftDial, rightDial), nil
}

func directTargetHostAllowed(directURL string) (bool, string, error) {
	raw := strings.TrimSpace(os.Getenv(envAllowedTargetHosts))
	if raw == "" {
		return true, "", nil
	}
	dialTarget, err := target.ParseDirectDialAddress(directURL)
	if err != nil {
		return false, "", err
	}
	host, port, err := net.SplitHostPort(dialTarget)
	if err != nil {
		return false, "", fmt.Errorf("parse direct target host: %w", err)
	}
	normalizedDial := net.JoinHostPort(host, port)
	for _, item := range strings.Split(raw, ",") {
		allowed := strings.TrimSpace(item)
		if allowed == "" {
			continue
		}
		if allowed == "*" ||
			strings.EqualFold(allowed, host) ||
			strings.EqualFold(allowed, dialTarget) ||
			strings.EqualFold(allowed, normalizedDial) {
			return true, host, nil
		}
	}
	return false, host, nil
}

func envBool(name string) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return false
	}
	value, err := strconv.ParseBool(raw)
	return err == nil && value
}

func serviceAllowed(service string) bool {
	raw := strings.TrimSpace(os.Getenv(envAllowedServices))
	if raw == "" {
		return true
	}
	for _, item := range strings.Split(raw, ",") {
		allowed := strings.TrimSpace(item)
		if allowed == "*" || allowed == service {
			return true
		}
	}
	return false
}

func capturePlanForSession(sessions *SessionStore, sessionID string, plan invoke.Plan) *PlanCaptureResult {
	if sessions == nil || sessionID == "" {
		return nil
	}
	capture := sessions.CapturePlan(sessionID, plan)
	return &capture
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
