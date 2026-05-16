# sofarpc-cli

Agent-first local MCP server for SOFARPC generic invoke.

- Design: [docs/architecture.md](./docs/architecture.md)
- Mainline: pure-Go `direct + bolt + hessian2`
- Entry point: `cmd/sofarpc-mcp`

## MCP tools

| Tool | Purpose |
| --- | --- |
| `sofarpc_open` | Open a workspace. Returns project root, resolved target, capabilities, and a session id. |
| `sofarpc_target` | Resolve the effective target and probe reachability. |
| `sofarpc_describe` | Resolve overloads and build a JSON skeleton when contract information is available. |
| `sofarpc_invoke` | Build a plan and execute it. `dryRun=true` returns the plan only. |
| `sofarpc_replay` | Re-run a captured plan from `sessionId` or a literal `payload`. |
| `sofarpc_doctor` | Run structured diagnostics across target, workspace state, and invoke prerequisites. |

Every failure returns a stable `errcode.Code` and may include a structured
`nextTool` hint. Agents are expected to follow that hint directly instead of
re-deriving the next action from prose.

## Install

Fresh machine, no Java runtime required:

```sh
go install github.com/hex1n/sofarpc-cli/cmd/sofarpc-mcp@latest
```

For repeatable installs, prefer a released tag once one exists:

```sh
go install github.com/hex1n/sofarpc-cli/cmd/sofarpc-mcp@v0.1.0
```

Use `@latest` only when you intentionally want the newest commit on the default
branch.

Repo-local helper scripts:

```sh
./scripts/install.sh
```

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\install.ps1
```

Setup is split into two scopes:

- User scope registers the MCP server with Claude Code and Codex and installs
  the `sofarpc-invoke` skill for the current user.
- Project scope writes target defaults into the Java project under
  `.sofarpc/`.

User setup is the default. It is idempotent and merges the existing sofarpc env
block by default, so re-running with one new flag does not drop manually added
guardrails:

```sh
sofarpc-mcp setup --scope=user                                      # both clients + skill
sofarpc-mcp setup --claude-code                                     # Claude Code only
sofarpc-mcp setup --codex                                           # Codex only
sofarpc-mcp setup --install-skill=false                             # MCP config only
sofarpc-mcp setup --replace-env --direct-url=bolt://host:12200      # replace sofarpc env
sofarpc-mcp setup --dry-run --allow-invoke --allowed-services='*'   # preview
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
  --registry-address=zookeeper://zk.example.com:2181 --protocol=bolt
```

`--local` writes `.sofarpc/config.local.json` and ensures that path is ignored
by the project's `.gitignore`. `--shared` writes `.sofarpc/config.json`.
Existing project config files are not overwritten unless `--force` is passed.
Real-invoke guardrails such as `--allow-invoke` are user-scope env settings and
are rejected in project setup.

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
export SOFARPC_PROJECT_ROOT=/abs/path/to/project
export SOFARPC_DIRECT_URL=bolt://host:12200

./bin/sofarpc-mcp
```

The server speaks stdio MCP. `SOFARPC_PROJECT_ROOT` falls back to the
process CWD, and `SOFARPC_PROTOCOL` / `SOFARPC_SERIALIZATION` default
to `bolt` / `hessian2`, so neither needs to be set unless you're
overriding the defaults.

Optional per-target tuning:

```sh
export SOFARPC_REGISTRY_ADDRESS=zookeeper://host:2181
export SOFARPC_UNIQUE_ID=demo
export SOFARPC_TIMEOUT_MS=3000
export SOFARPC_CONNECT_TIMEOUT_MS=1000
```

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

Do not set `mode` in project config. It is derived atomically from the first
configured endpoint in priority order: `directUrl` selects direct mode,
`registryAddress` selects registry mode, and the other endpoint fields from
lower-priority layers are ignored.

Target resolution order is:

```text
per-call input > .sofarpc/config.local.json > .sofarpc/config.json > MCP env > defaults
```

Real network calls are disabled by default. `dryRun=true` always works,
but non-dry-run `sofarpc_invoke` requires an explicit opt-in:

```sh
export SOFARPC_ALLOW_INVOKE=true
```

Use that only for development or test targets. For safer local setups,
restrict callable services, bound `@file` inputs, and cap how much plan data
sessions retain for `sessionId` replay. Direct BOLT responses are also capped
before the client allocates the response body:

```sh
export SOFARPC_ALLOWED_SERVICES=com.foo.UserFacade,com.foo.OrderFacade
export SOFARPC_ALLOWED_TARGET_HOSTS=127.0.0.1,dev-rpc.example.com:12200
export SOFARPC_ARGS_FILE_ROOT=/abs/path/to/project
export SOFARPC_ARGS_FILE_MAX_BYTES=1048576
export SOFARPC_SESSION_PLAN_MAX_BYTES=1048576
export SOFARPC_MAX_RESPONSE_BYTES=16777216
```

`@file` arguments are resolved inside `SOFARPC_ARGS_FILE_ROOT` when set,
otherwise inside `SOFARPC_PROJECT_ROOT`. Files outside that root are
rejected after symlink resolution. The default file-size limit is 1 MiB.

