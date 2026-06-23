# sofarpc-cli Agent Guide

## Project Context

- Agent-first local MCP server for SOFARPC generic invoke.
- Main entry point: `cmd/sofarpc-mcp`.
- Read `README.md` and `docs/architecture.md` before changing transport,
  replay, MCP handlers, setup, config, or source-contract behavior.
- Preserve unrelated dirty worktree changes, especially local IDE metadata.

## Safety Boundary

- Real SOFARPC invokes are disabled by default and require explicit opt-in.
- Do not connect to non-local business SOFARPC providers or change user/client
  MCP config unless the user explicitly approves the exact target/action.
- Do not commit generated binaries, local `.sofarpc/config.local.json`, logs,
  or machine-local config.

## Verification

Use the narrowest relevant check first.

- Focused package test: `go test ./<package>`
- Format touched Go files: `gofmt -w <files>`
- Standard broad checks: `go vet ./...`, `go test -race ./...`, `go build ./...`
- Optional local e2e smoke: `go test -tags=e2e ./tests/e2e/...`
- Docs-only: inspect Markdown and run `git diff --check`

Only claim live-provider or external MCP-client interoperability when that
specific smoke test was actually run.

## Final Response

End with changed files, verification commands/results, unverified gaps, and
residual risks. Do not commit or push unless explicitly asked.
