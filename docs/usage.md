# sofarpc-cli usage

Detailed build, command, manifest, runtime-source, and diagnostics reference.
Design notes live in [sofarpc-cli-design.md](./sofarpc-cli-design.md).

Architecture:

- Go CLI for config resolution, runtime selection, daemon management, and UX
- Java worker runtime for real SOFARPC invocation
- Local runtime cache and daemon pool keyed by runtime version, runtime digest,
  stub content digest, and Java major version

## What Exists Today

Current commands:

- `call`
- `describe`
- `doctor`
- `context`
- `manifest`
- `runtime`
- `daemon`

Current runtime features:

- SOFARPC runtime default version `5.7.6`
- payload modes: `raw`, `generic`, `schema`
- target modes: `direct`, `registry`
- local runtime install and cache
- runtime sources: `file`, `directory`
- local daemon inspection and cleanup
- interface reflection and local schema cache

## Repo Layout

- `cmd/sofarpc`: Go CLI entrypoint
- `internal/cli`: CLI command handlers
- `internal/config`: local config and manifest persistence
- `internal/runtime`: runtime selection, daemon pool, source resolution, diagnostics
- `runtime-worker-java`: Java worker runtime
- `internal/rpctest`: Go implementation of detect-config, schema/index generation, and case replay
- `spoon-indexer-java`: Spoon-based facade indexer
- `skills/`: Claude Code skills bundled with the CLI (currently `call-rpc`)

## Prerequisites

- **Go 1.26+** — required to build or run the CLI (`go version`)
- **JDK 8+** — worker runtime targets Java 8 bytecode; must be on `PATH` or passed via `--java-bin` (`java -version`)
- **Maven 3.6+** — required to build the worker jar (`mvn -version`)

Confirm all three are on `PATH` before building.

## Install

### Fresh-machine walkthrough

1. Install the prerequisites above.
2. Clone the repo:

   ```powershell
   git clone <repo-url> sofarpc-cli
   cd sofarpc-cli
   ```

3. Build the Java worker and the Go CLI:

   ```powershell
   mvn -f runtime-worker-java/pom.xml package
   go build -o bin/sofarpc.exe ./cmd/sofarpc
   ```

   Produces:

   - `runtime-worker-java/target/sofarpc-worker-5.7.6.jar`
   - `bin/sofarpc.exe` (or `bin/sofarpc` on macOS/Linux — drop the `.exe`)

4. Add `bin\` to your `PATH`, or copy the binary into any directory already on `PATH`. After that `sofarpc ...` works anywhere.

### Build for a different SOFARPC version

The shaded jar's filename reflects the `sofa-rpc.version` Maven property (default `5.7.6`). Override it to target another enterprise sofa-boot version:

```powershell
mvn -f runtime-worker-java/pom.xml -Dsofa-rpc.version=5.8.0 package
```

Output lands at `runtime-worker-java/target/sofarpc-worker-5.8.0.jar`.

### Transferring a prebuilt worker jar

If the new machine can't reach Maven central or an internal repo, copy a built jar from another box and register it directly — no Maven needed:

```powershell
sofarpc runtime install --version 5.7.6 --jar D:\transfer\sofarpc-worker-5.7.6.jar
```

You still need Go for the CLI binary. Copying the `sofarpc.exe` across works too.

### Running without installing a binary

While iterating on the Go code you can skip `go build`:

```powershell
go run ./cmd/sofarpc help
```

For everyday use, a built binary on `PATH` is faster and avoids recompiling.

## Claude Code Skills

The CLI ships a `call-rpc` skill at `skills/call-rpc/` that is a thin wrapper
for `sofarpc call`. Installing copies it to `~/.claude/skills/` so Claude Code
picks it up globally.

### Install

```powershell
sofarpc skills install                  # default: copies call-rpc
sofarpc skills install --force          # overwrite existing install
sofarpc skills install --dry-run        # preview copy
sofarpc skills where                    # show source / target paths
sofarpc skills list                     # list bundled skills
```

The skill does not bootstrap projects, build indexes, or run cases.
Use CLI facade subcommands directly when needed.

### Typical invocation

```powershell
sofarpc call --context prod -data '[...]' com.example.OrderFacade.createOrder
```

## Quick Start

### 1. Create a reusable target context

Direct target:

```powershell
go run ./cmd/sofarpc context set dev-direct `
  --direct-url bolt://127.0.0.1:12200 `
  --protocol bolt `
  --serialization hessian2 `
  --timeout-ms 10000 `
  --connect-timeout-ms 5000
```

