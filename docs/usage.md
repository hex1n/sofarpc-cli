# sofarpc-cli usage

Detailed build, command, manifest, runtime-source, and diagnostics reference.
Design notes live in [sofarpc-cli-design.md](./sofarpc-cli-design.md).

Architecture:

- Go CLI for config resolution, runtime selection, daemon management, and UX
- Java worker runtime for real SOFARPC invocation
- Local runtime cache and daemon pool keyed by runtime version, runtime digest,
  classpath digest, and Java major version

## What Exists Today

Current commands:

- `call`
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

## Repo Layout

- `cmd/sofarpc`: Go CLI entrypoint
- `internal/cli`: CLI command handlers
- `internal/config`: local config and manifest persistence
- `internal/runtime`: runtime selection, daemon pool, source resolution, diagnostics
- `runtime-worker-java`: Java worker runtime

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

Positional shorthand (`<fqcn>.<method>`; legacy `Service/method` slash form still works):

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

One `Long` argument, positional form:

```powershell
sofarpc call com.example.UserService.getUser "[123]"
```

Equivalent with explicit flags — useful when the paramType isn't obvious from the JSON:

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
- active local context

### SOFARPC runtime version precedence

- `--sofa-rpc-version`
- `manifest.sofaRpcVersion`
- built-in default `5.7.6`

### Stub path precedence

- `--stub-path`
- `manifest.stubPaths`

### Method metadata precedence

- `--service`, `--method`, `--types`, `--payload-mode`
- matching entry in `manifest.services`

Important behavior:

- `--args` must be valid JSON
- if `--args` is omitted, it defaults to `[]`
- either a direct target or a registry target must be resolvable
- relative `stubPaths` in the manifest are resolved relative to the manifest file directory

## Payload Modes

### `raw`

Use this for:

- primitive values
- plain JSON objects
- `Map` / `List` style payloads
- generic smoke tests

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

### `schema`

Use this when:

- your input should be interpreted using available type metadata on the worker side

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
- `<configDir>/sofarpc-cli/runtime-sources.json`

Cache files:

- `<cacheDir>/sofarpc-cli/runtimes/<version>/`
- `<cacheDir>/sofarpc-cli/daemons/`

Typical Windows locations:

- `%AppData%\sofarpc-cli\contexts.json`
- `%AppData%\sofarpc-cli\runtime-sources.json`
- `%LocalAppData%\sofarpc-cli\runtimes\`
- `%LocalAppData%\sofarpc-cli\daemons\`

## Notes and Limitations

- The current default runtime version is `5.7.6`
- `doctor` validates target and runtime reachability; it does not invoke your real business method
- `call` defaults to printing only the decoded `result`; use `--full-response` for diagnostics
- no release packaging is documented here yet; current usage is source build plus `go run` or a locally built binary
