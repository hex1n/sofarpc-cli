package mcp

import (
	"context"
	"fmt"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	promptBootstrapProject = "sofarpc_bootstrap_project"
	promptDryRunFacadeCall = "sofarpc_dry_run_facade_call"
	promptDiagnoseFailure  = "sofarpc_diagnose_failure"
)

func registerPrompts(server *sdkmcp.Server) {
	server.AddPrompt(&sdkmcp.Prompt{
		Name:        promptBootstrapProject,
		Title:       "Bootstrap SOFARPC Project",
		Description: "Guide an agent through safe first-time .sofarpc project setup before SOFARPC invocation.",
		Arguments: []*sdkmcp.PromptArgument{
			{Name: "project", Title: "Project Root", Description: "Absolute Java project root when known."},
			{Name: "config", Title: "Config Scope", Description: "local or shared; default to local for machine-specific targets."},
			{Name: "directUrl", Title: "Direct URL", Description: "Optional bolt://host:port target supplied by the user."},
			{Name: "registryAddress", Title: "Registry Address", Description: "Optional registry address supplied by the user."},
		},
	}, bootstrapProjectPrompt)

	server.AddPrompt(&sdkmcp.Prompt{
		Name:        promptDryRunFacadeCall,
		Title:       "Dry-run SOFARPC Facade Call",
		Description: "Guide an agent through opening a project, describing a facade method, and building a dry-run invoke plan.",
		Arguments: []*sdkmcp.PromptArgument{
			{Name: "service", Title: "Service", Description: "Fully qualified Java facade/service interface.", Required: true},
			{Name: "method", Title: "Method", Description: "Facade method to describe and invoke.", Required: true},
			{Name: "sessionId", Title: "Session ID", Description: "Existing sofarpc_open session id, when available."},
			{Name: "project", Title: "Project Root", Description: "Explicit Java project root, when no session exists."},
			{Name: "cwd", Title: "Working Directory", Description: "Working directory for project resolution, when no session exists."},
			{Name: "args", Title: "Args JSON", Description: "Optional inline JSON array argument vector for sofarpc_invoke."},
			{Name: "argsFile", Title: "Args File", Description: "Optional args JSON file path; the agent should pass args as @<path>."},
			{Name: "invocationProperties", Title: "Invocation Properties", Description: "Optional JSON object for gateway-carried SOFARPC request baggage."},
		},
	}, dryRunFacadeCallPrompt)

	server.AddPrompt(&sdkmcp.Prompt{
		Name:        promptDiagnoseFailure,
		Title:       "Diagnose SOFARPC Failure",
		Description: "Guide an agent through structured errcode recovery using target, describe, doctor, and replay tools.",
		Arguments: []*sdkmcp.PromptArgument{
			{Name: "sessionId", Title: "Session ID", Description: "Existing session id whose target/plan should be diagnosed."},
			{Name: "code", Title: "Error Code", Description: "Stable errcode from the failed tool result, such as target.unreachable."},
			{Name: "phase", Title: "Phase", Description: "Failed phase from the error object, if available."},
			{Name: "message", Title: "Error Message", Description: "Failed tool message, useful for invocationProperties and env diagnostics."},
			{Name: "nextTool", Title: "Hint Tool", Description: "hint.nextTool from the failed tool result, when present."},
			{Name: "nextArgs", Title: "Hint Args", Description: "hint.nextArgs JSON from the failed tool result, when present."},
		},
	}, diagnoseFailurePrompt)
}

func bootstrapProjectPrompt(_ context.Context, req *sdkmcp.GetPromptRequest) (*sdkmcp.GetPromptResult, error) {
	args := promptArgs(req)
	config := strings.TrimSpace(args["config"])
	if config == "" {
		config = "local"
	}
	text := strings.TrimSpace(fmt.Sprintf(`
Use the SOFARPC MCP tools to bootstrap a Java project safely.

Known inputs:
- project: %s
- config: %s
- directUrl: %s
- registryAddress: %s

Workflow:
1. If project is empty, call sofarpc_init_project with dryRun=true and inspect projectResolution. Do not write files until project or cwd is explicit.
2. If project is known, call sofarpc_init_project with project, config, and only the target supplied by the user. Do not invent directUrl or registryAddress.
3. Let sofarpc_init_project discover allowedServices from source contracts when services were not supplied. Use allowAllServices only when the user intentionally wants a wildcard allowlist.
4. After setup, call sofarpc_open with the resolved project and keep the returned sessionId.
5. Call sofarpc_target with sessionId and explain=true to show the effective target layers before any invoke workflow.
6. Keep real invoke disabled unless the user explicitly opted in through user-scope guardrails.
`, promptValue(args, "project"), config, promptValue(args, "directUrl"), promptValue(args, "registryAddress")))
	return promptTextResult("Safe project bootstrap workflow for SOFARPC MCP.", text), nil
}

