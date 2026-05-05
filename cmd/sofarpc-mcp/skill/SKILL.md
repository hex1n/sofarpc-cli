---
name: sofarpc-invoke
description: Invoke, dry-run, replay, or diagnose a SOFARPC Java facade method through the sofarpc-mcp generic-invoke MCP surface. Use when the user asks to call, test, invoke, debug, plan, replay, or inspect a Java facade/service through SOFARPC/BOLT, including target, contract, connectivity, payload-shape, and real-invoke guardrail failures. Covers sofarpc_open, sofarpc_target, sofarpc_describe, sofarpc_invoke, sofarpc_replay, sofarpc_doctor.
---

# sofarpc-invoke

Driver for the six-tool `sofarpc-mcp` surface. The binary does one thing — **one BOLT request, one SofaResponse back** — and the tools wrap that loop with workspace, contract, and diagnostics.

## Preconditions (verify once)

- `sofarpc-mcp` is registered with the MCP client (`sofarpc-mcp setup` does this).
- `SOFARPC_PROJECT_ROOT` is set on the server entry **or** the current CWD is the Java project; the server scans `.java` files from that root to power `describe`.
- Target reachability: either `SOFARPC_DIRECT_URL` is on the server env, or the user supplies `directUrl` at invoke time.
- Real network calls are disabled unless the server env has `SOFARPC_ALLOW_INVOKE=true`. `dryRun=true` always works and should be the default first step unless the user explicitly asks to send the request.
- Non-dry-run direct calls default to the MCP server env `SOFARPC_DIRECT_URL`. Per-call `directUrl` overrides and literal replay payload targets require `SOFARPC_ALLOW_TARGET_OVERRIDE=true`; `SOFARPC_ALLOWED_TARGET_HOSTS` can further restrict host or host:port values.
- Prefer inline JSON `args`. Use `args: "@path"` only when the user supplied a file, the payload is too large to edit inline, or the same payload must be reused. `@file` must resolve inside `SOFARPC_ARGS_FILE_ROOT` when set, otherwise inside `SOFARPC_PROJECT_ROOT`; files outside that root or over `SOFARPC_ARGS_FILE_MAX_BYTES` (default 1 MiB) are rejected.
- Session replay by `sessionId` retains only plans up to `SOFARPC_SESSION_PLAN_MAX_BYTES` (default 1 MiB). Oversized plans are still returned by `sofarpc_invoke`; replay them by passing the plan as a literal `payload`.
- Direct BOLT response bodies are capped by `SOFARPC_MAX_RESPONSE_BYTES` (default 16 MiB) before allocation and Hessian decoding.

If the user is on a brand-new checkout and these aren't set, do not guess — run `sofarpc_doctor` and fix in order.

## Golden path

1. **`sofarpc_open`** — first call of any new session. Returns `sessionId`, the resolved `target`, a `capabilities` banner (`{directInvoke, describe, replay}`), and a `contract` banner (`attached`, `indexedClasses`, `loadError`). Read the banner:
   - `capabilities.describe == false` → no Java contract attached; skip step 2, use trusted-mode invoke.
   - `contract.loadError != ""` → tell the user what the load error says; don't paper over it.

2. **`sofarpc_describe`** *(only if `capabilities.describe == true`)* — pass `service` and `method`. Returns matching overloads and a JSON skeleton for args. Pass the user-visible `types` array back on the invoke; that disambiguates overloads.

3. **`sofarpc_invoke` dry-run** — unless the user explicitly requested a real network call, set `dryRun: true` first. Inspect `plan.target`, `plan.paramTypes`, normalized `plan.args`, and `contractSource`.

4. **Real `sofarpc_invoke`** — only after the plan looks correct and the user requested execution. If it is rejected because real invoke is disabled, report the exact `runtime.rejected` message and tell the user to set `SOFARPC_ALLOW_INVOKE=true` on the MCP server entry; do not retry.

5. **`sofarpc_replay`** — for "run it again" or "try with the same args"; use the `sessionId` returned from a prior invoke when the plan was captured. If `diagnostics.sessionPlanCapture.reason == "plan-too-large"`, replay with the returned plan as `payload` instead.

