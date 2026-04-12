# sofa-rpcctl

[中文说明](./README.zh-CN.md)

`sofa-rpcctl` is a portable CLI for invoking SOFABoot / SOFARPC services from a terminal without depending on business interface jars.

The design follows four constraints:

1. It must work across different SOFABoot projects.
2. It must speak native SOFARPC instead of forcing every team to maintain a second REST surface.
3. It must stay honest about version compatibility instead of pretending one client runtime fits every provider.
4. It must remain usable like `curl`: inline target flags should work immediately, while smarter behavior comes from optional project or user metadata.

## What It Does

- Invokes SOFARPC services through `directUrl` or a registry.
- Uses native SOFARPC invocation without requiring business DTO classes on the caller classpath.
- Supports three payload modes: `raw` for scalar / `Map` / `List` payloads, `generic` for explicit `GenericObject`-compatible bodies, and `schema` when metadata provides method signatures.
- Supports stub-aware invocation through `--stub-path`, so local business classes can be used for complex DTO serialization and `$invoke`.
- Splits the CLI into a stable launcher and versioned SOFARPC runtimes.
- Supports explicit `--sofa-rpc-version`, automatic version inference, runtime auto-download, and local runtime caching.
- Auto-discovers `rpcctl-manifest.yaml` from the current project or `~/.config/sofa-rpcctl/`.
- Supports reusable global contexts via `rpcctl context`.
- Generates manifests from existing `config/rpcctl.yaml` and `config/metadata.yaml`.
- Preserves overloaded Java method signatures in generated manifests and requires `--types` when metadata alone is ambiguous.
- Emits structured diagnostics such as `payloadMode`, `paramTypeSource`, `invokeStyle`, `errorPhase`, `retriable`, resolved runtime version/source, and provider reachability hints.
- Produces release assets and a bootstrap installer so the CLI can be installed without copying the source tree.

## Commands

- `invoke`: full-form method invocation.
- `call`: shorter syntax for `invoke`.
- `doctor`: diagnose context discovery, runtime resolution, and target reachability before invoking.
- `list`: list services from metadata or a manifest.
- `describe`: show one service from metadata or a manifest.
- `context`: manage named user profiles in `~/.config/sofa-rpcctl/contexts.yaml`.
- `manifest generate|init`: generate `rpcctl-manifest.yaml` from existing config/metadata or create a skeleton.
- `manifest generate` can also import method signatures directly from local interface jars or compiled classes, including overloaded methods.
- `rpcctl-manifest-maven-plugin`: Maven goal `generate` can emit `rpcctl-manifest.yaml` using the same schema import pipeline.

## Quick Start

Build the launcher and the default runtime:

```bash
./scripts/build.sh
```

Build every runtime declared under `runtime-manifests/sofa-rpc/`:

```bash
./scripts/build-all-runtimes.sh
```

Run a zero-config direct invocation:

```bash
./bin/rpcctl invoke \
  --direct-url bolt://127.0.0.1:12200 \
  --service com.example.UserService \
  --method getUser \
  --types java.lang.Long \
  --args '[123]'
```

Run the same call with the short syntax:

```bash
./bin/rpcctl call \
  com.example.UserService/getUser \
  '[123]' \
  --direct-url bolt://127.0.0.1:12200
```

Registry invocation:

```bash
./bin/rpcctl call \
  com.example.UserService/getUser \
  '[123]' \
  --registry zookeeper://127.0.0.1:2181
```

If the current project provides `rpcctl-manifest.yaml`, `rpcctl` can fill `defaultEnv`, `uniqueId`, and method parameter types:

```bash
./bin/rpcctl call com.example.UserService/getUser '[123]'
```

## Smart Mode

`rpcctl` has two operating modes:

- Transport mode: behaves like `curl`. Pass `--direct-url` or `--registry` inline and call immediately.
- Metadata mode: behaves more intelligently because it reads project or user metadata instead of guessing.

The launcher discovers metadata in this order:

1. `--manifest`
2. `RPCCTL_MANIFEST`
3. upward search from the current directory for `rpcctl-manifest.yaml` or `rpcctl-manifest.yml`
4. `~/.config/sofa-rpcctl/rpcctl-manifest.yaml`

That last step is what makes manifest-backed usage work from any directory after a one-time setup.

## Payload Modes

`rpcctl` does not try to magically infer arbitrary DTO graphs from JSON. It supports three explicit operating shapes:

- `raw`: best for scalar parameters and signatures that already use `Map`, `List`, `Set`, arrays, or primitives. This is the closest mode to `curl`.
- `generic`: best for explicit `GenericObject`-compatible payloads where you know the declared DTO type and, if needed, provide nested `@type` / `@value` hints.
- `schema`: best for business DTOs when `rpcctl-manifest.yaml` or other metadata can provide the declared method signature.

