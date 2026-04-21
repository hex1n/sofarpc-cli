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

Repo-local helper scripts:

```sh
./scripts/install.sh
```

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\install.ps1
```

## Quick start

Build:

```sh
go build -o bin/sofarpc-mcp ./cmd/sofarpc-mcp
```

Configure the project-level MCP env:

```sh
export SOFARPC_PROJECT_ROOT=/abs/path/to/project
export SOFARPC_DIRECT_URL=bolt://host:12200
export SOFARPC_PROTOCOL=bolt
export SOFARPC_SERIALIZATION=hessian2
```

Optional per-target overrides:

```sh
# Alternative target source
export SOFARPC_REGISTRY_ADDRESS=zookeeper://host:2181

# Optional direct invoke hints
export SOFARPC_UNIQUE_ID=demo
export SOFARPC_TIMEOUT_MS=3000
export SOFARPC_CONNECT_TIMEOUT_MS=1000
```

Run:

```sh
./bin/sofarpc-mcp
```

The server speaks stdio MCP.

On startup, `sofarpc-mcp` scans `.java` files under `SOFARPC_PROJECT_ROOT`
to build describe-time contract information in pure Go. Hidden directories,
test trees, and common build-output directories are skipped.

If your agent host supports project-level MCP configuration, prefer putting the
same values on that project’s MCP server entry so `directUrl` does not need to
be repeated on every call:

```json
{
  "mcpServers": {
    "sofarpc-project": {
      "command": "/abs/path/to/sofarpc-mcp",
      "env": {
        "SOFARPC_PROJECT_ROOT": "/abs/path/to/project",
        "SOFARPC_DIRECT_URL": "bolt://host:12200",
        "SOFARPC_PROTOCOL": "bolt",
        "SOFARPC_SERIALIZATION": "hessian2"
      }
    }
  }
}
```

## Typical flow

1. `sofarpc_open`
2. `sofarpc_target`
3. `sofarpc_describe` if contract information is available
4. `sofarpc_invoke`
5. `sofarpc_replay`
6. `sofarpc_doctor` when invoke cannot proceed

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
- `directUrl` and `registryAddress` are per-call overrides; otherwise MCP env
  wins.
- `dryRun=true` returns the exact plan that `sofarpc_replay` can execute later.

When contract information is attached, facade-backed invoke automatically
normalizes common Java shapes before the wire step:

- root and nested DTOs get `@type` injected
- `List<DTO>` / `Map<String, V>` values are normalized recursively
- `java.math.BigDecimal` / `BigInteger` values are wrapped into canonical typed
  objects

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

## Repo layout

```text
cmd/
  sofarpc-mcp/           MCP entrypoint
  spike-invoke/          direct-transport validation CLI
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
  facadesemantic/        contract metadata shapes
  javatype/              Java type classification helpers
docs/
  architecture.md        architecture reference
```

## Status

- The repository is now pure-Go on the runtime path.
- `sofarpc_describe` works from project source scan; no Java sidecar or local cache is required.
- `go test ./...` passes on the current tree.
