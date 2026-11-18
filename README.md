# sofarpc-cli

Agent-first local MCP server for SOFARPC generic invoke.

- Design: [docs/architecture.md](./docs/architecture.md)
- Mainline: pure-Go `direct + bolt + hessian2`
- Entry point: `cmd/sofarpc-mcp`

## MCP tools

| Tool | Purpose |
| --- | --- |
| `sofarpc_init_project` | Initialize `.sofarpc/config*.json` with target settings for a Java project. |
| `sofarpc_open` | Open a workspace. Returns project root, resolved target, capabilities, and a session id. |
| `sofarpc_target` | Resolve the effective target and probe reachability. |
| `sofarpc_describe` | Resolve overloads and build a JSON skeleton when contract information is available. Omit `method` to list a service's callable methods; omit `service` too to list known services. |
| `sofarpc_invoke` | Build and execute an invocation plan. `dryRun=true` returns the plan and local preflight checks. |
| `sofarpc_replay` | Re-run a captured plan from `sessionId` (optionally a specific `planId` from the session's plan history) or a literal `payload` with project/session safety context. |
| `sofarpc_doctor` | Run structured diagnostics across target, workspace state, and invoke prerequisites. |

Every failure returns a stable `errcode.Code` and may include a structured
`nextTool` hint. Agents are expected to follow that hint directly instead of
re-deriving the next action from prose.

## MCP prompts

The server also exposes a small prompt surface for user-invoked workflow
shortcuts. Prompts return instructions for using the tools; they do not execute
SOFARPC calls themselves.

| Prompt | Purpose |
| --- | --- |
| `sofarpc_bootstrap_project` | Guide safe first-time `.sofarpc/config*.json` setup. |
| `sofarpc_dry_run_facade_call` | Guide `open -> describe -> invoke dryRun=true` for a facade method, including optional args and `invocationProperties`. |
| `sofarpc_diagnose_failure` | Guide structured errcode recovery with `target`, `doctor`, `describe`, `replay`, and session/plan resources. |

## Install

Fresh machine, no Java runtime required:

```sh
go install github.com/hex1n/sofarpc-cli/cmd/sofarpc-mcp@latest
```

Go's `@latest` selects the latest eligible module version. Use `@main` to
select the current branch revision explicitly.

```sh
go install github.com/hex1n/sofarpc-cli/cmd/sofarpc-mcp@main
```

Pin a published tag or commit for repeatable installs. Use
`sofarpc-mcp version -json` to inspect the installed binary.

Repo-local helper scripts:

```sh
./scripts/install.sh
```

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\install.ps1
```

MCPB one-click bundles can be built for macOS, Linux, and Windows without
requiring users to install Go:

```sh
VERSION=v0.1.0 MCPB_VERSION=0.1.0 bash scripts/build-mcpb.sh
```

The default target matrix is:

```text
darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64
```

Artifacts are written to `dist/mcpb/*.mcpb`. Each bundle contains a single
platform-specific binary under `server/` and a MCPB `manifest.json`; Windows
artifacts contain `server/sofarpc-mcp.exe`. Each bundle includes `LICENSE`. The manifest exposes safe user
configuration for real-invoke enablement, target override, allowed target
hosts, session plan retention, and max response size. The manual GitHub
Actions `mcpb` workflow runs the same script and uploads the generated bundles.

Setup is split into two scopes:

- User scope registers the MCP server with Claude Code and Codex and installs
  the `sofarpc-invoke` skill for the current user. It keeps only global
  execution guardrails and removes stale project-specific SOFARPC env values.
- Project scope writes target defaults into the Java
  project under `.sofarpc/`.

User setup is the default. It is idempotent and merges the existing sofarpc env
block by default, so re-running with one new flag does not drop manually added
guardrails:

```sh
sofarpc-mcp setup --scope=user                                      # both clients + skill
sofarpc-mcp setup --claude-code                                     # Claude Code only
sofarpc-mcp setup --codex                                           # Codex only
sofarpc-mcp setup --install-skill=false                             # MCP config only
sofarpc-mcp setup --allow-invoke --allowed-target-hosts=127.0.0.1   # global real-call guardrails
sofarpc-mcp setup --dry-run --allow-invoke                          # preview user config
```

When running from source with `go run`, use `--command /abs/path/to/sofarpc-mcp`
or build/install first; setup refuses to register Go's temporary build-cache
binary.

Project setup writes the target config that travels with a repository or stays
local to a checkout:

```sh
sofarpc-mcp setup --scope=project --project-root . --local \
  --direct-url=bolt://dev-rpc.example.com:12200 --timeout-ms=3000

sofarpc-mcp setup --scope=project --project-root . --shared \
  --direct-url=bolt://dev-rpc.example.com:12200 --protocol=bolt
```

`--local` writes `.sofarpc/config.local.json` and ensures that path is ignored
by the project's `.gitignore`. `--shared` writes `.sofarpc/config.json`.
Existing project config files are not overwritten unless `--force` is passed.
CLI and MCP config writes share a lock for each project and OS user when
they use the same OS cache directory.
Real-invoke enablement and host guardrails such as `--allow-invoke` and
`--allowed-target-hosts` are user-scope env settings and are rejected in project
setup. Target fields are project-scoped and are rejected in user setup.

Agents can do the same first-project bootstrap through MCP:

```json
{
  "project": "/abs/path/to/java-project",
  "config": "local",
  "directUrl": "bolt://dev-rpc.example.com:12200"
}
```

Call that as `sofarpc_init_project`. Agents should pass `project` or `cwd`
when they know the workspace root. If they omit project scope, the tool only
returns safe Java project auto-discovery evidence from the MCP process cwd; it
does not write files until a follow-up call passes `project`, `cwd`, or
`sessionId` explicitly. The response always includes `projectResolution` with
`confidence`, `markers`, `scanTruncated`, and `candidates`.

Pass at least one target setting to initialize project config. Supply
`directUrl` explicitly to configure a BOLT endpoint. Use `sofarpc_describe`
to discover services from Java source contracts.

The skill is baked into the binary via `//go:embed`, so a fresh
`go install` carries it — no repo checkout required. Canonical source
lives at `cmd/sofarpc-mcp/skill/SKILL.md`; the repo-level
`.claude/skills/sofarpc-invoke/SKILL.md` is a symlink to it so Claude
Code auto-discovery works when running inside this checkout too.

## Quick start

For most users the setup flow above is enough. The sections below
cover the manual paths — building from source, driving the server
without `setup`, and editing client config by hand.

### Build from source

```sh
go build -o bin/sofarpc-mcp ./cmd/sofarpc-mcp
```

Release builds can inject version metadata:

```sh
VERSION=$(git describe --tags --always --dirty)
COMMIT=$(git rev-parse --short HEAD)
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)

go build \
  -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
  -o bin/sofarpc-mcp \
  ./cmd/sofarpc-mcp
```

Inspect the installed binary:

```sh
sofarpc-mcp version
sofarpc-mcp version -json
```

### Run without client config

```sh
cd /abs/path/to/project
./bin/sofarpc-mcp
```

The server speaks stdio MCP. The project root falls back to the process CWD.
The target fields `protocol` and `serialization` default to `bolt` and
`hessian2`.

Project-level target config can live with the Java project. Shared defaults go
in `.sofarpc/config.json`; machine-local values such as `directUrl` should go
in `.sofarpc/config.local.json`:

```json
{
  "directUrl": "bolt://dev-rpc.example.com:12200",
  "protocol": "bolt",
  "serialization": "hessian2",
  "timeoutMs": 3000,
  "connectTimeoutMs": 1000,
  "uniqueId": "dev"
}
```

Do not set `mode` in project config. It is derived from the resolved
`directUrl`, which selects direct mode; lower-priority endpoint values are
ignored. The pure-Go mainline executes only direct `bolt + hessian2` targets —
`registryAddress`/`registryProtocol` are not accepted and a config carrying
either is rejected as an unknown field.

The invocation path rejects `tr://` before dialing, including when the
configured `protocol` is `bolt`. The TCP probe in `sofarpc_target` checks
port reachability separately.

Use `profile` for a new MCP invocation or `-profile` for a new CLI call to
select a target profile. MCP uses the session's profile when the call leaves it
unspecified. The project's `defaultProfile` is the final profile fallback.

```text
per-call input > local profile > shared profile > local base > shared base > defaults
```

Real network calls are disabled by default. Non-dry-run `sofarpc_invoke`
requires an explicit opt-in.

```sh
export SOFARPC_ALLOW_INVOKE=true
```

Use that only for development or test targets. User-scope env also controls
target hosts and size limits:

```sh
export SOFARPC_ALLOWED_TARGET_HOSTS=127.0.0.1,dev-rpc.example.com:12200
export SOFARPC_SESSION_PLAN_MAX_BYTES=1048576
export SOFARPC_MAX_RESPONSE_BYTES=16777216
```

Existing top-level `allowedServices` entries are accepted for compatibility
and ignored during execution. Project setup treats the old service options
(`--allowed-services`, and MCP `services`, `allowAllServices`, and
`serviceNameSuffixes`) as no-ops.

Project setup stores direct endpoints in `.sofarpc/config.local.json` or
`.sofarpc/config.json`.
Fresh calls can select a workspace or per-call Profile. A `directUrl` outside
the selected configured target requires `SOFARPC_ALLOW_TARGET_OVERRIDE=true`.
Replay uses the captured address and validates it against the current default
target or the override policy. `SOFARPC_ALLOWED_TARGET_HOSTS`,
when set, restricts real direct targets to a comma-separated list of host or
host:port values; `*` allows all hosts. `timeoutMs` bounds network I/O,
and `connectTimeoutMs` can shorten the dial phase. Invalid project target config is
reported by `sofarpc_target` / `sofarpc_doctor` and blocks real invoke until
fixed.

`SOFARPC_SESSION_PLAN_MAX_BYTES` controls the per-plan capture limit. Every
MCP process also caps retained plan JSON at 64 MiB. It evicts the oldest
plans across sessions to make room. Set the per-plan limit to `0` to use
only the aggregate limit. Plans too large to retain are still returned and
can be submitted later as a replay `payload`.
`sofarpc_doctor` reports real-invoke guardrails in its `invoke-policy` check.

`SOFARPC_MAX_RESPONSE_BYTES` controls the maximum BOLT response body accepted
by the direct transport. Invalid or non-positive values fall back to the
default 16 MiB limit.

`sofarpc-mcp` does not scan `.java` files during MCP startup. It loads
source-contract information lazily when a tool first needs the contract
store for the resolved project root, then caches that project-scoped store.
Call `sofarpc_open` with `refreshContract: true` after changing the Java
sources or to retry a failed index load.
`project` / `cwd` inputs select the project explicitly; otherwise a
`sessionId` reuses the project opened by `sofarpc_open`. Hidden directories,
test trees, and common build-output directories are skipped.

### Hand-written client config

If you prefer not to run `sofarpc-mcp setup`, drop this into the
client's MCP config (Claude Code: `~/.claude.json` → `mcpServers`;
Codex: `~/.codex/config.toml` under `[mcp_servers.sofarpc]`):

```json
{
  "mcpServers": {
    "sofarpc-project": {
      "command": "/abs/path/to/sofarpc-mcp"
    }
  }
}
```

## Typical flow

1. `sofarpc_init_project` when a Java checkout has no `.sofarpc/config*.json`;
   pass `project`/`cwd` when known, otherwise inspect `projectResolution`
2. `sofarpc_open`
3. `sofarpc_target` (optionally with `project` or `cwd` to inspect another project)
4. `sofarpc_describe` with the `sessionId` if contract information is available
5. `sofarpc_invoke` with the `sessionId`
6. `sofarpc_replay` with the `sessionId`, or `payload` plus `sessionId` for safety context
7. `sofarpc_doctor` when invoke cannot proceed

The installed [`sofarpc-invoke` skill](cmd/sofarpc-mcp/skill/SKILL.md) helps
Claude Code and Codex choose the CLI or MCP tools, check preflight results,
and follow structured error hints.

## One-shot CLI call

`sofarpc-mcp call` runs a single invocation through the same plan/execute
pipeline and guardrails as the MCP tools — useful for human smoke tests, CI
scripts, and reproducing an agent's call outside an MCP client:

```sh
# Preview the plan without touching the wire
sofarpc-mcp call -project . -service com.foo.UserFacade -method query \
  -types java.lang.String -args '["id-1"]' \
  -direct-url bolt://127.0.0.1:12200 -dry-run

# Execute for real (same opt-in and target rules as the MCP tools)
SOFARPC_ALLOW_INVOKE=true sofarpc-mcp call -project . \
  -service com.foo.UserFacade -method query \
  -types java.lang.String -args '["id-1"]'

# Replay a captured plan (bare plan JSON or invoke dry-run output)
sofarpc-mcp call -plan plan.json
```

CLI output has the same JSON fields as `sofarpc_invoke`, including `ok`,
`plan`, `preflight`, `result`, `diagnostics`, and `error` when applicable.
Real calls require `SOFARPC_ALLOW_INVOKE=true`.

When planning succeeds, CLI `-dry-run` and MCP invoke or replay with
`dryRun: true` return the plan and `preflight`. A successful plan can have `ok: true` and
`preflight.ready: false`. Read `preflight.policyError` and
`preflight.executionError` for blockers. Preflight checks local policy, plan
validity, transport compatibility, and the target address without opening
a connection. TCP reachability, baggage environment variables, and wire
encoding are checked separately.

## HTTP A/B checks

From a repository checkout, use [`scripts/http-ab.py`](scripts/http-ab.py) to
compare the same read-only HTTP request across two independently identified
deployments. The JSON scenario defines each route, credential environment
references, response selectors, and expected build IDs. `capture` sends the
requests. `compare` runs offline and checks build evidence before comparing
the selected business data. See the [configuration and commands](scripts/http-ab.md).

## `sofarpc_invoke` shape

```json
{
  "service": "com.foo.Facade",
  "method": "getUser",
  "types": ["com.foo.GetUserRequest"],
  "args": [{ "userId": 1 }],
  "version": "2.0",
  "targetAppName": "foo-app",
  "invocationProperties": {
    "tenant": { "value": "dev" },
    "authToken": { "env": "SOFARPC_AUTH_TOKEN" }
  },
  "directUrl": "bolt://host:12200",
  "sessionId": "ws_...",
  "dryRun": true
}
```

- `project` / `cwd` can explicitly select a project root; otherwise `sessionId`
  supplies the project-scoped target config and contract store.
- `version` overrides the SOFA service version for this call.
- `targetAppName` sets the direct-transport target app header.
- `invocationProperties` are gateway-carried SOFARPC request baggage. Direct
  invoke encodes them as `SofaRequest.requestProps["rpc_req_baggage"]`, so Java
  services read them with `RpcInvokeContext.getRequestBaggage(...)` when
  provider baggage is enabled (`invoke.baggage.enable`). `value` is literal;
  `env` is resolved only for real invoke/replay and stays redacted in plans.
- Transport attempts report `diagnostics.stage` and `diagnostics.requestState`.
  `request-written` means the local socket accepted the full request.
  A trace ID is available once the transport creates its SOFATracer context,
  which it places in `SofaRequest.requestProps["rpc_trace_context"]`.
  Use `diagnostics.traceId` to correlate provider logs where SOFATracer is
  enabled. MCP invoke and replay failures include the error code, phase,
  available transport details, and applicable next tool hint in the first
  text block.
- `directUrl` selects a per-call address. Without it, the target comes from
  the selected profile and project config. The MCP host's real-invoke
  environment settings apply to policy.
- Replay accepts a v2 plan, a dry-run result containing `plan`, or a
  captured plan selected by `sessionId` and optional `planId`. Literal
  payload replay may include `sessionId`, `project`, or `cwd` to select
  the current policy context.
- Real invocation requires `SOFARPC_ALLOW_INVOKE=true`. Parameterized methods
  require explicit arguments for invoke and replay. Use a new invoke with
  `args` to turn an argument template into an executable plan.
- A direct call uses the target selected by its Profile and project config.
  Per-call overrides require `SOFARPC_ALLOW_TARGET_OVERRIDE=true` and can be
  bounded with `SOFARPC_ALLOWED_TARGET_HOSTS`.
- Session capture is subject to both the per-plan and process-wide byte limits.
- Direct BOLT responses are capped by `SOFARPC_MAX_RESPONSE_BYTES` before the
  response body is allocated and decoded.

When contract information is attached, facade-backed invoke automatically
normalizes common Java shapes before the wire step:

- root and nested DTOs get `@type` injected
- `List<DTO>` / `Map<String, V>` values are normalized recursively
- `java.math.BigDecimal` / `BigInteger` values are wrapped into canonical typed
  objects
- enum constants are wrapped into SOFA's canonical enum object shape,
  `{"@type":"com.foo.Status","name":"ACTIVE"}`
- Bare integer JSON values outside JavaScript's safe-integer range are rejected
  before planning; send Java `Long` values as decimal strings or explicit
  `{"@type":"java.lang.Long","value":"<digits>"}` wrappers.

For example, a dry-run plan may turn:

```json
{
  "args": [
    {
      "amount": 1000.5
    }
  ]
}
```

into:

```json
{
  "args": [
    {
      "@type": "com.foo.GetUserRequest",
      "amount": {
        "@type": "java.math.BigDecimal",
        "value": "1000.5"
      }
    }
  ]
}
```

## Trusted mode

`sofarpc_invoke` can run without contract guidance as long as the caller
supplies:

- `service`
- `method`
- `types`
- `args`

In this mode the plan is marked `contractSource: "trusted"`. No overload
disambiguation, automatic type normalization, or skeleton generation happens.
If the remote side needs `@type`, `BigDecimal`, or other Java-specific payload
shapes, the caller must send them explicitly. Use `types: []` and `args: []`
for a zero-argument method. CLI callers use `-types= -args "[]"`.

Use `contractMode` to make contract behavior explicit:

- `auto` (default): use the project contract when available; if the contract
  cannot resolve the method and `service` / `method` / `types` / `args` are all
  supplied, fall back to trusted planning.
- `strict`: require the project contract and never fall back to trusted mode.
- `trusted`: ignore the contract and require the caller-supplied tuple.

## Testing

Default validation stays lightweight:

```sh
go vet ./...
go test -race ./...
go build ./...
```

Optional e2e smoke tests are behind the `e2e` build tag:

```sh
go test -tags=e2e ./tests/e2e/...
```

The current e2e smoke starts a local fake BOLT server and exercises the real
`invoke.Execute -> sofarpcwire -> boltclient` path. It does not require Java or
an external SOFARPC service. The GitHub Actions `e2e` workflow is manual
(`workflow_dispatch`) so default CI remains fast.

SOFARPC/Hessian wire compatibility is guarded by committed fixtures under
`internal/sofarpcwire/testdata/golden`. Default Go tests consume those fixtures
without Java. To regenerate and verify the Java-backed baseline locally:

```sh
bash scripts/verify-wire-fixtures.sh
```

To check runtime compatibility without changing committed fixtures:

```sh
bash scripts/verify-wire-fixtures.sh --matrix 5.4.0,5.7.6,5.8.0
```

Pull requests that change the wire or contract code run the Java baseline
automatically. Before release, run the full GitHub Actions `wire-fixtures` workflow; it uses
the same script to regenerate the baseline Java fixtures and verify the declared
SOFARPC version matrix.

## Release checklist

1. Confirm CI is green on `main`.
2. Run the manual `wire-fixtures` workflow.
3. Update `CHANGELOG.md`.
4. Create a version tag such as `v0.1.0`.
5. Build with version metadata injected through `-ldflags`.
6. Build MCPB bundles with `VERSION=v0.1.0 MCPB_VERSION=0.1.0 bash scripts/build-mcpb.sh`, or run the manual
   `mcpb` workflow.
7. Verify `sofarpc-mcp version` and `sofarpc-mcp version -json`.
8. Install from the tag with `go install ...@v0.1.0`.

## Repo layout

```text
cmd/
  sofarpc-mcp/
    skill/               embedded sofarpc-invoke SKILL.md (go:embed source)
  spike-invoke/          direct-transport validation CLI (build tag: spike)
internal/
  boltclient/            pure-Go BOLT client
  sofarpcwire/           SofaRequest / SofaResponse encoding
  sourcecontract/        Java source scan -> contract store
  errcode/               stable error codes + recovery hints
  mcp/                   tool registration + handlers
  core/
    workspace/           project root resolution
    target/              precedence chain + TCP probe
    contract/            overload resolution + skeleton generation
    invoke/              plan building + execution
  javamodel/             Java class / field / method value types
  javatype/              Java type classification helpers
.claude/
  skills/sofarpc-invoke/SKILL.md symlink to cmd/sofarpc-mcp/skill/SKILL.md
docs/
  architecture.md        architecture reference
tests/
  e2e/                   optional build-tagged e2e smoke tests
```

## Status

- The repository is now pure-Go on the runtime path.
- `sofarpc_describe` works from project source scan; no Java sidecar or local cache is required.
- `go test ./...` passes on the current tree.

## License

Copyright 2026 He Xin (hex1n) and sofarpc-cli contributors.

Licensed under the [Apache License, Version 2.0](LICENSE).
Third-party dependencies remain subject to their own licenses.