6. **`sofarpc_doctor`** — run this **before** guessing when `invoke` fails with anything other than a user-code error.

## `sofarpc_invoke` — two shapes

**Contract-assisted** (preferred; contract attached):

```json
{
  "service": "com.foo.OrderFacade",
  "method": "query",
  "types": ["com.foo.OrderQueryRequest"],
  "args": [{ "orderId": 42, "includeItems": true }],
  "sessionId": "ws_..."
}
```

The contract layer upgrades DTOs to `{"@type":"..."}` and wraps `BigDecimal`/`BigInteger`/nested `List<DTO>`/`Map<String,V>` recursively. Do not pre-wrap — send plain JSON the user would naturally write.

**Trusted mode** (no contract or the user asserts the exact Java shape):

```json
{
  "service": "com.foo.OrderFacade",
  "method": "query",
  "types": ["com.foo.OrderQueryRequest"],
  "args": [{ "@type": "com.foo.OrderQueryRequest", "orderId": 42 }],
  "directUrl": "bolt://host:12200"
}
```

In trusted mode the caller owns the exact payload shape; `@type` tags are required for user-defined objects. No overload resolution, no skeleton.

**Per-call overrides** (use only when different from MCP env):

- `directUrl`, `registryAddress`, `registryProtocol` — target override
- `version` — SOFA service version (default from env/defaults)
- `targetAppName` — direct-transport target app header; sometimes required by servers that enforce it
- `timeoutMs` — bump for slow methods
- `dryRun: true` — return the built plan without sending a BOLT request; use this when the user asks "what would this call look like"

Dry-run plans include `schemaVersion`. The current replayable schema is `sofarpc.invoke.plan/v1`.

If `sessionId` is present, the server also tries to capture the plan in memory for `sofarpc_replay` by session id. Plans larger than `SOFARPC_SESSION_PLAN_MAX_BYTES` are not retained; the invoke still succeeds and returns the full plan. In that case use literal `payload` replay.

**Real invoke guardrails**:

- Non-dry-run calls require `SOFARPC_ALLOW_INVOKE=true` in the MCP server env.
- `SOFARPC_ALLOWED_SERVICES` optionally restricts service FQNs; `*` allows all.
- Per-call target overrides require `SOFARPC_ALLOW_TARGET_OVERRIDE=true`; `SOFARPC_ALLOWED_TARGET_HOSTS` optionally restricts direct target host or host:port values.
- `SOFARPC_MAX_RESPONSE_BYTES` bounds the direct BOLT response body before decode.
- `registryAddress` is inspectable, but the executable mainline is direct + BOLT + Hessian2; use `directUrl` for real invoke.

**Args input preference**:

- Default to inline JSON because it keeps the call self-contained and visible in dry-run output.
- Use `@file` only for large or reusable payloads, or when the user explicitly provides a path.

Fallback file shape:

```json
{
  "service": "com.foo.OrderFacade",
  "method": "query",
  "types": ["com.foo.OrderQueryRequest"],
  "args": "@payloads/order-query.json",
  "dryRun": true
}
```

`@file` JSON is rooted at `SOFARPC_ARGS_FILE_ROOT` or `SOFARPC_PROJECT_ROOT`, symlinks are resolved, and the default max size is 1 MiB. On `input.args-invalid`, report the exact path/size/root message; do not switch to `@file` just to avoid fixing an invalid inline payload.

## Error recovery protocol

Every failure returns `{code, message, phase, hint?}`. The `hint.nextTool` / `hint.nextArgs` pair is a **machine instruction**, not a suggestion. Follow it literally before re-deriving.

Common codes and the expected response:

