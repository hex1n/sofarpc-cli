# Greenfield SOFARPC CLI

`greenfield/` contains the new control-plane/runtime split implementation
described in `docs/greenfield-sofarpc-cli-design.md`.

The current architecture is:

- Go CLI for config resolution, runtime selection, daemon management, and UX
- Java worker runtime for real SOFARPC invocation
- Local runtime cache and daemon pool keyed by runtime version, runtime digest,
  classpath digest, and Java major version

This README documents how to build and use the current implementation.

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
- runtime sources: `file`, `directory`, `url-template`, `manifest-url`
- runtime source validation without installation
- local daemon inspection and cleanup

## Repo Layout

- `cmd/rpc`: Go CLI entrypoint
- `internal/cli`: CLI command handlers
- `internal/config`: local config and manifest persistence
- `internal/runtime`: runtime selection, daemon pool, source resolution, diagnostics
- `runtime-worker-java`: Java worker runtime

## Prerequisites

- Java must be available on `PATH`, or pass `--java-bin`
- Maven is required to build the Java worker
- Go is required to build or run the CLI

## Build

From the repo root:

```powershell
mvn -f greenfield/runtime-worker-java/pom.xml package
go build -o greenfield/bin/rpc ./greenfield/cmd/rpc
```

From the `greenfield/` directory:

```powershell
mvn -f runtime-worker-java/pom.xml package
go build -o bin/rpc ./cmd/rpc
```

You can also run the CLI without building a binary:

```powershell
cd greenfield
go run ./cmd/rpc help
```

## Quick Start

### 1. Create a reusable target context

Direct target:

```powershell
go run ./cmd/rpc context set dev-direct `
  --direct-url bolt://127.0.0.1:12200 `
  --protocol bolt `
  --serialization hessian2 `
  --timeout-ms 10000 `
  --connect-timeout-ms 5000
```

Registry target:

```powershell
go run ./cmd/rpc context set dev-zk `
  --registry-address zookeeper://127.0.0.1:2181 `
  --registry-protocol zookeeper `
  --protocol bolt `
  --serialization hessian2
```

Switch active context:

```powershell
go run ./cmd/rpc context use dev-direct
```

Inspect contexts:

```powershell
go run ./cmd/rpc context list
go run ./cmd/rpc context show
go run ./cmd/rpc context show dev-direct
```

### 2. Verify target reachability and runtime resolution

```powershell
go run ./cmd/rpc doctor --context dev-direct
```

`doctor` reports:

- resolved manifest path
- active context
- resolved target config
- selected runtime jar and Java version
- daemon state
- TCP reachability
- a synthetic invoke probe that distinguishes TCP-only success from real RPC path success

### 3. Invoke a service

Explicit flag form:

```powershell
go run ./cmd/rpc call `
  --context dev-direct `
  --service com.example.UserService `
  --method getUser `
  --types java.lang.Long `
  --args "[123]"
```

Positional shorthand:

```powershell
go run ./cmd/rpc call com.example.UserService/getUser "[123]"
```

Print the full structured response:

```powershell
go run ./cmd/rpc call `
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
go run ./cmd/rpc call `
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

- `rpcctl.manifest.json` in the current working directory

Create a starter manifest:

```powershell
go run ./cmd/rpc manifest init `
  --output rpcctl.manifest.json `
  --service com.example.UserService `
  --method getUser `
  --types java.lang.Long `
  --payload-mode raw `
  --direct-url bolt://127.0.0.1:12200
```

Generate a manifest from an existing context:

```powershell
go run ./cmd/rpc manifest generate `
  --context dev-direct `
  --output rpcctl.manifest.json `
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
go run ./cmd/rpc runtime list
```

Show one runtime:

```powershell
go run ./cmd/rpc runtime show 5.7.6
```

Install from an explicit jar:

```powershell
go run ./cmd/rpc runtime install --version 5.7.6 --jar C:\path\to\rpc-runtime-worker-sofa-5.7.6.jar
```

Install from the active runtime source or bundled workspace artifact:

```powershell
go run ./cmd/rpc runtime install --version 5.7.6
```

Install from a named runtime source:

```powershell
go run ./cmd/rpc runtime install --version 5.7.6 --source local-cache
```

## Runtime Sources

Runtime sources are local-only configuration entries used to resolve worker jars.

Supported kinds:

- `file`
- `directory`
- `url-template`
- `manifest-url`

