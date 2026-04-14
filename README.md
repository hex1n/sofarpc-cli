# sofarpc-cli

CLI for invoking SOFARPC services.

Architecture (deliberately polyglot, each language kept to what it does best):

- **Go** — CLI control plane, daemon lifecycle, runtime cache. Fast cold
  start, clean Windows subprocess semantics, single-binary distribution.
- **Java** — SOFARPC worker runtime plus the Spoon-based facade indexer.

Start here:

- usage and command reference: [docs/usage.md](./docs/usage.md)
- design notes: [docs/sofarpc-cli-design.md](./docs/sofarpc-cli-design.md)

## Runtime Workflow

```mermaid
flowchart LR
    A[Run CLI command] --> B[internal cli parse command and resolve manifest context]
    B --> C[ResolveSpec and daemon key]
    C --> D[start or reuse runtime worker daemon]
    D --> E[TCP socket to Java runtime]
    B -->|call command| F[Prepare invocation request service method args targets]
    F --> G{Need schema inference}
    G -->|yes| H[DescribeService with action describe over daemon]
    G -->|no| I[Direct invoke request]
    H --> E
    I --> E
    B -->|describe command| H
    E --> J{request action}
    J -->|describe| K[WorkerMain describe cache by service in memory]
    J -->|other| L[WorkerMain invoke path]
    K --> M[Return ServiceSchema result]
    L --> M
    M --> N{response ok}
    N -->|error| O[Print structured diagnostics and return error]
    N -->|success| P[Format output or return result]
```

Notes:

- schema cache is now kept in the runtime daemon JVM memory, shared by CLI processes using the same daemon key;
- cache is process-lifetime only: no local schema files are written.
- schema refresh is supported via `refresh`/`no-cache` (goes into daemon describe request).

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

## Claude Code skills

The repo ships a `call-rpc` Claude Code skill that triggers `sofarpc call` for
SOFABoot projects. Install once at user scope:

```powershell
sofarpc skills install          # copies skills/call-rpc -> ~/.claude/skills/
sofarpc skills where            # show source / target paths
```

The skill intentionally does not handle project bootstrap, index generation,
cases, or result interpretation. It is a thin wrapper around the `sofarpc call`
command.

For full usage, examples, manifest format, runtime source management, and
diagnostics, see [docs/usage.md](./docs/usage.md).
