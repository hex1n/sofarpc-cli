#!/usr/bin/env sh
set -eu

VERSION="${1:-latest}"
PACKAGE="github.com/hex1n/sofarpc-cli/cmd/sofarpc-mcp@${VERSION}"

echo "Installing ${PACKAGE}"
go install "${PACKAGE}"

echo
echo "Installed sofarpc-mcp."
echo
echo "Set project-level MCP env:"
echo "  SOFARPC_PROJECT_ROOT=/abs/path/to/project"
echo "  SOFARPC_DIRECT_URL=bolt://host:12200"
echo "  SOFARPC_PROTOCOL=bolt"
echo "  SOFARPC_SERIALIZATION=hessian2"
echo
echo "On startup, sofarpc-mcp will scan .java files under SOFARPC_PROJECT_ROOT"
echo "to populate describe-time contract information."
