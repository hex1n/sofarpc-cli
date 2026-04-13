# sofarpc-cli

CLI for invoking SOFARPC services.

Architecture:

- Go CLI control plane
- Java SOFARPC worker runtime
- local runtime cache and daemon pool

Start here:

- usage and command reference: [docs/usage.md](./docs/usage.md)
- design notes: [docs/sofarpc-cli-design.md](./docs/sofarpc-cli-design.md)

## Quick Start

Build:

```powershell
mvn -f runtime-worker-java/pom.xml package
go build -o bin/sofarpc ./cmd/sofarpc
```

Run:

```powershell
go run ./cmd/sofarpc help
```

For full usage, examples, manifest format, runtime source management, and
diagnostics, see [docs/usage.md](./docs/usage.md).
