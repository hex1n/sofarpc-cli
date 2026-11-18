---
name: sofarpc-invoke
description: Plan, invoke, replay, and diagnose SOFARPC Java facade calls through MCP or the sofarpc-mcp CLI. Use when the user asks an agent to call, test, debug, replay, inspect, or pass gateway-carried context/request baggage for a Java facade/service through SOFARPC/BOLT, or when target, contract, payload-shape, invocationProperties, replay, or real-invoke guardrail errors occur.
---

# sofarpc-invoke

## Choose MCP Or CLI

Follow the user's requested path. Otherwise, choose based on what the task needs and what the agent can access.

- Use **MCP** for workspace discovery, target and contract diagnostics, per-call `invocationProperties`, or work with an existing session or resource link.
- Use **CLI** when the binary is available in the shell and the project and call details are already known. This fits one-shot calls, local argument files, and saved plans, including when MCP is unavailable.

You can inspect with MCP and use a saved plan with CLI. MCP and the shell can inherit different real-invoke settings and baggage environment variables. Once you choose a path for execution, use it for the preflight and live call.

## Plan Before Executing

1. Start with MCP `dryRun: true` or CLI `-dry-run`. For a preview-only request, use the existing environment.
2. When the user explicitly authorizes a live CLI call and `SOFARPC_ALLOW_INVOKE` is absent, pass `SOFARPC_ALLOW_INVOKE=true` only in the child process environment for both preflight and execution. Preserve any existing value, including `false` or an empty value. An MCP call uses the server's configured environment.
3. Check `ok` for plan creation and `preflight.ready` for local readiness. If policy or execution checks fail, report the specific `preflight.policyError` or `preflight.executionError` and stop the attempt. Keep the selected path when a policy rejects the call.
4. After a successful preflight with live-call authorization, execute using the same project, profile, target, payload or saved plan, and environment. Set MCP `dryRun: false` or omit CLI `-dry-run`. The user's authorization for that call covers this step.

Preflight covers local policy, plan, and target address checks. TCP reachability, baggage env resolution, and wire encoding are checked separately.

## CLI Workflow

Run `sofarpc-mcp call` through the shell. Pass `-project` to select the Java checkout and `-profile` to choose a named target profile for a new call. Pass comma-separated Java type names in `-types` to select the overload. Explicit types and arguments also allow a trusted fallback when the contract cannot be resolved. Use `-args-file` with a JSON array file for complex arguments. For a zero-argument dry run:

```powershell
sofarpc-mcp call -project . -profile dev -service com.example.DemoFacade -method ping -types= -args '[]' -dry-run
```

Preview a saved plan with `sofarpc-mcp call -project . -plan plan.json -dry-run`. The plan carries its resolved target and the selected project's current policy still applies. Read plan and execution results as JSON on stdout, including structured failures on an unsuccessful exit.

To pass a child-only variable on a POSIX shell, prefix the invocation with `SOFARPC_ALLOW_INVOKE=true`. In PowerShell, a subprocess launcher can set the variable in `ProcessStartInfo.Environment` and pass flags through `ArgumentList`. Use the same child environment for the dry run and authorized live call.

## MCP Workflow

If Java sources changed, reopen the project with `refreshContract: true`. MCP prompts can scaffold bootstrap, dry-run, or diagnosis steps. Use tools to execute the resulting steps.

1. Run `sofarpc_init_project` when a Java checkout has no `.sofarpc/config*.json`; use `dryRun: true` first if project scope is unclear.
2. Run `sofarpc_open` and keep the returned `sessionId`.
3. Run `sofarpc_target` with `explain: true` when target layers or reachability are unclear.
4. Run `sofarpc_describe` when contract data is available; reuse returned `types` to disambiguate overloads.
5. Run `sofarpc_invoke` using the selected overload and the appropriate dry-run setting.
6. Inspect resource links such as `sofarpc://session/{sessionId}/plan` when returned.
7. Run `sofarpc_replay` only after understanding the saved plan and any edits.

Useful prompts:
- `sofarpc_bootstrap_project`: inspect and produce setup steps.
- `sofarpc_dry_run_facade_call`: prepare a safe first-call plan.
- `sofarpc_diagnose_failure`: diagnose a failed invoke or replay session.

## Project And Safety

Most MCP tools accept `sessionId`, `project`, or `cwd`. Prefer `sessionId` after `sofarpc_open`; use `project` or `cwd` to select an ad hoc repository. Inspect `.sofarpc/config.json` and `.sofarpc/config.local.json` when behavior depends on config.

Before a real invoke:
- Identify the exact service, method, and overload `types`.
- Use contract-derived argument names, Java types, and overloads.
- Send an explicit argument array before executing a parameterized method. In MCP, use `types: []` and `args: []` for a zero-argument trusted call.
- Confirm side effects with the user unless the request already explicitly asks for the call.
- Treat retries and replay as live calls whenever dry-run is disabled.

Do not send speculative payloads to production-like targets. Real calls require `SOFARPC_ALLOW_INVOKE=true`; per-call target overrides require `SOFARPC_ALLOW_TARGET_OVERRIDE=true`.

