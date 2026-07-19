package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hex1n/sofarpc-cli/internal/core/contract"
	"github.com/hex1n/sofarpc-cli/internal/core/invoke"
	"github.com/hex1n/sofarpc-cli/internal/core/target"
	"github.com/hex1n/sofarpc-cli/internal/errcode"
)

// callOutput mirrors the sofarpc_invoke MCP output shape so agents, scripts,
// and humans read the same JSON regardless of the entrypoint.
type callOutput struct {
	Ok          bool           `json:"ok"`
	Plan        *invoke.Plan   `json:"plan,omitempty"`
	Result      any            `json:"result,omitempty"`
	Diagnostics map[string]any `json:"diagnostics,omitempty"`
	Error       *errcode.Error `json:"error,omitempty"`
}

type callFlags struct {
	project        string
	service        string
	method         string
	types          string
	args           string
	argsFile       string
	planFile       string
	profile        string
	directURL      string
	serviceVersion string
	targetApp      string
	timeoutMS      int
	dryRun         bool
}

// runCall implements `sofarpc-mcp call`: a one-shot plan/execute path over
// the same core pipeline and execution guardrails as the MCP tools. Real
// invokes still require SOFARPC_ALLOW_INVOKE=true plus the project service
// allowlist; -dry-run prints the plan without touching the wire.
func runCall(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("call", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var in callFlags
	fs.StringVar(&in.project, "project", "", "Java project root providing .sofarpc config and source contracts (default: current directory)")
	fs.StringVar(&in.service, "service", "", "service interface FQN, for example com.foo.UserFacade")
	fs.StringVar(&in.method, "method", "", "method name to invoke")
	fs.StringVar(&in.types, "types", "", "comma-separated parameter type FQNs (enables trusted mode without local contracts)")
	fs.StringVar(&in.args, "args", "", "JSON array argument vector, for example '[\"id-1\"]'")
	fs.StringVar(&in.argsFile, "args-file", "", "read the JSON array argument vector from a file, or - for stdin")
	fs.StringVar(&in.planFile, "plan", "", "execute a captured plan JSON (invoke dry-run output or bare plan) from a file, or - for stdin")
	fs.StringVar(&in.profile, "profile", "", "Target Profile to resolve the target under")
	fs.StringVar(&in.directURL, "direct-url", "", "direct target override, for example bolt://127.0.0.1:12200")
	fs.StringVar(&in.serviceVersion, "service-version", "", "SOFARPC service version for targetServiceUniqueName")
	fs.StringVar(&in.targetApp, "target-app", "", "optional target application name transport hint")
	fs.IntVar(&in.timeoutMS, "timeout-ms", 0, "per-call timeout override in milliseconds")
	fs.BoolVar(&in.dryRun, "dry-run", false, "build and print the plan without executing it")
	if err := fs.Parse(args); err != nil {
		return err
	}

	projectRoot := strings.TrimSpace(in.project)
	if projectRoot == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve working directory: %w", err)
		}
		projectRoot = wd
	}
	sources := target.ProjectSources(projectRoot, target.Config{})

	plan, err := callPlan(in, projectRoot, sources)
	if err != nil {
		return writeCallOutput(w, callOutput{Error: asCallError(err)}, err)
	}

	if in.dryRun {
		return writeCallOutput(w, callOutput{Ok: true, Plan: &plan}, nil)
	}

	policy := callExecutionPolicy(sources)
	if err := policy.Validate(plan, "call"); err != nil {
		return writeCallOutput(w, callOutput{Plan: &plan, Error: asCallError(err)}, err)
	}
	outcome, err := invoke.Execute(context.Background(), plan, "call")
	if err != nil {
		return writeCallOutput(w, callOutput{Plan: &plan, Diagnostics: outcome.Diagnostics, Error: asCallError(err)}, err)
	}
	return writeCallOutput(w, callOutput{
		Ok:          true,
		Plan:        &plan,
		Result:      outcome.Result,
		Diagnostics: outcome.Diagnostics,
	}, nil)
}

// callPlan produces the executable plan either from a captured plan file or
// by building one from the service/method flags, mirroring sofarpc_invoke's
// auto contract mode: contract-aware when sources parse, trusted fallback
// when the service is not in the local contract index but the caller
// supplied the full trusted tuple.
func callPlan(in callFlags, projectRoot string, sources target.Sources) (invoke.Plan, error) {
	if strings.TrimSpace(in.planFile) != "" {
		if strings.TrimSpace(in.service) != "" || strings.TrimSpace(in.method) != "" ||
			strings.TrimSpace(in.types) != "" || strings.TrimSpace(in.args) != "" ||
			strings.TrimSpace(in.argsFile) != "" {
			return invoke.Plan{}, errcode.New(errcode.ArgsInvalid, "call",
				"-plan replays a captured plan and cannot be combined with -service/-method/-types/-args")
		}
		return callPlanFromFile(in.planFile)
	}
	if strings.TrimSpace(in.service) == "" || strings.TrimSpace(in.method) == "" {
		return invoke.Plan{}, errcode.New(errcode.ArgsInvalid, "call",
			"-service and -method are required unless -plan is supplied")
	}
	args, err := callArgs(in)
	if err != nil {
		return invoke.Plan{}, err
	}
	paramTypes := splitCallTypes(in.types)
	input := invoke.Input{
		Service:       in.service,
		Method:        in.method,
		ParamTypes:    paramTypes,
		Args:          args,
		Version:       in.serviceVersion,
		TargetAppName: in.targetApp,
		Target: target.Input{
			Service:   in.service,
			Profile:   in.profile,
			DirectURL: in.directURL,
			TimeoutMS: in.timeoutMS,
		},
	}
	store := callContractStore(projectRoot)
	plan, err := invoke.BuildPlan(input, store, sources)
	if err != nil && store != nil && callTrustedFallback(err, paramTypes, args) {
		plan, err = invoke.BuildPlan(input, nil, sources)
	}
	return plan, err
}