The JSON result now reports which path was used through `payloadMode`, `paramTypeSource`, and `invokeStyle`.

Example raw payload:

```bash
rpcctl call \
  com.example.PayloadService/submit \
  '[{"requestId":"payload-01","meta":{"channel":"smoke"},"lines":[{"sku":"sku-apple","quantity":2}]}]' \
  --registry zookeeper://127.0.0.1:2181 \
  --types java.util.Map \
  --unique-id payload-service
```

Example generic payload with explicit type hints:

```bash
rpcctl invoke \
  --direct-url bolt://127.0.0.1:12200 \
  --service com.example.OrderService \
  --method submit \
  --types com.example.OrderRequest \
  --args '[{"@type":"com.example.OrderRequest","customer":{"@type":"com.example.Customer","name":"alice"}}]'
```

Example stub-aware DTO invocation:

```bash
rpcctl call \
  com.example.OrderService/submit \
  '[{"requestId":"order-01","customer":{"name":"alice","address":{"city":"Shanghai"}},"lines":[{"sku":"sku-apple","quantity":2,"price":12.3}]}]' \
  --direct-url bolt://127.0.0.1:12241 \
  --manifest ./rpcctl-manifest.yaml \
  --stub-path ./target/classes \
  --confirm
```

Important limits:

- The official SOFARPC generic path is only defined for `bolt + hessian2`.
- Without schema or business stubs, nested DTO graphs are not guaranteed to deserialize correctly.
- For ad-hoc smoke checks, prefer `raw` `Map` / `List` payloads or a REST binding if the service exposes one.

## Registry Notes

Registry mode is only as good as the provider address that gets published. If the provider exports `localhost`, an internal container IP, or another unreachable host, the registry lookup may succeed while the actual RPC connection still fails.

For local and containerized tests, prefer setting `ServerConfig#setVirtualHost(...)` and, when needed, `ServerConfig#setVirtualPort(...)` on the provider side so the registry publishes a reachable address. `rpcctl` now distinguishes between:

- provider not found in the registry
- provider found but unreachable
- method signature mismatches
- deserialization failures caused by DTO / payload incompatibility

Failure responses also carry machine-readable fields:

- `errorCode`: stable category such as `RPC_PROVIDER_UNREACHABLE` or `RPC_METHOD_NOT_FOUND`
- `errorPhase`: where the failure happened, such as `discovery`, `connect`, `invoke`, `serialize`, or `deserialize`
- `retriable`: whether retrying the same request may succeed without changing the payload
- `diagnostics`: structured low-level hints such as `targetMode`, `configuredTarget`, `providerAddress`, and `invokeStyle`

## Doctor

Run a dry diagnostic before the real call:

```bash
rpcctl doctor \
  --direct-url bolt://127.0.0.1:12200 \
  --sofa-rpc-version 5.4.0
```

It reports:

- config / manifest / context discovery source
- resolved SOFARPC version and whether it came from fallback
- local runtime jar resolution and whether auto-download would be used
- direct or registry TCP reachability

## Contexts

Contexts are reusable user profiles stored at:

```text
~/.config/sofa-rpcctl/contexts.yaml
```

They let you pin a default manifest, env, registry, direct target, runtime base URL, and SOFARPC version without depending on the current working directory.

Create or update a context:

```bash
rpcctl context set test \
  --manifest ~/.config/sofa-rpcctl/rpcctl-manifest.yaml \
  --stub-path ~/workspace/demo/target/classes \
  --runtime-base-url https://github.com/hex1n/sofa-rpcctl/releases/download/v0.1.0 \
  --current
```

List contexts:

```bash
rpcctl context list
```

Show the current context:

```bash
rpcctl context show
```

Switch contexts:

```bash
rpcctl context use test
```

Delete one:

```bash
rpcctl context delete test
```

Once a context is active, this works from any directory:

```bash
rpcctl call com.example.UserService/getUser '[123]'
```

## Manifest Generation

Generate `rpcctl-manifest.yaml` from existing `config/rpcctl.yaml` and `config/metadata.yaml`:

```bash
rpcctl manifest generate
```

Write somewhere else and overwrite if needed:

```bash
rpcctl manifest generate \
  --output /tmp/rpcctl-manifest.yaml \
  --force
```

Import service metadata directly from compiled business classes:

```bash
rpcctl manifest generate \
  --output rpcctl-manifest.yaml \
  --force \
  --stub-path ./target/classes \
  --service-class com.example.OrderService \
  --service-unique-id com.example.OrderService=order-service
```

When imported interfaces contain overloaded methods, `manifest generate` keeps them under `overloads:`. During invocation, `rpcctl` resolves them by `--types` first, then by argument count when that is unambiguous.

Generate with Maven (same importer pipeline as CLI `manifest generate`):

