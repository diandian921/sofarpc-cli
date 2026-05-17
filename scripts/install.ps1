param(
  [switch]$Uninstall
)

$ErrorActionPreference = "Stop"
$InstallRoot = if ($env:SOFARPC_HOME) { $env:SOFARPC_HOME } else { Join-Path $HOME ".sofarpc" }
$BinDir = Join-Path $InstallRoot "bin"
$LibDir = Join-Path $InstallRoot "lib"
$StateDir = Join-Path $InstallRoot "state"
$LogDir = Join-Path $InstallRoot "logs"
$CacheDir = Join-Path $InstallRoot "cache"
$ConfigFile = Join-Path $InstallRoot "config.json"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

if ($Uninstall) {
  $Cli = Join-Path $BinDir "sofarpc-cli.exe"
  if (Test-Path $Cli) {
    try { & $Cli daemon stop | Out-Null } catch {}
  }
  Remove-Item -Force -ErrorAction SilentlyContinue `
    (Join-Path $BinDir "sofarpc-cli.exe"), `
    (Join-Path $BinDir "sofarpc-mcp.exe"), `
    (Join-Path $LibDir "sofarpc-engine.jar")
  Write-Host "Uninstalled binaries and jar. Kept config, state, logs, token, and cache under $InstallRoot."
  exit 0
}

try {
  $JavaVersion = (& java -version 2>&1 | Select-String 'version' | Select-Object -First 1).ToString()
} catch {
  throw "java not found in PATH; JDK/JRE 8+ is required"
}
if ($JavaVersion -match 'version "([^"]+)"') {
  $VersionText = $Matches[1]
  $Parts = $VersionText.Split('.')
  $Major = if ($Parts[0] -eq "1" -and $Parts.Length -gt 1) { [int]$Parts[1] } else { [int]$Parts[0] }
  if ($Major -lt 8) {
    throw "Java 8+ is required, found $VersionText"
  }
}

New-Item -ItemType Directory -Force $BinDir, $LibDir, $StateDir, $LogDir, $CacheDir | Out-Null

if (!(Test-Path $ConfigFile)) {
  @'
{
  "projects": {},
  "servers": {},
  "engine": {
    "host": "127.0.0.1",
    "port": 37651,
    "javaHome": null,
    "idleTTL": "30m",
    "startTimeoutMs": 20000,
    "maxConcurrentInvokes": 8
  }
}
'@ | Set-Content -Encoding UTF8 $ConfigFile
}

$CliSrc = Join-Path $ScriptDir "sofarpc-cli.exe"
$McpSrc = Join-Path $ScriptDir "sofarpc-mcp.exe"
$JarSrc = Join-Path $ScriptDir "sofarpc-engine.jar"

if (!(Test-Path $CliSrc) -or !(Test-Path $McpSrc) -or !(Test-Path $JarSrc)) {
  throw "install.ps1 expects sofarpc-cli.exe, sofarpc-mcp.exe, and sofarpc-engine.jar next to the script. Build a release package first."
}

Copy-Item -Force $CliSrc (Join-Path $BinDir "sofarpc-cli.exe")
Copy-Item -Force $McpSrc (Join-Path $BinDir "sofarpc-mcp.exe")
Copy-Item -Force $JarSrc (Join-Path $LibDir "sofarpc-engine.jar")

Write-Host "Installed:"
Write-Host "  $(Join-Path $BinDir "sofarpc-cli.exe")"
Write-Host "  $(Join-Path $BinDir "sofarpc-mcp.exe")"
Write-Host "  $(Join-Path $LibDir "sofarpc-engine.jar")"
Write-Host "Add $BinDir to PATH if needed."
