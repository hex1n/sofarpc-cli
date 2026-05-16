---
name: sofarpc-invoke
description: Guides agents through using the sofarpc-mcp MCP tools to plan, invoke, replay, and diagnose SOFARPC Java facade calls. Use when the user asks an agent to call, test, debug, replay, or inspect a Java facade/service through SOFARPC/BOLT, or when target, contract, payload-shape, replay, or real-invoke guardrail errors occur.
---

# sofarpc-invoke
Agent playbook for the `sofarpc-mcp` tools: `sofarpc_open`,
`sofarpc_target`, `sofarpc_describe`, `sofarpc_invoke`, `sofarpc_replay`, and
`sofarpc_doctor`.

## Operating Loop
1. Call `sofarpc_open` once for a new project/session. Keep the `sessionId`.
2. Read `capabilities` and `contract` from `sofarpc_open`.
3. If `capabilities.describe == true`, call `sofarpc_describe` with
   `service` and `method`; reuse the returned `types`.
4. Call `sofarpc_invoke` with `dryRun: true` unless the user explicitly asked
   to send a real request.
5. Inspect `plan.target`, `plan.paramTypes`, `plan.args`, and `contractSource`.
6. For real calls, invoke without `dryRun` only after the plan matches intent.
7. On failure, follow `hint.nextTool` / `hint.nextArgs` before guessing.

## Preconditions
- Server registered: `sofarpc-mcp setup --scope=user`.
- Target resolution: per-call input, `.sofarpc/config.local.json`,
  `.sofarpc/config.json`, MCP env, defaults. Project config must not set `mode`.
- Contract data comes from `SOFARPC_PROJECT_ROOT` or project CWD. If no contract
  is available, use trusted mode.
- Real calls require `SOFARPC_ALLOW_INVOKE=true`; per-call `directUrl`
  overrides require `SOFARPC_ALLOW_TARGET_OVERRIDE=true`.

## Invoke Shapes
Contract-assisted invoke, preferred when `describe` is available:

```json
{
  "service": "com.foo.OrderFacade",
  "method": "query",
  "types": ["com.foo.OrderQueryRequest"],
  "args": [{ "orderId": 42, "includeItems": true }],
  "sessionId": "ws_..."
}
```

The contract layer adds DTO `@type` tags and Java numeric wrappers. Do not
pre-wrap values when contract assistance is active.

In contract-assisted invoke, enum params and enum DTO fields may be passed as
the Java constant name: `"ACTIVE"`, `"DISABLED"`, etc. The plan normalizes them
to SOFA's enum object shape: `{ "@type": "com.foo.Status", "name": "ACTIVE" }`.
In trusted mode, use that canonical object shape yourself when no contract can
identify the enum field.

Trusted mode, for no contract or exact user-supplied Java shape:

```json
{
  "service": "com.foo.OrderFacade",
  "method": "query",
  "types": ["com.foo.OrderQueryRequest"],
  "args": [{ "@type": "com.foo.OrderQueryRequest", "orderId": 42 }],
  "directUrl": "bolt://host:12200"
}
```

Use per-call target/version/timeout fields only when the user wants an override.
The executable mainline is direct + BOLT + Hessian2; `registryAddress` is
inspectable but not executable.

Prefer inline JSON `args`. Use `args: "@payloads/order-query.json"` only for
large/reusable payloads or a user-provided file. `@file` is rooted at
`SOFARPC_ARGS_FILE_ROOT` or the project root.

## Replay

Same payload: `sofarpc_replay` with captured `sessionId`. Changed payload:
dry-run replay by `sessionId`, edit the returned full plan, then send it back
as `payload`. Payload replay requires `schemaVersion:
"sofarpc.invoke.plan/v1"`; unsupported versions require a fresh dry-run invoke.
If `diagnostics.sessionPlanCapture.reason == "plan-too-large"`, replay with the
returned literal plan payload.

## Error Recovery

Every failure returns `{code, message, phase, hint?}`. Treat `hint.nextTool` and
`hint.nextArgs` as machine instructions.

- `target.missing` or `target.invalid`: call `sofarpc_target` with
  `{"explain": true}`.
- `target.unreachable`, `target.connect-failed`, `runtime.timeout`, or
  `runtime.protocol-failed`: call `sofarpc_doctor`.
- `contract.method-not-found` or `runtime.serialize-failed`: call
  `sofarpc_describe`; inspect overloads, args, and `@type` shape.
- `workspace.facade-not-configured`: confirm project root or use trusted mode.
- `replay.plan-version-unsupported`: rebuild a fresh dry-run plan.
- `runtime.rejected`: report the guardrail message; do not retry blindly.

## Agent Rules

Do not bypass guardrails, pass `directUrl` every call when resolved target is
right, use `registryAddress` as executable direct invoke, call `sofarpc_open`
repeatedly in one workflow, or paraphrase errcode messages.

On success, show `result`, key diagnostics, and `sessionId`; truncate large
fields explicitly. On failure, lead with `code` and `message`, then execute or
report the hinted next tool.
