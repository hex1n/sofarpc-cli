# Changelog

All notable changes to this project are recorded here.

## Unreleased

### Added

- Added `sofarpc-mcp version` and `sofarpc-mcp version -json` for release and support diagnostics.
- Added build-time version metadata injection through `-ldflags` for version, commit, and build date.
- Added MCP server version injection so the MCP implementation metadata matches the CLI build version.
- Added release-oriented documentation covering tag installs, version inspection, and release checklist.

### Changed

- Recommended tag-based installs for repeatable environments while keeping `@latest` available for development snapshots.

## v0.1.0 - planned

Initial hardening release target.

### Added

- Pure-Go SOFARPC direct generic invoke path for `direct + bolt + hessian2`.
- Six MCP tools: `sofarpc_open`, `sofarpc_target`, `sofarpc_describe`, `sofarpc_invoke`, `sofarpc_replay`, and `sofarpc_doctor`.
- Source-first Java contract scan for describe/skeleton generation.
- Trusted invoke mode when local contract information is unavailable.
- Real-invoke safety guardrails: explicit invoke opt-in, optional service allowlist, and bounded `@file` args.
- Shared target address parsing for probe and direct invoke.
- Structured errcode recovery hints for agent workflows.
