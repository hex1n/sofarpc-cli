# sofa-rpcctl

`sofa-rpcctl` is a standalone CLI for invoking SOFABoot / SOFARPC services from the command line without depending on business interface jars.

The project is designed around three constraints:

1. It must be portable across different SOFABoot projects.
2. It must speak native SOFARPC instead of forcing every service to expose a second REST contract.
3. It must stay honest about what SOFARPC cannot discover universally at runtime.

## What It Does

- Invokes SOFARPC services through `directUrl` or a registry.
- Uses generic invocation so the caller does not need business DTO classes on the classpath.
- Accepts JSON arguments and converts complex objects into `GenericObject`.
- Keeps optional metadata in a separate YAML file for `list`, `describe`, and safer write-call confirmation.

## Current Scope

This implementation is a portable CLI MVP:

- Implemented: `invoke`, `list`, `describe`
- Implemented: `direct` and `registry` environment modes
- Implemented: write-risk confirmation based on metadata
- Not implemented: production gateway mode
- Not implemented: universal runtime method discovery

The last limitation is structural, not accidental. SOFARPC does not provide a universal, project-agnostic method catalog that a standalone client can reliably introspect for any service. That is why metadata is optional for invocation but required for `list` / `describe`.

## Build

```bash
./scripts/build.sh
```

Override the bundled SOFARPC version when you need to align with a specific provider stack:

```bash
./scripts/build.sh 5.4.0
```

Or:

```bash
SOFA_RPC_VERSION=5.4.0 ./scripts/build.sh
```

## Run

```bash
./bin/rpcctl invoke \
  --env local-direct \
  --service com.example.UserService \
  --method getUser \
  --types java.lang.Long \
  --args '[123]'
```

Metadata-backed invocation:

```bash
./bin/rpcctl invoke \
  --env test-zk \
  --service com.example.UserService \
  --method getUser \
  --args '[123]'
```

Nested complex object with explicit type hints:

```bash
./bin/rpcctl invoke \
  --env test-zk \
  --service com.example.UserService \
  --method updateUser \
  --args '[
    {
      "@type": "com.example.UserUpdateRequest",
      "id": 123,
      "profile": {
        "@type": "com.example.UserProfile",
        "nickname": "neo"
      }
    }
  ]'
```

List services from metadata:

```bash
./bin/rpcctl list
```

Describe one service from metadata:

```bash
./bin/rpcctl describe --service com.example.UserService
```

Install it from source:

```bash
./scripts/install.sh
```

Then use it from any terminal directory:

```bash
rpcctl invoke \
  --env test-zk \
  --service com.example.UserService \
  --method getUser \
  --args '[123]'
```

Build a release artifact that can be copied or hosted without the source tree:

```bash
./scripts/dist.sh
```

That produces:

```text
dist/sofa-rpcctl-0.1.0.tar.gz
```

Install from the extracted release package:

```bash
tar -xzf dist/sofa-rpcctl-0.1.0.tar.gz
./sofa-rpcctl-0.1.0/install.sh
```

Install directly from a local archive or remote URL:

```bash
./sofa-rpcctl-0.1.0/install-from-archive.sh /path/to/sofa-rpcctl-0.1.0.tar.gz
```

Or:

```bash
./sofa-rpcctl-0.1.0/install-from-archive.sh https://example.com/sofa-rpcctl-0.1.0.tar.gz
```

## Configuration

`config/rpcctl.yaml` stores environment definitions. `config/metadata.yaml` is optional but recommended.

Config lookup order:

1. `--config`
2. `RPCCTL_CONFIG`
3. `./config/rpcctl.yaml`
4. `~/.config/sofa-rpcctl/rpcctl.yaml`

Relative `metadataPath` values are resolved relative to the chosen config file, not the shell working directory.

## Distribution Model

The source project is no longer the install unit. The install unit is the generated release archive:

- `bin/rpcctl`
- `lib/sofa-rpcctl.jar`
- `share/sofa-rpcctl/*.yaml`
- `install.sh`
- `install-from-archive.sh`

That means you can:

1. build once
2. keep only the `tar.gz`
3. install on another machine without copying the repository

To make it literally “curl-like” for other users, you still need to host the generated `tar.gz` somewhere reachable. Once hosted, the installation path is archive-based instead of source-tree-based.

Environment modes:

- `direct`: use `directUrl`, for example `bolt://127.0.0.1:12200`
- `registry`: use `registryProtocol` + `registryAddress`, or a full `registryAddress` URI

Recommended defaults for broad compatibility:

- protocol: `bolt`
- serialization: `hessian2`
- Java: `8`

## Data Model Rules

- `--types` is a comma-separated list.
- `--args` is always a JSON array.
- Complex objects are converted into SOFARPC `GenericObject`.
- Nested complex fields should carry `@type` when they are not plain `Map` values.
- Array types can be written as `java.lang.String[]` instead of JVM descriptor form.

## Operational Notes

- Write methods should be described in metadata with `risk: write` or `risk: dangerous`.
- `risk: write` and `risk: dangerous` require `--confirm`.
- For version-sensitive providers, rebuild with a matching `sofa-rpc.version`.

## Layout

```text
sofa-rpcctl/
  bin/rpcctl
  config/rpcctl.yaml
  config/metadata.yaml
  scripts/build.sh
  src/main/java/com/hex1n/sofarpcctl/...
```