### Local file source

```powershell
go run ./cmd/rpc runtime source set `
  --kind file `
  --path C:\artifacts\rpc-runtime-worker-sofa-5.7.6.jar `
  local-file
```

### Local directory source

The directory source looks for these candidate paths:

- `<base>/rpc-runtime-worker-sofa-<version>.jar`
- `<base>/<version>/rpc-runtime-worker-sofa-<version>.jar`
- `<base>/runtime-worker-java/target/rpc-runtime-worker-sofa-<version>.jar`
- `<base>/greenfield/runtime-worker-java/target/rpc-runtime-worker-sofa-<version>.jar`

Example:

```powershell
go run ./cmd/rpc runtime source set `
  --kind directory `
  --path C:\artifacts\sofa-runtimes `
  local-dir
```

### URL template source

```powershell
go run ./cmd/rpc runtime source set `
  --kind url-template `
  --path https://artifacts.example.com/sofa/{version}/rpc-runtime-worker-sofa-{version}.jar `
  --sha256-url https://artifacts.example.com/sofa/{version}/rpc-runtime-worker-sofa-{version}.jar.sha256 `
  remote-template
```

`--sha256-url` is only supported for `url-template`.

### Manifest URL source

```powershell
go run ./cmd/rpc runtime source set `
  --kind manifest-url `
  --path https://artifacts.example.com/sofa/runtime-manifest.json `
  remote-catalog
```

Example runtime source manifest:

```json
{
  "schemaVersion": "v1alpha1",
  "versions": {
    "5.7.6": {
      "url": "https://artifacts.example.com/sofa/5.7.6/rpc-runtime-worker-sofa-{version}.jar",
      "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    },
    "5.8.0": {
      "url": "https://artifacts.example.com/sofa/5.8.0/rpc-runtime-worker-sofa-{version}.jar",
      "sha256Url": "https://artifacts.example.com/sofa/5.8.0/rpc-runtime-worker-sofa-{version}.jar.sha256"
    }
  }
}
```

### Inspect and switch sources

```powershell
go run ./cmd/rpc runtime source list
go run ./cmd/rpc runtime source show
go run ./cmd/rpc runtime source show remote-template
go run ./cmd/rpc runtime source use remote-template
go run ./cmd/rpc runtime source delete remote-template
```

### Validate sources without installation

Validate one source:

```powershell
go run ./cmd/rpc runtime source validate remote-template --version 5.7.6
```

Validate the active source:

```powershell
go run ./cmd/rpc runtime source validate --version 5.7.6
```

Summarize all configured sources for a version:

```powershell
go run ./cmd/rpc runtime source list --version 5.7.6
```

Validation reports include:

- whether the source defines the requested version
- whether the artifact is reachable
- whether checksum metadata is available
- resolved runtime, manifest, and checksum URLs when applicable

`runtime source validate` exits `0` on success and `1` on validation failure after printing the JSON report.

## Daemon Management

List daemons:

```powershell
go run ./cmd/rpc daemon list
```

Show one daemon:

```powershell
go run ./cmd/rpc daemon show <daemon-key>
```

Stop one daemon:

```powershell
go run ./cmd/rpc daemon stop <daemon-key>
```

Remove stale daemon metadata and logs:

```powershell
go run ./cmd/rpc daemon prune
```

## Local Files and Directories

The CLI stores local state outside the repo using `os.UserConfigDir()` and
`os.UserCacheDir()`.

Config files:

- `<configDir>/rpcctl-greenfield/contexts.json`
- `<configDir>/rpcctl-greenfield/runtime-sources.json`

Cache files:

- `<cacheDir>/rpcctl-greenfield/runtimes/<version>/`
- `<cacheDir>/rpcctl-greenfield/daemons/`

Typical Windows locations:

- `%AppData%\rpcctl-greenfield\contexts.json`
- `%AppData%\rpcctl-greenfield\runtime-sources.json`
- `%LocalAppData%\rpcctl-greenfield\runtimes\`
- `%LocalAppData%\rpcctl-greenfield\daemons\`

## Notes and Limitations

- The current default runtime version is `5.7.6`
- `doctor` validates target and runtime reachability; it does not invoke your real business method
- `call` defaults to printing only the decoded `result`; use `--full-response` for diagnostics
- runtime source validation is read-only; it does not populate the runtime cache
- no release packaging is documented here yet; current usage is source build plus `go run` or a locally built binary
