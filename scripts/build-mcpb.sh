#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
OUT_DIR=${OUT_DIR:-"$ROOT_DIR/dist/mcpb"}
WORK_DIR="$OUT_DIR/.work"
TARGETS=${TARGETS:-"darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64"}
PACKAGE="./cmd/sofarpc-mcp"

derive_mcpb_version() {
	raw=$(printf "%s" "$1" | sed 's/^v//')
	case "$raw" in
		[0-9]*.[0-9]*.[0-9]*)
			printf "%s" "$raw"
			;;
		*)
			printf "0.0.0-dev"
			;;
	esac
}

artifact_version() {
	printf "%s" "$1" | sed 's/^v//; s#[^A-Za-z0-9._-]#_#g'
}

git_value() {
	git -C "$ROOT_DIR" "$@" 2>/dev/null || true
}

platform_for_goos() {
	case "$1" in
		darwin)
			printf "darwin"
			;;
		linux)
			printf "linux"
			;;
		windows)
			printf "win32"
			;;
		*)
			printf "unsupported"
			;;
	esac
}

binary_for_goos() {
	case "$1" in
		windows)
			printf "sofarpc-mcp.exe"
			;;
		*)
			printf "sofarpc-mcp"
			;;
	esac
}

write_bundle_readme() {
	target=$1
	path=$2
	cat >"$path" <<EOF
# SOFARPC MCP Bundle

This MCPB contains the platform-specific sofarpc-mcp binary for $target.

The server speaks MCP over stdio. Use the tools with explicit project/cwd
inputs, or open a project first and reuse the returned sessionId.

Real SOFARPC invoke is disabled unless the bundle configuration sets
SOFARPC_ALLOW_INVOKE=true. Keep allowed target hosts narrow for development and
test environments.
EOF
}

write_manifest() {
	path=$1
	platform=$2
	binary_name=$3
	cat >"$path" <<EOF
{
  "manifest_version": "0.3",
  "name": "sofarpc-mcp",
  "display_name": "SOFARPC MCP",
  "version": "$MCPB_VERSION",
  "description": "Agent-first local MCP server for SOFARPC generic invoke.",
  "long_description": "SOFARPC MCP provides project bootstrap, target diagnostics, contract-aware describe/invoke planning, guarded real invoke, and replay for SOFARPC generic calls. It runs locally over stdio and expects Java project context through explicit tool inputs or MCP sessions.",
  "author": {
    "name": "hex1n"
  },
  "repository": {
    "type": "git",
    "url": "https://github.com/hex1n/sofarpc-cli"
  },
  "homepage": "https://github.com/hex1n/sofarpc-cli",
  "documentation": "https://github.com/hex1n/sofarpc-cli#readme",
  "support": "https://github.com/hex1n/sofarpc-cli/issues",
  "server": {
    "type": "binary",
    "entry_point": "server/$binary_name",
    "mcp_config": {
      "command": "\${__dirname}\${/}server\${/}$binary_name",
      "env": {
        "SOFARPC_ALLOW_INVOKE": "\${user_config.allow_invoke}",
        "SOFARPC_ALLOW_TARGET_OVERRIDE": "\${user_config.allow_target_override}",
        "SOFARPC_ALLOWED_TARGET_HOSTS": "\${user_config.allowed_target_hosts}",
        "SOFARPC_SESSION_PLAN_MAX_BYTES": "\${user_config.session_plan_max_bytes}",
        "SOFARPC_MAX_RESPONSE_BYTES": "\${user_config.max_response_bytes}"
      }
    }
  },
  "tools": [
    {
      "name": "sofarpc_init_project",
      "description": "Initialize .sofarpc project config and allowedServices for a Java project."
    },
    {
      "name": "sofarpc_open",
      "description": "Open a SOFARPC project workspace and create an MCP session."
    },
    {
      "name": "sofarpc_target",
      "description": "Resolve and diagnose the effective SOFARPC target."
    },
    {
      "name": "sofarpc_describe",
      "description": "Resolve contract overloads and build argument skeletons."
    },
    {
      "name": "sofarpc_invoke",
      "description": "Build a SOFARPC invoke plan and optionally execute it."
    },
    {
      "name": "sofarpc_replay",
      "description": "Replay a captured or literal SOFARPC invoke plan."
    },
    {
      "name": "sofarpc_doctor",
      "description": "Run structured diagnostics for project, target, and invoke readiness."
    }
  ],
  "tools_generated": false,
  "prompts_generated": false,
  "keywords": [
    "sofarpc",
    "mcp",
    "rpc",
    "bolt",
    "hessian2"
  ],
  "compatibility": {
    "platforms": [
      "$platform"
    ]
  },
  "user_config": {
    "allow_invoke": {
      "type": "boolean",
      "title": "Enable real invoke",
      "description": "Allow non-dry-run SOFARPC calls. Leave disabled for planning, describe, replay dry-runs, and diagnostics.",
      "required": false,
      "default": false
    },
    "allow_target_override": {
      "type": "boolean",
      "title": "Allow target override",
      "description": "Allow per-call directUrl overrides instead of requiring the project .sofarpc target.",
      "required": false,
      "default": false
    },
    "allowed_target_hosts": {
      "type": "string",
      "title": "Allowed target hosts",
      "description": "Comma-separated hosts or host:port values allowed for real direct invoke. Use * only for isolated test environments.",
      "required": false,
      "default": "127.0.0.1,localhost"
    },
    "session_plan_max_bytes": {
      "type": "number",
      "title": "Session plan max bytes",
      "description": "Maximum JSON plan size retained in memory for sessionId replay. Set 0 to disable this bound.",
      "required": false,
      "default": 1048576,
      "min": 0
    },
    "max_response_bytes": {
      "type": "number",
      "title": "Max response bytes",
      "description": "Maximum SOFARPC response body accepted by the direct transport.",
      "required": false,
      "default": 16777216,
      "min": 1
    }
  }
}
EOF
}

