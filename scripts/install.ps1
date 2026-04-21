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
Write-Host ""
Write-Host "Set project-level MCP env:"
Write-Host "  SOFARPC_PROJECT_ROOT=C:\path\to\project"
Write-Host "  SOFARPC_DIRECT_URL=bolt://host:12200"
Write-Host "  SOFARPC_PROTOCOL=bolt"
Write-Host "  SOFARPC_SERIALIZATION=hessian2"
Write-Host ""
Write-Host "On startup, sofarpc-mcp will scan .java files under SOFARPC_PROJECT_ROOT"
Write-Host "to populate describe-time contract information."
