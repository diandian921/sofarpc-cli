# Thin bootstrap. All install logic lives in `sofarpc self-install`; this
# script only locates/builds the binaries and hands off to it, calling the
# binary by absolute path (never PATH-resolved) to avoid the self-install
# PATH trap.
$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

$Cli = Join-Path $ScriptDir "sofarpc.exe"
$Mcp = Join-Path $ScriptDir "sofarpc-mcp.exe"

if ((Test-Path $Cli) -and (Test-Path $Mcp)) {
  & $Cli self-install --mcp-path $Mcp @args
  exit $LASTEXITCODE
}

if (!(Get-Command go -ErrorAction SilentlyContinue)) {
  throw "go not found and no packaged binaries present next to install.ps1"
}
function Invoke-CheckedNative {
  param(
    [string]$Description,
    [scriptblock]$Command
  )
  & $Command
  if ($LASTEXITCODE -ne 0) {
    throw "$Description failed with exit code $LASTEXITCODE"
  }
}
$RepoRoot = Split-Path -Parent $ScriptDir
# Module is at repo root: release tags are vX.Y.Z.
try { $Version = (git -C $RepoRoot describe --tags --match 'v*' --always 2>$null) } catch { $Version = "dev" }
if (-not $Version) { $Version = "dev" }
$BuildDir = New-Item -ItemType Directory -Path (Join-Path $env:TEMP ([System.Guid]::NewGuid())) -Force
$Cli = Join-Path $BuildDir "sofarpc.exe"
$Mcp = Join-Path $BuildDir "sofarpc-mcp.exe"
$PushedLocation = $false
$ExitCode = 1
try {
  Push-Location $RepoRoot
  $PushedLocation = $true
  Invoke-CheckedNative "go build sofarpc" { go build -ldflags "-X main.BuildVersion=$Version" -o $Cli ./cmd/sofarpc }
  Invoke-CheckedNative "go build sofarpc-mcp" { go build -ldflags "-X main.BuildVersion=$Version" -o $Mcp ./cmd/sofarpc-mcp }
  Pop-Location
  $PushedLocation = $false
  & $Cli self-install --mcp-path $Mcp @args
  $ExitCode = $LASTEXITCODE
} finally {
  if ($PushedLocation) {
    Pop-Location
  }
  Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $BuildDir
}
exit $ExitCode