Registry target:

```powershell
go run ./cmd/sofarpc context set dev-zk `
  --registry-address zookeeper://127.0.0.1:2181 `
  --registry-protocol zookeeper `
  --protocol bolt `
  --serialization hessian2
```

Project-scoped context (automatic selection when calling from that project):

```powershell
go run ./cmd/sofarpc context set project-a `
  --project-root C:\code\project-a `
  --direct-url bolt://127.0.0.1:12200 `
  --protocol bolt
```

Switch active context:

```powershell
go run ./cmd/sofarpc context use dev-direct
```

Inspect contexts:

```powershell
go run ./cmd/sofarpc context list
go run ./cmd/sofarpc context show
go run ./cmd/sofarpc context show dev-direct
```

### 2. Verify target reachability and runtime resolution

```powershell
go run ./cmd/sofarpc doctor --context dev-direct
```

`doctor` reports:

- resolved manifest path
- active context
- resolved target config
- selected runtime jar and Java version
- selected SOFARPC runtime version and where it came from (`flag`, `manifest`, or `default`)
- daemon state
- TCP reachability
- a synthetic invoke probe that distinguishes TCP-only success from real RPC path success

### 3. Invoke a service

Explicit flag form:

```powershell
go run ./cmd/sofarpc call `
  --context dev-direct `
  --service com.example.UserService `
  --method getUser `
  --types java.lang.Long `
  --args "[123]"
```

Positional shorthand (`<fqcn>.<method>`):

```powershell
go run ./cmd/sofarpc call com.example.UserService.getUser "[123]"
```

Print the full structured response:

```powershell
go run ./cmd/sofarpc call `
  --context dev-direct `
  --service com.example.UserService `
  --method getUser `
  --types java.lang.Long `
  --args "[123]" `
  --full-response
```

Default success behavior:

- CLI prints only the decoded `result`

Failure behavior:

- CLI prints the full structured error response to `stderr`
- process exits with code `1`

## Call Examples

The examples below assume `sofarpc.exe` is on `PATH` and a context named `dev-direct` is active. Substitute `go run ./cmd/sofarpc` for `sofarpc` when running from source.

### Simple request — primitive argument

One `Long` argument, positional form. When a stub jar is configured the CLI infers `--types` via reflection, so this is enough:

```powershell
sofarpc call com.example.UserService.getUser "[123]"
```

For a single-arg method the body can skip the outer array — the CLI wraps it into `[123]` automatically:

```powershell
sofarpc call com.example.UserService.getUser "123"
```

Equivalent with explicit flags — useful when no stub jar is available or you want to pin a specific overload:

```powershell
sofarpc call `
  --service com.example.UserService `
  --method getUser `
  --types java.lang.Long `
  --args "[123]"
```

On success the CLI prints just the decoded `result`. Add `--full-response` to see diagnostics (runtime jar, daemon key, java version).

### Complex request — DTO payload with stub jar

The worker classpath needs to resolve any custom DTO. Pass the API jar via `--stub-path`, and put the JSON body in a file to skip shell-quoting:

```powershell
sofarpc call `
  --service com.example.OrderService `
  --method createOrder `
  --types com.example.OrderCreateRequest `
  --stub-path D:\projects\order-app\target\order-api.jar `
  -d @order.json