## Invoke Payloads

Contract-assisted shape:

```json
{
  "service": "com.example.DemoFacade",
  "method": "echo",
  "types": ["com.example.EchoRequest"],
  "args": [{"message": "hello"}],
  "invocationProperties": {
    "tenant": {"value": "dev"},
    "authToken": {"env": "SOFARPC_AUTH_TOKEN"}
  },
  "dryRun": true
}
```

Use `args` as an inline JSON array. Contract-assisted planning injects DTO `@type`, numeric wrappers, enum shapes, and nested collection normalization where the Java contract makes that safe.

Never send a bare JSON integer outside JavaScript's safe-integer range for a Java `long`/`Long`: the MCP boundary rejects it because a calling JavaScript host may already have rounded the value. Send the exact decimal as a string or as `{"@type":"java.lang.Long","value":"<digits>"}`; `@file` can carry that string/wrapper payload without another object serialization step.

For overloads, include `types` from `sofarpc_describe`. Use `contractMode: "trusted"` or `trusted: true` only when the user intentionally bypasses contract parsing and provides the complete `service`, `method`, `types`, and ordered `args` tuple.

## Invocation Properties

Use `invocationProperties` for gateway-carried request context, such as tenant, route, trace, identity, or token-like values that downstream Java services read from request baggage. Do not model this data as facade method arguments unless the Java contract actually declares them.

Supported entries:
- `{"value": "literal"}` sends a literal string.
- `{"env": "ENV_NAME"}` resolves the value from the environment for real invoke or replay.
- `{"unset": true}` removes or masks a value from lower-precedence config.

Precedence is per-call payload over `.sofarpc/config.local.json` over `.sofarpc/config.json`. Env references remain redacted in dry-run plans and saved sessions. They resolve only for real invoke or replay; missing or empty env values fail before wire IO.

On the wire, direct BOLT calls encode resolved entries into `SofaRequest.requestProps["rpc_req_baggage"]`. Java providers typically read them with `RpcInvokeContext.getRequestBaggage(...)`; the provider still needs SOFARPC baggage support enabled, commonly `invoke.baggage.enable`.

Never put secrets in literal `value` entries. Prefer `env`, run `sofarpc_doctor` if env resolution fails, and keep copied replay plans redacted.

## Trace Correlation

When a real invoke or replay reaches the transport, it creates a SOFATracer trace context and sets `SofaRequest.requestProps["rpc_trace_context"]`. A provider running SOFATracer adopts the ID for its server span. Use the exact `diagnostics.traceId` to search its digest and business logs. Calls arrive tagged with caller app `sofarpc-cli`.

On MCP failure, read the first text block for the error code, phase, available attempt details, and suggested diagnostic. For CLI, use the `error` and `diagnostics` JSON fields. Report the available trace ID with the error. Use `requestState` to decide where to look. For `not-sent`, diagnose the local failure stage first. For `write-uncertain`, `request-written`, or `response-received`, correlate available provider logs by trace ID before considering a retry. `request-written` means the full request was accepted by the local socket. Each execution creates a fresh trace ID.

## Replay And Resources

For unchanged replay, call `sofarpc_replay` with the `sessionId`. For edited replay, first inspect `sofarpc://session/{sessionId}/plan` if the invoke response returned that resource link. Then submit the edited payload with the original `sessionId` and project context.

Replay supports v2 saved plans. Legacy v1 plans are intentionally rejected. If session capture reports that the plan was too large to store, keep the literal dry-run plan from the invoke response or rebuild with `dryRun: true` and pass the edited payload directly.

## Error Recovery

- `target.missing` or `target.invalid`: call `sofarpc_target` with `explain: true`; use `sofarpc_init_project` if config is absent.
- `contract.method-not-found` or overload ambiguity: call `sofarpc_describe` and include the exact `types`.
- `input.args-invalid`, `contract.method-ambiguous`, or `runtime.serialize-failed`: fix payload shape, send `args` as an array, match Java parameter names inside DTO objects, and preserve list/map nesting.
- Java type mismatch: check primitives, boxed types, enums, arrays, and date/time representations.
- `invocationProperties` validation or missing env: check key names, remove mixed entry modes, export required env vars, then run `sofarpc_doctor`.
- Transport errors require the resolved address, protocol, and timeout settings. Follow the diagnostic hint with its project and profile, then use the attempt state to decide whether provider logs can help.
- Hessian/BOLT mismatch: inspect target protocol and contract before forcing a wire mode.
- Replay failure: read the saved plan resource before retrying and confirm whether the call is still safe.

## Agent Rules

- Prefer MCP resources over pasted logs when resource links are available.
- Keep dry-run plans redacted when they include env-backed invocation properties.
- Claim call success only when the live MCP or CLI result confirms it. Check the business response when the task expects a specific outcome.
- Summarize target, method, dry-run status, and session id when available.
- When blocked, report the exact tool error code, the rejected field, and the next corrective action.
