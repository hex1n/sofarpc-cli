# ADR 0003: Target Profiles for Per-Environment Configuration

Date: 2026-06-22

Status: accepted

## Context

A project's `.sofarpc` config has two files — `config.json` (shared, committed)
and `config.local.json` (local, gitignored) — merged in one fixed precedence
chain (`input > project-local > project > mcp-env > defaults`). That pair is a
*visibility* axis (committed vs personal), not a *deployment-environment* axis.
A user who configures their `local` environment and then wants to call the
`test` environment has only one place to put a target, so they must overwrite
the single local config to switch. There is no way to hold several environments
at once and select between them.

The words "environment" and "env" are already overloaded in this codebase: the
`mcp-env` config layer, the `invocationProperties[].env` OS-variable reference,
and the file suffix `.local`. A new "switch by environment" concept needs a name
that does not collide with any of these.

## Decision

Introduce a **Target Profile**: a named, selectable bundle of target endpoint,
wire knobs, and `invocationProperties`. "Profile" is the canonical term;
"environment"/"env" stays reserved for OS environment variables.

**Scope.** A profile overrides endpoint (`directUrl`/`registryAddress`), wire
knobs (`protocol`/`serialization`/`uniqueId`/timeouts), and
`invocationProperties`. It does **not** carry `allowedServices`: the service
allowlist stays a project-wide safety guardrail that does not change when the
profile changes.

**File shape.** Both `config.json` and `config.local.json` gain a `profiles`
map. The two axes stay orthogonal: a profile can have a team-shared definition
in `config.json` and a personal override in `config.local.json`. Each profile
object is target+wire+`invocationProperties` only; `allowedServices` inside a
profile is rejected (enforced by the type). Files with no `profiles` key behave
exactly as before.

**Base + overlay.** The top-level (non-profile) fields are **Base Target
Settings**: they apply when no profile is selected, and otherwise act as a base
that the selected profile overlays. Resolution for a selected profile `P`:

```text
input
  > project-local:profiles[P]
  > project:profiles[P]
  > project-local (base)
  > project (base)
  > mcp-env
  > defaults
```

Profile layers sit **above** base layers deliberately: if a personal top-level
`directUrl` outranked the team's `profiles[test]`, selecting `test` would
silently do nothing. `invocationProperties` merge per key using the existing
`invocationprops.Merge`, so a profile can override or `unset` a base property.

**Selection.** The **Active Target Profile** is chosen by precedence:

```text
per-call `profile` argument > session profile (sofarpc_open) > config `defaultProfile` > none (base only)
```

`defaultProfile` is declared in config (local declaration wins over shared).
There is no hidden, stateful "active profile" pointer — selection is always
explicit or declarative so resolution stays reproducible.

To switch the persisted default without hand-editing JSON there are two
surfaces, both of which write a plain `defaultProfile` field (automation of a
declarative edit, not a hidden pointer — the state lives in the config file, is
inspectable, and follows the same visibility rules as the rest of the file):

- `profile use <name>` is a *personal* selection: it always writes
  `defaultProfile` into `config.local.json`, after verifying the profile is
  defined in either file (an undefined name errors rather than persisting a
  default that only resurfaces as a hard error at resolve time).
- `setup --scope=project --profile <name> --set-default` records the default in
  the *same file it just wrote the profile to* — `--shared` makes a team-wide
  default, `--local` a personal one. This is consistent with `defaultProfile`
  living in both files (local wins over shared); forcing it to `config.local.json`
  even for a `--shared` profile would split one declaration across two files and
  hide a team default from the team.

Both keep resolution reproducible and remove the "must hand-edit to switch"
residue.

**Undefined profile is an error.** A `profile` that is named but defined in
neither file fails with an error listing the available profiles. It never falls
through to base. Omitting `profile` entirely is the legitimate base path and is
not an error.

**Creation reuses the existing surface.** `sofarpc_init_project` and
`setup --scope=project` gain a `profile` argument; when set, the target/wire
fields are written into `profiles[name]` and merged into the existing file
(every other field — base settings, other profiles, an explicit
`allowedServices: []` — is preserved; only that profile key changes). Because a
profile never carries `allowedServices`, profile mode *rejects* the
service-allowlist inputs (`services` / `allowAllServices` / `serviceNameSuffixes`
/ `--allowed-services`) rather than silently ignoring them — the allowlist is a
base, project-wide write. `force` is needed only to overwrite an existing
same-named profile; adding a new profile to an existing file is not destructive.

**Discoverability.** `sofarpc_open` / `sofarpc_target` / `sofarpc_doctor` return
`availableProfiles` and `activeProfile`. The `explain` trace layer names become
profile-aware (e.g. `project-local:profiles[test]` vs `project-local`) so a
reader can see whether a profile or the base won each field.

**Replay.** A captured plan records the profile name as informational
provenance only. This is an **additive field on the existing
`sofarpc.invoke.plan/v2` schema** (`profile`, `omitempty`), not a new schema
version: replay ignores it, older plans without it stay valid, and the field is
purely a label. Replay still uses the frozen target and re-resolves redacted
`env` references at replay time; it does not re-resolve through the profile, so
plans cannot drift if a profile is later edited (consistent with ADR 0002).

## Considered Options

- **A profile lives wholly in one file** (no cross-visibility merge). Rejected:
  the team could define `test` but you could not override one field of it
  locally, losing the value of the shared/local split.
- **Profiles only in `config.local.json`.** Rejected: the team could not share
  named environment definitions.
- **Top-level = an implicit default profile that named profiles fully replace**
  (no base inheritance). Rejected: shared `invocationProperties` and wire knobs
  would have to be repeated in every profile.
- **kubectl-style stateful `profile use`** writing a persisted active-profile
  pointer. Rejected: hidden mutable state makes it impossible for an agent (or
  the user later) to tell why an invoke targeted a given environment, fighting
  the project's reproducibility stance.
- **Per-profile `allowedServices`.** Rejected: the allowlist is a safety
  guardrail and should not silently widen or narrow when switching environments.
- **Silent fallback to base for an unknown profile.** Rejected: a typo could
  send an RPC to the wrong environment.

## Consequences

- Users can hold `local`, `test`, `staging`, … at once and switch per call,
  per session, or via `defaultProfile` without overwriting any config.
- The config file format gains a `profiles` map and a `defaultProfile` field;
  old files keep working unchanged.
- A profile named `local` is distinct from the file `config.local.json`; docs
  and the glossary must keep the two axes (visibility vs profile) clear.
- The resolver gains profile-aware layers; `explain` output and the layer set
  grow accordingly.
- `sofarpc_init_project` / `setup` gain merge-into-existing semantics for the
  profile path, a departure from their current whole-file write.
