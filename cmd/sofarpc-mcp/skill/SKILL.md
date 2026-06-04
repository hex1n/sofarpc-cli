---
name: sofarpc-invoke
description: Guides agents through using the sofarpc-mcp MCP tools and prompts to plan, invoke, replay, and diagnose SOFARPC Java facade calls. Use when the user asks an agent to call, test, debug, replay, inspect, or pass gateway-carried context/request baggage for a Java facade/service through SOFARPC/BOLT, or when target, contract, payload-shape, invocationProperties, replay, or real-invoke guardrail errors occur.
---

# sofarpc-invoke

Use the MCP tools for execution and the MCP prompts as workflow shortcuts. Prompts can scaffold bootstrap, dry-run, or diagnosis steps, but they do not execute calls by themselves.

## Quick Start

1. Run `sofarpc_init_project` when a Java checkout has no `.sofarpc/config*.json`; use `dryRun: true` first if project scope is unclear.
2. Run `sofarpc_open` and keep the returned `sessionId`.
3. Run `sofarpc_target` with `explain: true` when target layers or reachability are unclear.
4. Run `sofarpc_describe` when contract data is available; reuse returned `types` to disambiguate overloads.
5. Run `sofarpc_invoke` with `dryRun: true` first unless the user explicitly asks for a live call and the payload is low risk.
6. Inspect resource links such as `sofarpc://session/{sessionId}/plan` when returned.
7. Run `sofarpc_replay` only after understanding the saved plan and any edits.

Useful prompts:
- `sofarpc_bootstrap_project`: inspect and produce setup steps.
- `sofarpc_dry_run_facade_call`: prepare a safe first-call plan.
- `sofarpc_diagnose_failure`: diagnose a failed invoke or replay session.

## Project And Safety

Most tools accept `sessionId`, `project`, or `cwd`. Prefer `sessionId` after `sofarpc_open`; use `project` or `cwd` to select an ad hoc repository. Inspect `.sofarpc/config.json` and `.sofarpc/config.local.json` when behavior depends on config.

Before a real invoke:
- Identify the exact service, method, and overload `types`.
- Use contract-derived argument names, Java types, and overloads.
- Send `args` as an inline JSON array, even for one-parameter methods.
- Confirm side effects with the user unless the request already explicitly asks for the call.
- Treat retries and replay as live calls when `dryRun` is false.

Do not send speculative payloads to production-like targets. Real calls require `SOFARPC_ALLOW_INVOKE=true` and project `allowedServices`; per-call target overrides require `SOFARPC_ALLOW_TARGET_OVERRIDE=true`.

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

## Replay And Resources

For unchanged replay, call `sofarpc_replay` with the `sessionId`. For edited replay, first inspect `sofarpc://session/{sessionId}/plan` if the invoke response returned that resource link. Then submit the edited payload with the original `sessionId` and project context.

Replay supports v2 saved plans. Legacy v1 plans are intentionally rejected. If session capture reports that the plan was too large to store, keep the literal dry-run plan from the invoke response or rebuild with `dryRun: true` and pass the edited payload directly.

## Error Recovery

- `target.missing` or `target.invalid`: call `sofarpc_target` with `explain: true`; use `sofarpc_init_project` if config is absent.
- `contract.method-not-found` or overload ambiguity: call `sofarpc_describe` and include the exact `types`.
- `input.args-invalid`, `contract.method-ambiguous`, or `runtime.serialize-failed`: fix payload shape, send `args` as an array, match Java parameter names inside DTO objects, and preserve list/map nesting.
- Java type mismatch: check primitives, boxed types, enums, arrays, and date/time representations.
- `invocationProperties` validation or missing env: check key names, remove mixed entry modes, export required env vars, then run `sofarpc_doctor`.
- Transport errors: verify address, protocol, timeout, and direct-vs-registry target mode.
- Hessian/BOLT mismatch: inspect target protocol and contract before forcing a wire mode.
- Replay failure: read the saved plan resource before retrying and confirm whether the call is still safe.

## Agent Rules

- Prefer MCP resources over pasted logs when resource links are available.
- Keep dry-run plans redacted when they include env-backed invocation properties.
- Do not claim a call succeeded unless the MCP result shows a successful real invoke.
- Summarize target, method, dry-run status, and session id in user-facing results.
- When blocked, report the exact tool error code, the rejected field, and the next corrective action.