if ! command -v zip >/dev/null 2>&1; then
	echo "zip is required to create .mcpb archives" >&2
	exit 1
fi

VERSION=${VERSION:-$(git_value describe --tags --always --dirty)}
if [ -z "$VERSION" ]; then
	VERSION=dev
fi
MCPB_VERSION=${MCPB_VERSION:-$(derive_mcpb_version "$VERSION")}
COMMIT=${COMMIT:-$(git_value rev-parse --short HEAD)}
if [ -z "$COMMIT" ]; then
	COMMIT=unknown
fi
DATE=${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}
ARTIFACT_VERSION=$(artifact_version "$VERSION")

mkdir -p "$OUT_DIR"
rm -rf "$WORK_DIR"
mkdir -p "$WORK_DIR"
trap 'rm -rf "$WORK_DIR"' EXIT INT TERM

cd "$ROOT_DIR"

for target in $TARGETS; do
	goos=${target%/*}
	goarch=${target#*/}
	platform=$(platform_for_goos "$goos")
	if [ "$platform" = "unsupported" ]; then
		echo "unsupported target: $target" >&2
		exit 1
	fi
	binary_name=$(binary_for_goos "$goos")
	stage="$WORK_DIR/sofarpc-mcp-$ARTIFACT_VERSION-$goos-$goarch"
	artifact="$OUT_DIR/sofarpc-mcp-$ARTIFACT_VERSION-$goos-$goarch.mcpb"

	mkdir -p "$stage/server"
	ldflags="-s -w -X main.version=$VERSION -X main.commit=$COMMIT -X main.date=$DATE"
	echo "Building $target"
	env CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "$ldflags" -o "$stage/server/$binary_name" "$PACKAGE"
	if [ "$goos" != "windows" ]; then
		chmod 0755 "$stage/server/$binary_name"
	fi

	write_manifest "$stage/manifest.json" "$platform" "$binary_name"
	write_bundle_readme "$target" "$stage/README.md"
	rm -f "$artifact"
	(
		cd "$stage"
		zip -qr "$artifact" manifest.json README.md server
	)
	echo "Wrote $artifact"
done
