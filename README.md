# sofa-rpcctl

This repository now uses the `greenfield/` implementation as the primary
`rpcctl`.

Current architecture:

- Go CLI control plane
- Java SOFARPC worker runtime
- local runtime cache and daemon pool

Start here:

- usage and command reference: [greenfield/README.md](./greenfield/README.md)
- implementation design: [docs/greenfield-sofarpc-cli-design.md](./docs/greenfield-sofarpc-cli-design.md)

## Quick Start

Build:

```powershell
mvn -f greenfield/runtime-worker-java/pom.xml package
go build -o greenfield/bin/rpc ./greenfield/cmd/rpc
```

Run:

```powershell
cd greenfield
go run ./cmd/rpc help
```

Or invoke directly from the repo root:

```powershell
go run ./greenfield/cmd/rpc help
```

For full usage, examples, manifest format, runtime source management, and
diagnostics, use [greenfield/README.md](./greenfield/README.md).