```

Where `order.json` is plain JSON:

```json
[{"userId":123,"sku":"A1","qty":2}]
```

Useful overrides when the defaults don't fit:

- `--sofa-rpc-version 5.8.0` — pin a runtime version
- `--java-bin "C:\Program Files\Zulu\zulu-8\bin\java.exe"` — pick a JDK
- `--timeout-ms 15000` — raise the call timeout
- `--full-response` — also print runtime/daemon diagnostics

For repeated invocations, move the service metadata and stub path into a `sofarpc.manifest.json` (see [Manifest](#manifest)) so the positional form stays terse.

### Body input forms

`--args` (alias `--data` / `-d`, curl-style) accepts three forms:

- inline JSON: `-d "[123]"`
- `@path` — read from file (relative to cwd): `-d @order.json`
- `-` — read from stdin: `cat order.json | sofarpc call ... -d -`

Use `@file` or stdin to dodge PowerShell / bash JSON escaping.

## Describe

Reflect an interface from its stub jar and print the method signatures. Result is cached locally (keyed by stub-jar content), so later calls are sub-50ms.

```powershell
sofarpc describe --stub-path target\order-api.jar com.example.OrderService
```

Put flags before the positional FQCN (Go's flag parser stops at the first non-flag arg).

Bypass the cache and re-run the worker:

```powershell
sofarpc describe --refresh --stub-path target\order-api.jar com.example.OrderService
```

Schemas live in `<cacheDir>/sofarpc-cli/schemas/<classpathDigest>/<fqcn>.json`; `classpathDigest` changes whenever a stub jar's content changes, so rebuilt jars invalidate automatically.
Daemon keys use the same stub-content digest, so changing stub byte content changes the `daemon-key` and forces a fresh worker lifecycle.
When this happens, older loopback daemons for the same runtime profile are stopped automatically so stale worker processes are replaced cleanly.

## How Resolution Works

For `call` and `doctor`, effective config is resolved in this order:

### Target config precedence

- explicit CLI flags
- named or active context
- `manifest.defaultTarget`
- built-in defaults

Built-in defaults:

- `protocol = bolt`
- `serialization = hessian2`
- `timeoutMs = 3000`
- `connectTimeoutMs = 1000`

### Context selection precedence

- `--context`
- `manifest.defaultContext`
- project-scoped context matched by current project root (when neither flag nor manifest context is set)
- active local context

### SOFARPC runtime version precedence

- `--sofa-rpc-version`
- `manifest.sofaRpcVersion`
- built-in default `5.7.6`

### Stub path precedence

- `--stub-path`
- `manifest.stubPaths`
- auto-discovered jars from `<project>/.sofarpc/config.json` (`jarGlob` and `depsDir`)

### Method metadata precedence

- `--service`, `--method`, `--types`, `--payload-mode`
- matching entry in `manifest.services`
- for `--types`, when still unset and a stub jar is configured, the CLI reflects the interface and picks the method's `paramTypes` (errors on overload ambiguity — pass `--types` to pin one)

Important behavior:

- `--args` must be valid JSON
- if `--args` is omitted, it defaults to `[]`
- if the resolved method takes exactly one parameter and the body isn't already a JSON array, it's wrapped as `[body]` automatically
- either a direct target or a registry target must be resolvable
- relative `stubPaths` in the manifest are resolved relative to the manifest file directory

## Payload Modes

### `raw`

Use this for:

- primitive values
- plain JSON objects
- `Map` / `List` style payloads
- generic smoke tests
- wrapper-style responses such as `OperationResult<T>` when stub jars are complete

Notes:

- raw mode is the preferred path when the worker classpath contains the DTO jar
- when the top-level parameter is a declared DTO class, raw mode now materializes that class directly, so nested fields like `List<FundAssetItem>` are reconstructed correctly
- the worker now falls back to field-based response serialization if helper getters like `dataOptional()` / `dataOrThrow()` explode during Jackson introspection

Example:

```powershell
go run ./cmd/sofarpc call `
  --context dev-direct `
  --service com.example.UserService `
  --method updateUser `
  --types com.example.UserUpdateRequest `
  --payload-mode raw `
  --args "[{\"id\":123,\"name\":\"alice\"}]"
```

### `generic`

Use this when:

- worker classpath does not contain the DTO
- you want to drive the generic path explicitly

Limitations:

- only the top-level `paramTypes` are declared to SOFARPC
- nested custom collection elements such as `List<FundAssetItem>` can still arrive on the provider side as `LinkedHashMap`
- do not switch to `generic` just because the response wrapper exposes `Optional` helper getters; prefer `raw` first

### `schema`

Use this when:

- your input should be interpreted using available type metadata on the worker side

Current boundary:

- when interface metadata is available, `describe` records full generic parameter signatures
- schema mode can reconstruct nested generic collection/map element types from those signatures
- schema mode is mainly for top-level generic parameters such as `List<CustomDTO>` or `Map<String, CustomDTO>`
- if you skip stub jars entirely, schema mode falls back to the top-level types you passed

## Manifest

Default manifest path:

- `sofarpc.manifest.json` in the current working directory

Create a starter manifest:

```powershell
go run ./cmd/sofarpc manifest init `
  --output sofarpc.manifest.json `
  --service com.example.UserService `
  --method getUser `
  --types java.lang.Long `
  --payload-mode raw `
  --direct-url bolt://127.0.0.1:12200
```

Generate a manifest from an existing context:

```powershell
go run ./cmd/sofarpc manifest generate `
  --context dev-direct `
  --output sofarpc.manifest.json `
  --service com.example.UserService `
  --method getUser `
  --types java.lang.Long `
  --payload-mode raw `
  --stub-path ..\\app\\target\\app-api.jar
```

