param(
  [switch]$Uninstall
)

$ErrorActionPreference = "Stop"
$InstallRoot = if ($env:SOFARPC_HOME) { $env:SOFARPC_HOME } else { Join-Path $HOME ".sofarpc" }
$BinDir = Join-Path $InstallRoot "bin"
$CacheDir = Join-Path $InstallRoot "cache"
$ConfigFile = Join-Path $InstallRoot "config.json"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

if ($Uninstall) {
  Remove-Item -Force -ErrorAction SilentlyContinue `
    (Join-Path $BinDir "sofarpc-cli.exe"), `
    (Join-Path $BinDir "sofarpc-mcp.exe")
  Write-Host "Uninstalled binaries. Kept config and cache under $InstallRoot."
  exit 0
}

New-Item -ItemType Directory -Force $BinDir, $CacheDir | Out-Null

if (!(Test-Path $ConfigFile)) {
  @'
{
  "projects": {},
  "servers": {}
}
'@ | Set-Content -Encoding UTF8 $ConfigFile
}

$CliSrc = Join-Path $ScriptDir "sofarpc-cli.exe"
$McpSrc = Join-Path $ScriptDir "sofarpc-mcp.exe"

if (!(Test-Path $CliSrc) -or !(Test-Path $McpSrc)) {
  throw "install.ps1 expects sofarpc-cli.exe and sofarpc-mcp.exe next to the script. Build a release package first."
}

Copy-Item -Force $CliSrc (Join-Path $BinDir "sofarpc-cli.exe")
Copy-Item -Force $McpSrc (Join-Path $BinDir "sofarpc-mcp.exe")

Write-Host "Installed:"
Write-Host "  $(Join-Path $BinDir "sofarpc-cli.exe")"
Write-Host "  $(Join-Path $BinDir "sofarpc-mcp.exe")"
Write-Host "Add $BinDir to PATH if needed."
