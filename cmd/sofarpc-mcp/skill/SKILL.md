---
name: sofarpc-invoke
description: Invoke a SOFARPC Java facade method via the sofarpc-mcp generic-invoke tool surface. Use when the user asks to call, test, invoke, or debug a Java facade/service through SOFARPC/BOLT, wants to replay a prior invoke, or asks to diagnose target/contract/connectivity problems in a SOFARPC workspace. Covers sofarpc_open, sofarpc_target, sofarpc_describe, sofarpc_invoke, sofarpc_replay, sofarpc_doctor.
---

# sofarpc-invoke

Driver for the six-tool `sofarpc-mcp` surface. The binary does one thing — **one BOLT request, one SofaResponse back** — and the tools wrap that loop with workspace, contract, and diagnostics.

## Preconditions (verify once)

- `sofarpc-mcp` is registered with the MCP client (`sofarpc-mcp setup` does this).
- `SOFARPC_PROJECT_ROOT` is set on the server entry **or** the current CWD is the Java project; the server scans `.java` files from that root to power `describe`.
- Target reachability: either `SOFARPC_DIRECT_URL` is on the server env, or the user supplies `directUrl` at invoke time.

If the user is on a brand-new checkout and these aren't set, do not guess — run `sofarpc_doctor` and fix in order.

## Golden path

1. **`sofarpc_open`** — first call of any new session. Returns `sessionId`, the resolved `target`, a `capabilities` banner (`{directInvoke, describe, replay}`), and a `contract` banner (`attached`, `indexedClasses`, `loadError`). Read the banner:
   - `capabilities.describe == false` → no Java contract attached; skip step 2, use trusted-mode invoke.
   - `contract.loadError != ""` → tell the user what the load error says; don't paper over it.

2. **`sofarpc_describe`** *(only if `capabilities.describe == true`)* — pass `service` and `method`. Returns matching overloads and a JSON skeleton for args. Pass the user-visible `types` array back on the invoke; that disambiguates overloads.

3. **`sofarpc_invoke`** — the call. See shapes below.

4. **`sofarpc_replay`** — for "run it again" or "try with the same args"; use the `sessionId` returned from a prior invoke. Don't rebuild args by hand for a re-run.

5. **`sofarpc_doctor`** — run this **before** guessing when `invoke` fails with anything other than a user-code error.

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

## Error recovery protocol

Every failure returns `{code, message, phase, hint?}`. The `hint.nextTool` / `hint.nextArgs` pair is a **machine instruction**, not a suggestion. Follow it literally before re-deriving.

Common codes and the expected response:

| code | meaning | your next move |
| --- | --- | --- |
| `target.missing` | no layer supplied `mode` | call `sofarpc_target` with `{"explain": true}` to see what's set; ask user for `directUrl` or registry |
| `target.unreachable` | TCP probe failed | call `sofarpc_doctor`; likely wrong host, VPN down, or firewall |
| `contract.method-not-found` | overload resolution failed | call `sofarpc_describe` to list overloads; ask user which signature |
| `workspace.facade-not-configured` | contract needed but store missing | confirm `SOFARPC_PROJECT_ROOT`; fall back to trusted mode with user-supplied `types` |
| `runtime.deserialize-failed` | Hessian decode crashed on the response | surface the diagnostics block verbatim; don't retry |
| `runtime.rejected` | server refused | report `responseStatus` and `responseClass` from diagnostics; don't retry blindly |

Do not prompt the user to fix something `sofarpc_doctor` has not yet diagnosed — doctor is cheap, prose is expensive.

## Replay

When the user says "run it again" or "try with X changed":

- Same payload: `sofarpc_replay` with the `sessionId`. No args in the body.
- Changed subset: fetch the captured plan, diff, and send `sofarpc_replay` with a full `payload` containing the modified plan — **not** a new `sofarpc_invoke` (replay skips plan building, so the diagnostics line up with the first call).

## Anti-patterns

- ❌ Do not hand-construct `@type` tags when contract is attached; the contract layer does it and will get `BigDecimal` / `BigInteger` right.
- ❌ Do not retry a `runtime.rejected` or `target.unreachable` call without running `sofarpc_doctor`. The user's network state is not your guess.
- ❌ Do not pass a `directUrl` every call if the MCP env has one — you'll override user-level config silently. Only pass it when the user asks to target a different host.
- ❌ Do not call `sofarpc_open` repeatedly in a session — it mints a new `sessionId` every time and invalidates replay context.
- ❌ Do not paraphrase `errcode` messages; they're stable strings and downstream tooling may match on them.

## Output to the user

After a successful invoke, show:

1. `result` (the decoded SofaResponse body) — truncate large fields, tell the user you're truncating
2. `diagnostics.requestId` and `diagnostics.responseStatus` — the one line they'll want to grep logs with
3. The `sessionId` — so they know what `sofarpc_replay` will target

On failure, lead with `code` and `message`, then the `hint.nextTool` if present. The agent's job is to execute the hint, not editorialize on it.