Example manifest:

```json
{
  "schemaVersion": "v1alpha1",
  "sofaRpcVersion": "5.7.6",
  "defaultContext": "dev-direct",
  "defaultTarget": {
    "mode": "direct",
    "directUrl": "bolt://127.0.0.1:12200",
    "protocol": "bolt",
    "serialization": "hessian2",
    "timeoutMs": 10000,
    "connectTimeoutMs": 5000
  },
  "stubPaths": [
    "../app/target/app-api.jar"
  ],
  "services": {
    "com.example.UserService": {
      "methods": {
        "getUser": {
          "paramTypes": [
            "java.lang.Long"
          ],
          "payloadMode": "raw"
        }
      }
    }
  }
}
```

## Runtime Cache

List installed runtimes:

```powershell
go run ./cmd/sofarpc runtime list
```

Show one runtime:

```powershell
go run ./cmd/sofarpc runtime show 5.7.6
```

Install from an explicit jar:

```powershell
go run ./cmd/sofarpc runtime install --version 5.7.6 --jar C:\path\to\sofarpc-worker-5.7.6.jar
```

Install from the active runtime source or bundled workspace artifact:

```powershell
go run ./cmd/sofarpc runtime install --version 5.7.6
```

Install from a named runtime source:

```powershell
go run ./cmd/sofarpc runtime install --version 5.7.6 --source local-cache
```

## Runtime Sources

Runtime sources are local-only configuration entries used to resolve worker jars.

Supported kinds:

- `file`
- `directory`

### Local file source

```powershell
go run ./cmd/sofarpc runtime source set `
  --kind file `
  --path C:\artifacts\sofarpc-worker-5.7.6.jar `
  local-file
```

### Local directory source

The directory source looks for these candidate paths:

- `<base>/sofarpc-worker-<version>.jar`
- `<base>/<version>/sofarpc-worker-<version>.jar`
- `<base>/runtime-worker-java/target/sofarpc-worker-<version>.jar`

Example:

```powershell
go run ./cmd/sofarpc runtime source set `
  --kind directory `
  --path C:\artifacts\sofa-runtimes `
  local-dir
```

### Inspect and switch sources

```powershell
go run ./cmd/sofarpc runtime source list
go run ./cmd/sofarpc runtime source show
go run ./cmd/sofarpc runtime source show local-dir
go run ./cmd/sofarpc runtime source use local-dir
go run ./cmd/sofarpc runtime source delete local-dir
```

## Daemon Management

List daemons:

```powershell
go run ./cmd/sofarpc daemon list
```

Show one daemon:

```powershell
go run ./cmd/sofarpc daemon show <daemon-key>
```

Stop one daemon:

```powershell
go run ./cmd/sofarpc daemon stop <daemon-key>
```

Remove stale daemon metadata and logs:

```powershell
go run ./cmd/sofarpc daemon prune
```

## Local Files and Directories

The CLI stores local state outside the repo using `os.UserConfigDir()` and
`os.UserCacheDir()`.

Config files:

- `<configDir>/sofarpc-cli/contexts.json`
- `<configDir>/sofarpc-cli/contexts.template.json`
- `<configDir>/sofarpc-cli/runtime-sources.json`

Quick bootstrap:

- `sofarpc skills install` prints `contexts.template.json` path.
- copy the template to `contexts.json`, fill your project entries, and keep multiple contexts in one file (with optional `projectRoot`).

Cache files:

- `<cacheDir>/sofarpc-cli/runtimes/<version>/`
- `<cacheDir>/sofarpc-cli/daemons/`
- `<cacheDir>/sofarpc-cli/schemas/<classpathDigest>/<fqcn>.json`

Typical Windows locations:

- `%AppData%\sofarpc-cli\contexts.json`
- `%AppData%\sofarpc-cli\contexts.template.json`
- `%AppData%\sofarpc-cli\runtime-sources.json`
- `%LocalAppData%\sofarpc-cli\runtimes\`
- `%LocalAppData%\sofarpc-cli\daemons\`
- `%LocalAppData%\sofarpc-cli\schemas\`

## Notes and Limitations

- The current default runtime version is `5.7.6`
- `doctor` validates target and runtime reachability; it does not invoke your real business method
- `call` defaults to printing only the decoded `result`; use `--full-response` for diagnostics
- no release packaging is documented here yet; current usage is source build plus `go run` or a locally built binary
