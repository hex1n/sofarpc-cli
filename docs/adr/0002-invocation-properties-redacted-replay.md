# ADR 0002: Invocation Properties Use Redacted Replay

Date: 2026-06-04

Status: accepted

## Context

`sofarpc-cli` must support gateway-carried invocation context for SOFARPC calls
so downstream Java services can read token-like or routing-like information
from SOFA invocation properties. Some of those properties are sensitive, while
MCP dry-run plans, session resources, logs, and replay payloads are designed to
be inspectable and reusable by agents.

## Decision

Model gateway-carried invocation context as explicit `invocationProperties`
that are encoded as SOFARPC request baggage, not business method arguments,
flat SOFA request properties, or generic BOLT protocol headers.

On the wire, resolved invocation properties are carried in the official
SOFARPC baggage request property:

```text
SofaRequest.requestProps["rpc_req_baggage"] = Map<String, String>
```

This is the request-side shape consumed by SOFARPC's provider baggage filter
and exposed to downstream Java service code through
`RpcInvokeContext.getRequestBaggage(...)`.

`invocationProperties` declarations are keyed by property name. Each key may
declare exactly one of:

- `value`: a string literal that may appear in dry-run plans.
- `env`: a string environment-variable reference that is redacted in plans and
  resolved only immediately before real invoke or replay execution.
- `unset`: a mask that removes a lower-priority default property for that key.

`unset` is a merge-time declaration only. Once the final effective invocation
context is computed, masked properties are omitted from v2 replay plans.

The first implementation uses explicit source precedence:

```text
per-call input > .sofarpc/config.local.json > .sofarpc/config.json
```

Environment variables are not an implicit invocation-property layer. They are
read only when an explicit `env` reference names one.

Dry-run validates declarations, merge behavior, and masks, but does not
resolve `env` references. Real invoke and replay fail before sending a SOFARPC
request when an `env` reference is missing or resolves to an empty string. A
literal `value: ""` remains an explicit empty string.

Replay plans use a new `schemaVersion`:

```text
sofarpc.invoke.plan/v2
```

Version 2 plans store only the final effective redacted invocation-property
declarations needed for replay. They do not store full layer provenance, masked
lower-priority values, or resolved secret values. Replay uses the captured
effective declarations and re-resolves `env` references at execution time; it
does not re-merge current project config. New dry-runs produce only v2 plans,
and replay rejects v1 payloads instead of maintaining dual replay semantics.

Fixed SOFA generic request properties are authored by the wire encoder, not by
`invocationProperties`. User-declared invocation-property keys are baggage map
keys, not top-level SOFA request-property keys, so keys such as `type`,
`generic.revise`, or `sofa_head_*` are not rejected solely because they look
like SOFA framework request properties. Empty keys and keys that duplicate
after trimming are still invalid.

The direct wire path writes the framework-owned `rpc_req_baggage` key itself.
User declarations do not control that top-level request-property key and the
implementation does not rely on `rpc_req_baggage.*` BOLT header prefix folding,
because that header folding behavior is version-sensitive across SOFARPC 5.x.

Java service visibility also depends on the target SOFARPC runtime enabling
request baggage (`invoke.baggage.enable`). `sofarpc_doctor` can validate local
declarations and env references, but it cannot prove a remote provider has
that runtime setting enabled.

`sofarpc_doctor` will include an `invocation-properties` diagnostic that
merges the relevant declarations and checks `env` references for presence
without exposing values.

## Considered Options

- Store resolved secret values in the plan for exact replay. Rejected because
  dry-run plans, session resources, logs, and replay payloads are reusable
  inspection surfaces.
- Re-merge current project config during replay. Rejected because the same
  replay payload would silently change behavior as config changes.
- Keep v1 replay compatibility. Rejected because the project is still evolving,
  and v2 redacted replay is a cleaner semantic break than carrying two replay
  models.
- Allow arbitrary JSON values in invocation properties. Rejected for the first
  implementation because string-only values match the expected gateway context
  shape and keep Hessian compatibility and redaction rules narrow.
- Carry invocation properties as flat top-level request properties. Rejected
  because Java business code using the standard
  `RpcInvokeContext.getRequestBaggage(...)` path would not reliably see those
  keys.
- Depend on `rpc_req_baggage.*` BOLT headers. Rejected because official 5.x
  tags differ in whether BOLT header prefix folding materializes baggage into
  requestProps.
- Reject baggage keys such as `sofa_*` by default. Rejected because baggage
  keys are business-context keys inside `rpc_req_baggage`, not top-level SOFA
  framework request properties.

## Consequences

- Agents can inspect, capture, and replay invocation plans without receiving
  secret property values.
- Downstream Java services can read invocation properties through the standard
  `RpcInvokeContext.getRequestBaggage(...)` API when provider baggage is
  enabled.
- Replay is stable with respect to the captured call shape but still picks up
  current secret values through explicit references.
- Users must regenerate dry-run plans when they want current project defaults
  to affect invocation properties.
- Existing v1 replay payloads must be regenerated as v2 plans.
