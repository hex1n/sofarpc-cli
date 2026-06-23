# sofarpc-cli

This context describes SOFARPC generic invocation concepts used by the CLI and MCP server.

## Language

**Target Profile**:
A named, selectable bundle of target endpoint, wire knobs, and invocation-context settings (for example `local`, `test`, `staging`) that lets a project hold several deployment environments at once and switch between them without overwriting. It never carries the service allowlist.
_Avoid_: Environment, env (reserved for OS environment variables), config (reserved for the on-disk file)

**Base Target Settings**:
The top-level (non-profile) target, wire, and invocation-property fields in a config file. They apply when no **Target Profile** is selected, and otherwise act as a base that the selected profile overlays.
_Avoid_: Default config, global config

**Active Target Profile**:
The single **Target Profile** in effect for a given invocation, chosen by precedence: per-call argument, then session selection, then **Default Target Profile**, then none.
_Avoid_: Current environment

**Default Target Profile**:
The **Target Profile** a project falls back to when a call names none, declared as `defaultProfile` in config (local declaration wins over shared).
_Avoid_: Fallback environment

**Gateway-Carried Invocation Context**:
Information a Java service gateway attaches to an outbound SOFARPC invocation so the downstream service can read it as invocation context.
_Avoid_: MCP client credential, method argument

**Invocation Property**:
A key-value entry in **Gateway-Carried Invocation Context** exposed to downstream Java service code for the current SOFARPC invocation.
Wire carrier: official SOFARPC request baggage, encoded as `SofaRequest.requestProps["rpc_req_baggage"]` and read with `RpcInvokeContext.getRequestBaggage(...)`.
Runtime prerequisite: the target SOFARPC provider must enable baggage (`invoke.baggage.enable`).
_Avoid_: BOLT protocol header, method argument

**Sensitive Invocation Property**:
An **Invocation Property** whose value is confidential and must not be exposed as reusable invocation documentation.
_Avoid_: Literal replay value, logged token

**Invocation Property Reference**:
A non-secret pointer to the current value of an **Invocation Property**.
_Avoid_: Captured secret value

**Masked Invocation Property**:
A higher-priority declaration that excludes an **Invocation Property** from the final **Gateway-Carried Invocation Context**.
_Avoid_: Empty string value

**Default Invocation Context**:
Gateway-carried invocation context that is prepared before a specific outbound invocation adds its per-call properties.
_Avoid_: Implicit environment scan

## Relationships

- A **Target Profile** overlays the **Base Target Settings**; a profile-level field beats the same base field.
- A **Target Profile** never carries the service allowlist; the allowlist stays a project-wide guardrail independent of the **Active Target Profile**.
- An **Active Target Profile** that is named but undefined is an error, not a fall-through to **Base Target Settings**.
- A **Gateway-Carried Invocation Context** belongs to one outbound SOFARPC invocation.
- A **Gateway-Carried Invocation Context** contains zero or more **Invocation Properties**.
- An **Invocation Property** is identified by its key within a **Gateway-Carried Invocation Context**.
- A **Masked Invocation Property** prevents lower-priority defaults for the same key from contributing to a **Gateway-Carried Invocation Context**.
- A **Default Invocation Context** contributes **Invocation Properties** to a **Gateway-Carried Invocation Context**.
- A **Sensitive Invocation Property** is an **Invocation Property**.
- A **Sensitive Invocation Property** is represented in reusable invocation documentation by an **Invocation Property Reference**.
- A **Gateway-Carried Invocation Context** is separate from MCP host authentication.

## Example Dialogue

> **Dev:** "Should the MCP client's login token be forwarded as **Gateway-Carried Invocation Context**?"
> **Domain expert:** "No. Only the context that the Java service gateway would carry to the downstream SOFARPC service belongs there."
> **Dev:** "Should an **Invocation Property** be modeled as a normal facade method parameter?"
> **Domain expert:** "No. It is part of invocation context, not part of the service method signature."
> **Dev:** "Can a **Sensitive Invocation Property** appear in a reusable replay plan?"
> **Domain expert:** "Only as a redacted reference, never as the literal secret value."
> **Dev:** "When replaying a call, should the old secret value be reused?"
> **Domain expert:** "No. The **Invocation Property Reference** points to the current value at replay time."
> **Dev:** "If an **Invocation Property Reference** cannot be resolved, should the call continue without that property?"
> **Domain expert:** "No. A missing reference means the invocation context is incomplete, so the invocation should not be sent."
> **Dev:** "If an **Invocation Property Reference** resolves to an empty value, is that still resolved?"
> **Domain expert:** "No. A reference with an empty current value is unresolved, while a literal empty **Invocation Property** value remains explicit."
> **Dev:** "Can **Default Invocation Context** be discovered by scanning all environment variables?"
> **Domain expert:** "No. Defaults must be explicit; environment variables only matter when an **Invocation Property Reference** names one."
> **Dev:** "If two sources define the same **Invocation Property** key, should their details be merged?"
> **Domain expert:** "No. The higher-priority property for that key is the property used for the invocation."
> **Dev:** "Should a caller send an empty string to remove a default **Invocation Property**?"
> **Domain expert:** "No. Use a **Masked Invocation Property** so the final invocation context has no property for that key."

## Flagged Ambiguities

- "environment" can mean a deployment environment selector, an OS environment variable, or the `mcp-env` config layer; resolved so far: the user-selectable deployment selector is a **Target Profile**, while "env" stays reserved for OS environment variables.
- "token" can mean MCP client authentication, a downstream invocation credential, or non-secret routing metadata; resolved so far: this discussion is about gateway-carried invocation context, not MCP client authentication.
- "header" can mean a BOLT protocol header or an **Invocation Property**; resolved so far: Java service-visible context belongs to **Invocation Properties**.
- "missing property reference" can mean an absent property or an empty property value; resolved so far: an unresolved **Invocation Property Reference** is incomplete invocation context, not an empty property.
- "empty value" can mean an explicitly provided empty **Invocation Property** value or an empty referenced value; resolved so far: explicit empty values are values, but empty referenced values are unresolved references.
- "override" can mean merging parts of a property or replacing the property for a key; resolved so far: same-key **Invocation Properties** replace as a whole.
- "unset" can mean an empty value or no property at all; resolved so far: a **Masked Invocation Property** means no final property for that key.