For non-dry-run direct calls, the default policy only executes the resolved
project/env target from `.sofarpc/config.local.json`, `.sofarpc/config.json`, or
`SOFARPC_DIRECT_URL`. Per-call `directUrl` overrides and literal replay payload
targets require `SOFARPC_ALLOW_TARGET_OVERRIDE=true`. `SOFARPC_ALLOWED_TARGET_HOSTS`,
when set, restricts real direct targets to a comma-separated list of host or
host:port values; `*` allows all hosts. Invalid project target config is
reported by `sofarpc_target` / `sofarpc_doctor` and blocks real invoke until
fixed.

`SOFARPC_SESSION_PLAN_MAX_BYTES` controls only in-memory session capture for
`sofarpc_replay` by `sessionId`. When a plan is larger than this limit,
`sofarpc_invoke` still returns the full plan and can still be replayed by
passing that plan as `payload`; the plan is just not retained in the session.
Set it to `0` to disable the captured-plan byte bound.
`sofarpc_doctor` reports real-invoke guardrails in its `invoke-policy` check.

`SOFARPC_MAX_RESPONSE_BYTES` controls the maximum BOLT response body accepted
by the direct transport. Invalid or non-positive values fall back to the
default 16 MiB limit.

`sofarpc-mcp` does not scan `.java` files during MCP startup. It loads
source-contract information lazily when a tool first needs the contract
store, so tool registration stays fast even for large workspaces. Hidden
directories, test trees, and common build-output directories are skipped.

### Hand-written client config

If you prefer not to run `sofarpc-mcp setup`, drop this into the
client's MCP config (Claude Code: `~/.claude.json` → `mcpServers`;
Codex: `~/.codex/config.toml` under `[mcp_servers.sofarpc]`):

```json
{
  "mcpServers": {
    "sofarpc-project": {
      "command": "/abs/path/to/sofarpc-mcp",
      "env": {
        "SOFARPC_PROJECT_ROOT": "/abs/path/to/project",
        "SOFARPC_DIRECT_URL": "bolt://host:12200"
      }
    }
  }
}
```

## Typical flow

1. `sofarpc_open`
2. `sofarpc_target` (optionally with `project` or `cwd` to inspect another project)
3. `sofarpc_describe` if contract information is available
4. `sofarpc_invoke`
5. `sofarpc_replay`
6. `sofarpc_doctor` when invoke cannot proceed

The installed `sofarpc-invoke` skill turns this flow into a machine-
readable playbook for Claude Code / Codex, including the errcode
recovery protocol. See `cmd/sofarpc-mcp/skill/SKILL.md` for the
source.

## `sofarpc_invoke` shape

```json
{
  "service": "com.foo.Facade",
  "method": "getUser",
  "types": ["com.foo.GetUserRequest"],
  "args": [{ "userId": 1 }],
  "version": "2.0",
  "targetAppName": "foo-app",
  "directUrl": "bolt://host:12200",
  "dryRun": true
}
```

- `version` overrides the SOFA service version for this call.
- `targetAppName` sets the direct-transport target app header.
- `directUrl` and `registryAddress` are per-call overrides; otherwise project
  config wins, then MCP env.
- `dryRun=true` returns the exact plan that `sofarpc_replay` can execute later;
  replay accepts either that plan or a dry-run output object containing `plan`.
- Real invocation requires `SOFARPC_ALLOW_INVOKE=true`; keep the default disabled
  when you only want planning, skeletons, and diagnostics.
- Non-dry-run direct calls default to the resolved project/env direct target;
  per-call target overrides require `SOFARPC_ALLOW_TARGET_OVERRIDE=true` and can
  be bounded with `SOFARPC_ALLOWED_TARGET_HOSTS`.
- If `sessionId` is provided, the plan is retained for session replay only when
  its JSON size is at or below `SOFARPC_SESSION_PLAN_MAX_BYTES`; oversized plans
  are still returned and can be replayed as a literal payload.
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
shapes, the caller must send them explicitly.

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
without Java. Before release, run the manual GitHub Actions `wire-fixtures`
workflow; it regenerates the baseline Java fixtures and verifies the declared
SOFARPC version matrix.

## Release checklist

1. Confirm CI is green on `main`.
2. Run the manual `wire-fixtures` workflow.
3. Update `CHANGELOG.md`.
4. Create a version tag such as `v0.1.0`.
5. Build with version metadata injected through `-ldflags`.
6. Verify `sofarpc-mcp version` and `sofarpc-mcp version -json`.
7. Install from the tag with `go install ...@v0.1.0`.

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
  skills/sofarpc-invoke/ symlink to cmd/sofarpc-mcp/skill/ for in-repo discovery
docs/
  architecture.md        architecture reference
tests/
  e2e/                   optional build-tagged e2e smoke tests
```

## Status

- The repository is now pure-Go on the runtime path.
- `sofarpc_describe` works from project source scan; no Java sidecar or local cache is required.
- `go test ./...` passes on the current tree.
