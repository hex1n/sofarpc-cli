#!/usr/bin/env sh
set -eu

VERSION="${1:-latest}"
PACKAGE="github.com/hex1n/sofarpc-cli/cmd/sofarpc-mcp@${VERSION}"

echo "Installing ${PACKAGE}"
go install "${PACKAGE}"

echo
echo "Installed sofarpc-mcp."
echo "Version:"
if command -v sofarpc-mcp >/dev/null 2>&1; then
  sofarpc-mcp version || true
else
  echo "  sofarpc-mcp is not on PATH yet; ensure GOPATH/bin or GOBIN is configured."
fi

echo
echo "Set project-level MCP env:"
echo "  SOFARPC_PROJECT_ROOT=/abs/path/to/project"
echo "  SOFARPC_DIRECT_URL=bolt://host:12200"
echo "  SOFARPC_PROTOCOL=bolt"
echo "  SOFARPC_SERIALIZATION=hessian2"
echo
echo "Real invoke is disabled by default. Enable it only for dev/test targets:"
echo "  SOFARPC_ALLOW_INVOKE=true"
echo "  SOFARPC_ALLOWED_SERVICES=com.foo.UserFacade,com.foo.OrderFacade"
echo
echo "On startup, sofarpc-mcp will scan .java files under SOFARPC_PROJECT_ROOT"
echo "to populate describe-time contract information."
