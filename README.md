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
- Uses generic invocation so callers do not need business DTO classes on the classpath.
- Accepts JSON arguments and converts complex objects into `GenericObject`.
- Splits the CLI into a stable launcher and versioned SOFARPC runtimes.
- Supports explicit `--sofa-rpc-version`, automatic version inference, runtime auto-download, and local runtime caching.
- Auto-discovers `rpcctl-manifest.yaml` from the current project or `~/.config/sofa-rpcctl/`.
- Supports reusable global contexts via `rpcctl context`.
- Generates manifests from existing `config/rpcctl.yaml` and `config/metadata.yaml`.
- Produces release assets and a bootstrap installer so the CLI can be installed without copying the source tree.

## Commands

- `invoke`: full-form method invocation.
- `call`: shorter syntax for `invoke`.
- `list`: list services from metadata or a manifest.
- `describe`: show one service from metadata or a manifest.
- `context`: manage named user profiles in `~/.config/sofa-rpcctl/contexts.yaml`.
- `manifest generate|init`: generate `rpcctl-manifest.yaml` from existing config/metadata or create a skeleton.

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

The default runtime download base is:

```text
https://github.com/hex1n/sofa-rpcctl/releases/download/v<version>
```

You can also point it at a local directory or `file://` URL for offline usage.

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

## Current Scope

Implemented:

- `invoke`, `call`, `list`, `describe`, `context`, `manifest`
- `direct` and `registry` targets
- manifest and global context auto-discovery
- runtime version isolation
- runtime auto-download and cache
- release asset generation and bootstrap installer
- write-risk confirmation based on metadata
- normalized JSON error output with hints

Not implemented:

- production gateway mode
- universal runtime service/method discovery from SOFARPC alone

The second limitation is structural. SOFARPC does not expose a universal, project-agnostic method catalog that a standalone client can reliably introspect for every service. That is why inline invocation works without metadata, but `list` and `describe` still require a manifest or metadata catalog.