func dryRunFacadeCallPrompt(_ context.Context, req *sdkmcp.GetPromptRequest) (*sdkmcp.GetPromptResult, error) {
	args := promptArgs(req)
	service := strings.TrimSpace(args["service"])
	method := strings.TrimSpace(args["method"])
	if service == "" {
		return nil, fmt.Errorf("service argument is required")
	}
	if method == "" {
		return nil, fmt.Errorf("method argument is required")
	}
	scope := promptScope(args)
	text := strings.TrimSpace(fmt.Sprintf(`
Build a dry-run SOFARPC plan for the requested facade call.

Known inputs:
- service: %s
- method: %s
%s
- args: %s
- argsFile: %s
- invocationProperties: %s

Workflow:
1. If sessionId is missing, call sofarpc_open with project or cwd when available, then keep the returned sessionId.
2. Call sofarpc_describe with sessionId, service, and method. Reuse the returned types to disambiguate overloads.
3. Use provided args when present. Inline args must be a JSON array; if argsFile is present, pass args as "@<argsFile>" so sofarpc_invoke reads the file through SOFARPC_ARGS_FILE_ROOT or the project root.
4. If invocationProperties are present, include them as the invocationProperties object. Treat them as SOFARPC request baggage encoded through rpc_req_baggage; prefer env references for token-like values so dry-run plans stay redacted.
5. Ask the user for missing args or invocation properties only when the Java call cannot be planned without them.
6. Call sofarpc_invoke with sessionId, service, method, types, args, invocationProperties, and dryRun=true.
7. Inspect plan.target, plan.paramTypes, plan.args, plan.invocationProperties, contractSource, diagnostics, and any resource links. Do not send a real invoke yet.
8. If a real invoke is requested later, first confirm the dry-run plan matches intent, required env references resolve, target services support invoke.baggage.enable when request baggage is needed, and project allowlists plus user-scope real-invoke guardrails allow it.
`, service, method, scope, promptValue(args, "args"), promptValue(args, "argsFile"), promptValue(args, "invocationProperties")))
	return promptTextResult("Dry-run facade invocation workflow for SOFARPC MCP.", text), nil
}

func diagnoseFailurePrompt(_ context.Context, req *sdkmcp.GetPromptRequest) (*sdkmcp.GetPromptResult, error) {
	args := promptArgs(req)
	code := strings.TrimSpace(args["code"])
	if code == "" {
		code = "<use failed result code>"
	}
	text := strings.TrimSpace(fmt.Sprintf(`
Diagnose a SOFARPC MCP failure by following structured recovery data, not prose guesses.

Known inputs:
- sessionId: %s
- code: %s
- phase: %s
- message: %s
- hint.nextTool: %s
- hint.nextArgs: %s

Workflow:
1. If hint.nextTool and hint.nextArgs are present, call that tool with those arguments first.
2. If sessionId is present, read sofarpc://session/{sessionId}. If the session or failed result exposes sofarpc://session/{sessionId}/plan, read it before replaying or editing a payload.
3. For target.missing, target.invalid, target.unreachable, target.connect-failed, runtime.timeout, or runtime.protocol-failed, call sofarpc_target with explain=true and then sofarpc_doctor with sessionId when available.
4. For contract.method-not-found, contract.method-ambiguous, input.args-invalid, or runtime.serialize-failed, call sofarpc_describe with service/method/sessionId and compare overloads, paramTypes, normalized args, and payload shape.
5. For input.args-invalid messages that mention invocationProperties, env, or request baggage, call sofarpc_doctor with the same session/project context and inspect the invocation-properties check. Missing or empty env references must be fixed before any real invoke or replay.
6. For replay.plan-version-unsupported, rebuild a fresh dry-run plan with sofarpc_invoke dryRun=true.
7. For runtime.rejected, report the guardrail reason and do not retry blindly or bypass policy.
8. End with the stable errcode, the next tool result, and whether the failure is target, contract, payload, invocation-properties, policy, or runtime.
`, promptValue(args, "sessionId"), code, promptValue(args, "phase"), promptValue(args, "message"), promptValue(args, "nextTool"), promptValue(args, "nextArgs")))
	return promptTextResult("Structured SOFARPC MCP failure recovery workflow.", text), nil
}

func promptTextResult(description, text string) *sdkmcp.GetPromptResult {
	return &sdkmcp.GetPromptResult{
		Description: description,
		Messages: []*sdkmcp.PromptMessage{{
			Role:    "user",
			Content: &sdkmcp.TextContent{Text: text},
		}},
	}
}

func promptArgs(req *sdkmcp.GetPromptRequest) map[string]string {
	if req == nil || req.Params == nil || req.Params.Arguments == nil {
		return map[string]string{}
	}
	return req.Params.Arguments
}

func promptValue(args map[string]string, key string) string {
	value := strings.TrimSpace(args[key])
	if value == "" {
		return "<not provided>"
	}
	return value
}

func promptScope(args map[string]string) string {
	fields := []string{
		"- sessionId: " + promptValue(args, "sessionId"),
		"- project: " + promptValue(args, "project"),
		"- cwd: " + promptValue(args, "cwd"),
	}
	return strings.Join(fields, "\n")
}
