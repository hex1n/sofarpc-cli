param(
    [string]$Version = "latest"
)

$package = "github.com/hex1n/sofarpc-cli/cmd/sofarpc-mcp@$Version"
Write-Host "Installing $package"
go install $package
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}

Write-Host ""
Write-Host "Installed sofarpc-mcp."
Write-Host "Version:"
$cmd = Get-Command sofarpc-mcp -ErrorAction SilentlyContinue
if ($cmd) {
    sofarpc-mcp version
} else {
    Write-Host "  sofarpc-mcp is not on PATH yet; ensure GOPATH/bin or GOBIN is configured."
}

Write-Host ""
Write-Host "Register the MCP server for this user:"
Write-Host "  sofarpc-mcp setup --scope=user"
Write-Host ""
Write-Host "Write project-level target config from a Java project checkout:"
Write-Host "  sofarpc-mcp setup --scope=project --project-root . --local --direct-url=bolt://host:12200 --allowed-services=com.foo.UserFacade"
Write-Host "  sofarpc-mcp setup --scope=project --project-root . --shared --registry-address=zookeeper://host:2181 --allowed-services=com.foo.UserFacade"
Write-Host ""
Write-Host "Real invoke is disabled by default. Enable it only for dev/test targets:"
Write-Host "  sofarpc-mcp setup --scope=user --allow-invoke --allowed-target-hosts=127.0.0.1"
Write-Host ""
Write-Host "Source-contract information and service allowlists are loaded lazily from the resolved project root."
