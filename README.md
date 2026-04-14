# sofarpc-cli

CLI for invoking SOFARPC services.

Architecture (deliberately polyglot, each language kept to what it does best):

- **Go** — CLI control plane, daemon lifecycle, runtime cache. Fast cold
  start, clean Windows subprocess semantics, single-binary distribution.
- **Java** — SOFARPC worker runtime, version-matched to the target.
- **`sofarpc_cli` Python package** — shared helpers for the bundled
  Claude Code skills and user-written scripts. Not a planned replacement
  for the Go CLI.

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

## Claude Code skills

The repo ships a `call-rpc` Claude Code skill that drives facade
invocation and result validation on any SOFABoot project. Install once at user scope:

```powershell
sofarpc skills install          # copies skills/call-rpc -> ~/.claude/skills/
sofarpc skills where            # show source / target paths
```

Per-project state lives primarily at `<project>/.sofarpc/`; legacy projects may
still resolve to `<project>/.claude/rpc-test/`. The Python helpers under the
skill import from the `sofarpc_cli` package — the shared Python library; when
the CLI is on `PATH` or `SOFARPC_HOME` is set, the install automatically writes
a pointer so no extra pip step is required.

For full usage, examples, manifest format, runtime source management, and
diagnostics, see [docs/usage.md](./docs/usage.md).