```bash
mvn com.hex1n:rpcctl-manifest-maven-plugin:0.1.0:generate \
  -Doutput=/tmp/rpcctl-manifest.yaml \
  -DserviceClass=com.example.OrderService \
  -DstubPath=target/classes \
  -DserviceUniqueId=com.example.OrderService=order-service
```

Bootstrap a manifest with root overrides:

```bash
rpcctl manifest init \
  --default-env test-zk \
  --sofa-rpc-version 5.4.0 \
  --protocol bolt \
  --serialization hessian2 \
  --timeout-ms 3000
```

Minimal example:

```yaml
defaultEnv: test-zk
sofaRpcVersion: 5.4.0
protocol: bolt
serialization: hessian2
timeoutMs: 3000
envs:
  test-zk:
    mode: registry
    registryProtocol: zookeeper
    registryAddress: 127.0.0.1:2181
services:
  com.example.UserService:
    uniqueId: user-service
    methods:
      getUser:
        risk: read
        paramTypes:
          - java.lang.Long
```

## Runtime Model

The launcher is intentionally separate from the SOFARPC client runtime:

- `rpcctl-launcher.jar`: stable CLI surface.
- `rpcctl-runtime-sofa-<version>.jar`: versioned SOFARPC client runtime.

Runtime resolution order is:

1. explicit `--sofa-rpc-version`
2. selected context
3. manifest or config
4. current project version detection
5. runtime cache / bundled runtimes
6. runtime auto-download

This prevents all projects from being forced onto one SOFARPC version.

### Runtime Auto-Download

If a required runtime is missing locally, the launcher can download it into the cache:

```text
~/.cache/sofa-rpcctl/runtimes/sofa-rpc/<version>/
```

Relevant environment variables:

- `RPCCTL_RUNTIME_BASE_URL`
- `RPCCTL_RUNTIME_HOME`
- `RPCCTL_RUNTIME_CACHE_DIR`
- `RPCCTL_RUNTIME_AUTO_DOWNLOAD`
- `RPCCTL_DEBUG_RUNTIME=1`
- `RPCCTL_RUNTIME_VERBOSE=1`

The default runtime download base is:

```text
https://github.com/hex1n/sofa-rpcctl/releases/download/v<version>
```

You can also point it at a local directory or `file://` URL for offline usage.

When auto-download fails, `rpcctl` now reports the first few candidate URLs and failure reasons. Set `RPCCTL_RUNTIME_VERBOSE=1` to stream per-candidate download diagnostics to stderr.

## Compatibility Strategy

`rpcctl` is designed around explicit runtime isolation:

- Discover version from `--sofa-rpc-version`, current manifest/config, then workspace (`pom.xml` / `gradle.*`), then local/remote runtime cache.
- Do not assume one runtime works for all provider stacks. Keep one runtime per target SOFARPC version.
- For complex payloads, run with the documented SOFARPC compatibility envelope (`bolt` + `hessian2`) as a default.
- Invocation output includes version diagnostics such as `resolvedSofaRpcVersion`, `sofaRpcVersionSource`, `sofaRpcVersionFallback`, and `supportedSofaRpcVersions` when fallback or support mismatches happen.

Official SOFARPC references:

- Consumer fields: `protocol`, `serialization`, `generic`, `directUrl`, `registry` at the configuration layer.
- Server publish fields: `virtualHost`, `virtualPort` for registry-visible address control.

References:

- https://www.sofastack.tech/en/projects/sofa-rpc/configuration-common/

## Configuration

Project-level metadata is best expressed through `rpcctl-manifest.yaml`.

Legacy config still works:

- `config/rpcctl.yaml`
- `config/metadata.yaml`

Config lookup order:

1. `--config`
2. `RPCCTL_CONFIG`
3. `./config/rpcctl.yaml`
4. `~/.config/sofa-rpcctl/rpcctl.yaml`

Contexts lookup order:

1. `RPCCTL_CONTEXTS`
2. `~/.config/sofa-rpcctl/contexts.yaml`

Relative `metadataPath` and manifest references are resolved relative to the file that declared them, not the current shell directory.

## Installation

Install from source:

```bash
./scripts/install.sh
```

Build a portable release archive:

```bash
./scripts/dist.sh
```

This produces:

```text
dist/sofa-rpcctl-0.1.0.tar.gz
```

Install from an extracted release:

```bash
tar -xzf dist/sofa-rpcctl-0.1.0.tar.gz
./sofa-rpcctl-0.1.0/install.sh
```

Install from a local archive or URL:

```bash
tar -xzf sofa-rpcctl-0.1.0.tar.gz
./sofa-rpcctl-0.1.0/install-from-archive.sh /path/to/sofa-rpcctl-0.1.0.tar.gz
```

Or:

```bash
./sofa-rpcctl-0.1.0/install-from-archive.sh https://example.com/sofa-rpcctl-0.1.0.tar.gz
```

## Install On A New Machine

For a fresh machine, you usually only need:

- a working `java` runtime
- network access to the published GitHub Release, or a copied release archive
- network access to the target SOFARPC provider or registry

Online install:

```bash
curl -fsSL \
  https://github.com/hex1n/sofa-rpcctl/releases/download/v0.1.0/get-rpcctl.sh \
  | bash -s -- 0.1.0
```

The bootstrap installer verifies the release archive against the published `checksums.txt` before extracting it. Set `RPCCTL_SKIP_CHECKSUM=1` only if you intentionally want to bypass that verification.

If `~/.local/bin` is not already in `PATH`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

Sanity check:

```bash
rpcctl --help
```

Offline install from a copied release archive:

```bash
tar -xzf sofa-rpcctl-0.1.0.tar.gz
./sofa-rpcctl-0.1.0/install.sh
export PATH="$HOME/.local/bin:$PATH"
```

If you use `install-from-archive.sh`, it will verify the archive when a neighboring `.sha256` file or `checksums.txt` is available; otherwise it prints a warning and continues.

First direct call on a fresh machine:

```bash
rpcctl call \
  com.foo.UserService/getUser \
  '[123]' \
  --direct-url bolt://test-provider-host:12200 \
  --types java.lang.Long \
  --unique-id user-service \
  --sofa-rpc-version 5.4.0
```

First registry-backed call:

```bash
rpcctl call \
  com.foo.UserService/getUser \
  '[123]' \
  --registry zookeeper://test-zk-host:2181 \
  --types java.lang.Long \
  --unique-id user-service \
  --sofa-rpc-version 5.4.0
```

`rpcctl-manifest.yaml` is optional for invocation. Without a manifest, the call still works, but you must provide the target yourself and usually also provide `--types`, `--unique-id`, and sometimes `--sofa-rpc-version`.

If you want smarter behavior from any directory, do a one-time user setup:

```bash
mkdir -p ~/.config/sofa-rpcctl
cp rpcctl-manifest.yaml ~/.config/sofa-rpcctl/
rpcctl context set test \
  --manifest ~/.config/sofa-rpcctl/rpcctl-manifest.yaml \
  --current
```

After that, a shorter command can work:

```bash
rpcctl call com.foo.UserService/getUser '[123]'
```

## Release Assets

Generate a releasable asset directory:

```bash
./scripts/release.sh
```

That creates:

- `dist/release-assets/sofa-rpcctl-<version>.tar.gz`
- `dist/release-assets/rpcctl-runtime-sofa-<version>.jar`
- `dist/release-assets/get-rpcctl.sh`
- `dist/release-assets/checksums.txt`

After publishing those files to a GitHub Release, installation can be as short as:

```bash
curl -fsSL \
  https://github.com/hex1n/sofa-rpcctl/releases/download/v0.1.0/get-rpcctl.sh \
  | bash -s -- 0.1.0
```

The repository also includes a GitHub Actions workflow that builds and publishes these release assets on `v*` tags.

## E2E Smoke

The repository now includes a repeatable smoke script:

```bash
./scripts/e2e-smoke.sh
```

It requires:

- `java` and `javac`
- a Docker-compatible runtime such as Docker Desktop or OrbStack

The script:

1. builds the requested SOFARPC runtime
2. compiles local fixture providers under `e2e/fixtures/src`
3. verifies a direct `UserService#getUser(Long)` call
4. verifies a stub-aware DTO call generated from imported manifest schema
5. starts a local ZooKeeper and verifies a registry-backed complex `Map` payload call

Optional environment overrides:

- `RPCCTL_E2E_DIRECT_PORT`
- `RPCCTL_E2E_REGISTRY_PORT`
- `RPCCTL_E2E_ZK_PORT`
- `RPCCTL_E2E_ZK_CONTAINER`

## Current Scope

Implemented:

- `invoke`, `call`, `list`, `describe`, `context`, `manifest`
- `direct` and `registry` targets
- `raw`, `generic`, and metadata-driven `schema` payload classification
- stub-aware DTO invocation through `--stub-path`
- manifest and global context auto-discovery
- manifest schema import from local jars / compiled classes
- runtime version isolation
- runtime auto-download and cache
- release asset generation and bootstrap installer
- write-risk confirmation based on metadata
- normalized JSON error output with hints
- repo-native direct and registry smoke fixtures via `./scripts/e2e-smoke.sh`

Not implemented:

- production gateway mode
- universal runtime service/method discovery from SOFARPC alone

The second limitation is structural. SOFARPC does not expose a universal, project-agnostic method catalog that a standalone client can reliably introspect for every service. That is why inline invocation works without metadata, but `list` and `describe` still require a manifest or metadata catalog.