func callContractStore(projectRoot string) contract.Store {
	store, _ := loadContractStore(projectRoot)
	if store == nil {
		return nil
	}
	return store
}

// callTrustedFallback matches sofarpc_invoke's auto contract mode: an
// unresolvable local contract falls back to trusted execution only when the
// caller supplied the complete trusted tuple.
func callTrustedFallback(err error, paramTypes []string, args any) bool {
	if len(paramTypes) == 0 || args == nil {
		return false
	}
	var ecerr *errcode.Error
	return errors.As(err, &ecerr) && ecerr.Code == errcode.ContractUnresolvable
}

func callArgs(in callFlags) (any, error) {
	raw := strings.TrimSpace(in.args)
	if in.argsFile != "" {
		if raw != "" {
			return nil, errcode.New(errcode.ArgsInvalid, "call", "-args and -args-file are mutually exclusive")
		}
		body, err := readCallInput(in.argsFile)
		if err != nil {
			return nil, errcode.New(errcode.ArgsInvalid, "call", fmt.Sprintf("read args file: %v", err))
		}
		raw = strings.TrimSpace(string(body))
	}
	if raw == "" {
		return nil, nil
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, errcode.New(errcode.ArgsInvalid, "call", fmt.Sprintf("parse args as JSON: %v", err))
	}
	return value, nil
}

func callPlanFromFile(path string) (invoke.Plan, error) {
	body, err := readCallInput(path)
	if err != nil {
		return invoke.Plan{}, errcode.New(errcode.ArgsInvalid, "call", fmt.Sprintf("read plan: %v", err))
	}
	payload := callPlanPayload(body)
	dec := json.NewDecoder(strings.NewReader(string(payload)))
	dec.UseNumber()
	var plan invoke.Plan
	if err := dec.Decode(&plan); err != nil {
		return invoke.Plan{}, errcode.New(errcode.ArgsInvalid, "call",
			fmt.Sprintf("plan payload is not a plan: %v", err))
	}
	if err := invoke.ValidateReplayPlan(plan, "call"); err != nil {
		return invoke.Plan{}, err
	}
	return plan, nil
}

// callPlanPayload unwraps the common envelopes around a plan: sofarpc_invoke
// dry-run output ({"plan": ...}) and MCP structured content
// ({"structuredContent": {"plan": ...}}). A bare plan passes through.
func callPlanPayload(body []byte) []byte {
	var envelope struct {
		Plan              json.RawMessage `json:"plan"`
		StructuredContent struct {
			Plan json.RawMessage `json:"plan"`
		} `json:"structuredContent"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return body
	}
	switch {
	case len(envelope.Plan) > 0:
		return envelope.Plan
	case len(envelope.StructuredContent.Plan) > 0:
		return envelope.StructuredContent.Plan
	default:
		return body
	}
}

func readCallInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func splitCallTypes(raw string) []string {
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if value := strings.TrimSpace(item); value != "" {
			out = append(out, value)
		}
	}
	return out
}

// callExecutionPolicy adapts the same environment guardrails and project
// allowlist the MCP server enforces, so `call` cannot reach targets the
// MCP tools would reject. Env parsing goes through the shared
// invoke.EnvFlag/EnvList helpers to keep both entrypoints identical.
func callExecutionPolicy(sources target.Sources) invoke.ExecutionPolicy {
	allowlist := target.ServiceAllowlistForSources(sources)
	policy := invoke.ExecutionPolicy{
		AllowInvoke:         invoke.EnvFlag(invoke.EnvAllowInvoke),
		AllowTargetOverride: invoke.EnvFlag(invoke.EnvAllowTargetOverride),
		AllowedTargetHosts:  invoke.EnvList(invoke.EnvAllowedTargetHosts),
		Sources:             sources,
	}
	if allowlist.Configured {
		policy.AllowedServices = allowlist.Services
		policy.AllowedServicesConfigured = true
		policy.AllowedServicesSource = allowlist.Source
	}
	return policy
}

func asCallError(err error) *errcode.Error {
	var ecerr *errcode.Error
	if errors.As(err, &ecerr) {
		return ecerr
	}
	return errcode.New(errcode.ArgsInvalid, "call", err.Error())
}

func writeCallOutput(w io.Writer, out callOutput, cause error) error {
	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal call output: %w", err)
	}
	if _, err := fmt.Fprintln(w, string(body)); err != nil {
		return err
	}
	return cause
}
