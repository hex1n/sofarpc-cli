---
name: sofarpc-invoke
description: Guides agents through using the sofarpc-mcp MCP tools to plan, invoke, replay, and diagnose SOFARPC Java facade calls. Use when the user asks an agent to call, test, debug, replay, or inspect a Java facade/service through SOFARPC/BOLT, or when target, contract, payload-shape, replay, or real-invoke guardrail errors occur.
---

# sofarpc-invoke
Agent playbook for the `sofarpc-mcp` tools: `sofarpc_init_project`,
`sofarpc_open`, `sofarpc_target`, `sofarpc_describe`, `sofarpc_invoke`,
`sofarpc_replay`, and `sofarpc_doctor`.

## Operating Loop
1. If the Java project has no `.sofarpc/config*.json`, call
   `sofarpc_init_project` with `project`/`cwd` when you know the workspace.
   If no scope is available, call it with `dryRun: true` first and inspect
   `projectResolution`; retry with an explicit candidate `project` before
   writing, even when confidence is high. Pass `directUrl` or `registryAddress`
   only when the user supplied the target. Otherwise let it write discovered
   `allowedServices` and report target next steps. If discovery finds no
   services, pass explicit `services`; use `allowAllServices: true` only when
   the user intentionally allows every service.
2. Call `sofarpc_open` once for a new project/session. Keep the `sessionId`.
3. Read `capabilities` and `contract` from `sofarpc_open`.
4. If `capabilities.describe == true`, call `sofarpc_describe` with
   `sessionId`, `service`, and `method`; reuse the returned `types`.
5. Call `sofarpc_invoke` with `dryRun: true` unless the user explicitly asked
   to send a real request. Include the `sessionId`.
6. Inspect `plan.target`, `plan.paramTypes`, `plan.args`, and `contractSource`.
7. For real calls, invoke without `dryRun` only after the plan matches intent.
8. On failure, follow `hint.nextTool` / `hint.nextArgs` before guessing.

## Preconditions
- Server registered: `sofarpc-mcp setup --scope=user`.
- Target resolution: per-call input, `.sofarpc/config.local.json`,
  `.sofarpc/config.json`, defaults. Project config must not set `mode`.
- Contract data is loaded lazily per resolved project root. `project` / `cwd`
  selects a project explicitly; otherwise `sessionId` selects the project opened
  by `sofarpc_open`.
- If no contract is available, use trusted mode. If a stale contract cannot
  resolve a complete user-supplied tuple, use `contractMode: "trusted"` or
  `trusted: true`. Use `contractMode: "strict"` only when falling back would be
  unsafe.
- Real calls require `SOFARPC_ALLOW_INVOKE=true` and explicit project
  `.sofarpc/config*.json` `allowedServices`; missing allowlists block invoke.
  Per-call `directUrl` overrides require `SOFARPC_ALLOW_TARGET_OVERRIDE=true`.

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
  "directUrl": "bolt://host:12200",
  "contractMode": "trusted"
}
```

Use per-call target/version/timeout fields only when the user wants an override.
The executable mainline is direct + BOLT + Hessian2; `registryAddress` is
inspectable but not executable.

Always send `args` as an inline JSON array. Single-parameter methods still use
a one-item array.

## Replay

Same payload: `sofarpc_replay` with captured `sessionId`. Changed payload:
dry-run replay by `sessionId`, edit the returned full plan, then send it back
as `payload` together with the same `sessionId` so replay policy uses the
session project context. Payload replay requires `schemaVersion:
"sofarpc.invoke.plan/v1"`; unsupported versions require a fresh dry-run invoke.
If `diagnostics.sessionPlanCapture.reason == "plan-too-large"`, replay with the
returned literal plan payload.

## Error Recovery

Every failure returns `{code, message, phase, hint?}`. Treat `hint.nextTool` and
`hint.nextArgs` as machine instructions.

- `target.missing` or `target.invalid`: call `sofarpc_target` with
  `{"explain": true}`. If no project config exists, call
  `sofarpc_init_project`; if project scope is unclear, start with
  `dryRun: true` and inspect `projectResolution`. Do not guess directUrl or
  registryAddress.
- `target.unreachable`, `target.connect-failed`, `runtime.timeout`, or
  `runtime.protocol-failed`: call `sofarpc_doctor`.
- `contract.method-not-found` or `runtime.serialize-failed`: call
  `sofarpc_describe`; inspect overloads, args, and `@type` shape.
- `workspace.facade-not-configured`: confirm `projectRoot` / `contractRoot` in
  `sofarpc_open` or `sofarpc_doctor`, then use trusted mode only with a complete
  service/method/types/args tuple.
- `replay.plan-version-unsupported`: rebuild a fresh dry-run plan.
- `runtime.rejected`: report the guardrail message; do not retry blindly.

## Agent Rules

Do not bypass guardrails, pass `directUrl` every call when resolved target is
right, use `registryAddress` as executable direct invoke, call `sofarpc_open`
repeatedly in one workflow, or paraphrase errcode messages.

On success, show `result`, key diagnostics, and `sessionId`; truncate large
fields explicitly. On failure, lead with `code` and `message`, then execute or
report the hinted next tool.
