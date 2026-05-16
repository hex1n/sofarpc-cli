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
echo "Register the MCP server for this user:"
echo "  sofarpc-mcp setup --scope=user"
echo
echo "Write project-level target config from a Java project checkout:"
echo "  sofarpc-mcp setup --scope=project --project-root . --local --direct-url=bolt://host:12200"
echo "  sofarpc-mcp setup --scope=project --project-root . --shared --registry-address=zookeeper://host:2181"
echo
echo "Real invoke is disabled by default. Enable it only for dev/test targets:"
echo "  sofarpc-mcp setup --scope=user --allow-invoke --allowed-services=com.foo.UserFacade,com.foo.OrderFacade"
echo
echo "Source-contract information is loaded lazily from the project root when a tool needs it."