| code | meaning | your next move |
| --- | --- | --- |
| `target.missing` | no layer supplied `mode` | call `sofarpc_target` with `{"explain": true}` to see what's set; ask user for `directUrl` or registry |
| `target.invalid` | direct/registry address cannot be parsed | call `sofarpc_target` with `{"explain": true}` and fix the address shape |
| `target.unreachable` | TCP probe failed | call `sofarpc_doctor`; likely wrong host, VPN down, or firewall |
| `target.connect-failed` | direct invoke could not connect | call `sofarpc_target` with `{"explain": true}` and report `diagnostics.dialTarget` if present |
| `contract.method-not-found` | overload resolution failed | call `sofarpc_describe` to list overloads; ask user which signature |
| `workspace.facade-not-configured` | contract needed but store missing | confirm `SOFARPC_PROJECT_ROOT`; fall back to trusted mode with user-supplied `types` |
| `replay.plan-version-unsupported` | replay payload/session plan is missing or has an unsupported `schemaVersion` | run `sofarpc_invoke` with `dryRun=true` again and replay the fresh plan |
| `runtime.serialize-failed` | request payload could not be encoded | call `sofarpc_describe`; inspect `paramTypes`, normalized `plan.args`, and `@type` shape |
| `runtime.deserialize-failed` | Hessian decode crashed on the response | surface the diagnostics block verbatim; don't retry |
| `runtime.timeout` | direct invoke timed out | call `sofarpc_doctor`; consider `timeoutMs` only after target reachability is proven |
| `runtime.protocol-failed` | BOLT/SOFARPC exchange failed outside connect/serialize/decode buckets | call `sofarpc_doctor`; surface diagnostics verbatim |
| `runtime.rejected` | local guardrail or remote server refused | if message mentions `SOFARPC_ALLOW_INVOKE`, stop and ask user to enable it; otherwise report `responseStatus` and `responseClass` from diagnostics |

Do not prompt the user to fix something `sofarpc_doctor` has not yet diagnosed — doctor is cheap, prose is expensive.

## Replay

When the user says "run it again" or "try with X changed":

- Same payload: `sofarpc_replay` with the `sessionId` only when the prior invoke captured the plan. No args in the body.
- Changed subset: first call `sofarpc_replay` with `{"sessionId":"...","dryRun":true}` to retrieve the captured plan. Modify the full `payload` and send it back to `sofarpc_replay` — **not** a new `sofarpc_invoke` (replay skips plan building, so diagnostics line up with the first call).
- Payload replay requires `schemaVersion: "sofarpc.invoke.plan/v1"`. If replay returns `replay.plan-version-unsupported`, do not patch the version by hand; re-run `sofarpc_invoke` with `dryRun=true` and replay that fresh plan.
- If invoke returns `diagnostics.sessionPlanCapture.reason == "plan-too-large"`, session replay by id will not have a captured plan. Use the returned `plan` as literal `payload`.

## Anti-patterns

- ❌ Do not hand-construct `@type` tags when contract is attached; the contract layer does it and will get `BigDecimal` / `BigInteger` right.
- ❌ Do not retry a `runtime.rejected` or `target.unreachable` call without running `sofarpc_doctor`. The user's network state is not your guess.
- ❌ Do not bypass the real-invoke guardrail. If `SOFARPC_ALLOW_INVOKE` is not enabled, use dry-run and tell the user the server env change required.
- ❌ Do not pass a `directUrl` every call if the MCP env has one — you'll override user-level config silently. Only pass it when the user asks to target a different host.
- ❌ Do not use `registryAddress` as a substitute for executable direct invoke; the pure-Go runtime sends only direct+BOLT requests.
- ❌ Do not call `sofarpc_open` repeatedly in a session — it mints a new `sessionId` every time and invalidates replay context.
- ❌ Do not paraphrase `errcode` messages; they're stable strings and downstream tooling may match on them.

## Output to the user

After a successful invoke, show:

1. `result` (the decoded SofaResponse body) — truncate large fields, tell the user you're truncating
2. `diagnostics.requestId`, `diagnostics.dialTarget`, and `diagnostics.responseStatus` — the line they'll want to grep logs with
3. The `sessionId` — so they know what `sofarpc_replay` will target. If `diagnostics.sessionPlanCapture.reason == "plan-too-large"`, tell the user to replay with the returned plan payload instead of session id.

On failure, lead with `code` and `message`, then the `hint.nextTool` if present. The agent's job is to execute the hint, not editorialize on it.
