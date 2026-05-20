# Network bootstrap. Designed to be served and piped to PowerShell:
#
#   iwr -useb https://raw.githubusercontent.com/diandian921/sofarpc-cli/main/scripts/install.ps1 | iex
#   & ([scriptblock]::Create((iwr -useb https://raw.githubusercontent.com/diandian921/sofarpc-cli/main/scripts/install.ps1))) codex
#
# Acquires the release zip (detect arch, download, verify SHA256, extract)
# and hands control to .\sofarpc.exe install <host>. Knows nothing about
# host config formats and duplicates no install logic.
#
# Falls back to a local from-repo build when run inside a working tree that
# contains cmd/sofarpc/.
$ErrorActionPreference = "Stop"

$Repo = "diandian921/sofarpc-cli"

function Show-Usage {
@"
Usage: install.ps1 [-Version vX.Y.Z] [claude|codex|all]

  (no host)        Install only; do not register any host.
  claude           Install and register the MCP server with Claude Code.
  codex            Install and register the MCP server with Codex.
  all              Install and register both.
  -Version TAG     Pin a release tag instead of resolving the latest.
"@
}

# Parse args manually so we can also be invoked via iex with arguments.
$Version = $null
$Host = $null
$i = 0
while ($i -lt $args.Count) {
  switch ($args[$i]) {
    "-h"        { Show-Usage; exit 0 }
    "--help"    { Show-Usage; exit 0 }
    "-Version"  { $i++; $Version = $args[$i] }
    "--version" { $i++; $Version = $args[$i] }
    "claude"    { $Host = "claude" }
    "codex"     { $Host = "codex"  }
    "all"       { $Host = "all"    }
    default {
      Write-Error "unknown argument $($args[$i])"
      Show-Usage
      exit 2
    }
  }
  $i++
}

# Dev path: invoked from a repo checkout that has cmd/sofarpc/.
$ScriptDir = if ($MyInvocation.MyCommand.Path) { Split-Path -Parent $MyInvocation.MyCommand.Path } else { $null }
if ($ScriptDir) {
  $RepoRoot = Split-Path -Parent $ScriptDir
  if ((Test-Path (Join-Path $RepoRoot "cmd/sofarpc")) -and (Get-Command go -ErrorAction SilentlyContinue)) {
    try { $BinVersion = (git -C $RepoRoot describe --tags --match 'v*' --always 2>$null) } catch { $BinVersion = "dev" }
    if (-not $BinVersion) { $BinVersion = "dev" }
    $BuildDir = New-Item -ItemType Directory -Path (Join-Path $env:TEMP ([System.Guid]::NewGuid())) -Force
    $Cli = Join-Path $BuildDir "sofarpc.exe"
    Push-Location $RepoRoot
    try {
      & go build -ldflags "-X main.BuildVersion=$BinVersion" -o $Cli ./cmd/sofarpc
      if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    } finally { Pop-Location }
    if ($Host) { & $Cli install $Host } else { & $Cli install }
    exit $LASTEXITCODE
  }
}

# Network path: detect arch.
$Arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture) {
  "X64"   { "amd64" }
  "Arm64" { "arm64" }
  default { throw "unsupported architecture $($_)" }
}

# Resolve latest tag via the redirect.
if (-not $Version) {
  $resp = Invoke-WebRequest -UseBasicParsing -MaximumRedirection 0 -Uri "https://github.com/$Repo/releases/latest" -ErrorAction SilentlyContinue
  $location = $null
  if ($resp.Headers["Location"]) { $location = $resp.Headers["Location"] }
  if (-not $location) {
    $resp = Invoke-WebRequest -UseBasicParsing -Uri "https://github.com/$Repo/releases/latest"
    $location = $resp.BaseResponse.ResponseUri.AbsoluteUri
  }
  if ($location -match "/tag/(.+)$") { $Version = $matches[1] }
  if (-not $Version) { throw "could not resolve latest release tag" }
}

$Archive = "sofarpc-$Version-windows-$Arch.zip"
$BaseUrl = "https://github.com/$Repo/releases/download/$Version"
$Tmp     = New-Item -ItemType Directory -Path (Join-Path $env:TEMP ([System.Guid]::NewGuid())) -Force

try {
  Write-Host "Downloading $Archive ..."
  Invoke-WebRequest -UseBasicParsing -Uri "$BaseUrl/$Archive"   -OutFile (Join-Path $Tmp $Archive)
  Invoke-WebRequest -UseBasicParsing -Uri "$BaseUrl/SHA256SUMS" -OutFile (Join-Path $Tmp "SHA256SUMS")

  Write-Host "Verifying SHA256 ..."
  $expected = $null
  Get-Content (Join-Path $Tmp "SHA256SUMS") | ForEach-Object {
    $parts = ($_ -split "\s+")
    if ($parts.Count -ge 2 -and ($parts[1] -eq $Archive -or $parts[1] -eq "*$Archive")) {
      $expected = $parts[0]
    }
  }
  if (-not $expected) { throw "$Archive missing from SHA256SUMS" }
  $actual = (Get-FileHash -Algorithm SHA256 (Join-Path $Tmp $Archive)).Hash.ToLower()
  if ($actual -ne $expected.ToLower()) { throw "checksum mismatch for $Archive" }

  Write-Host "Extracting ..."
  Expand-Archive -Path (Join-Path $Tmp $Archive) -DestinationPath $Tmp -Force
  $Cli = Join-Path $Tmp "sofarpc-$Version-windows-$Arch/sofarpc.exe"
  if (-not (Test-Path $Cli)) { throw "archive did not contain sofarpc.exe" }

  if ($Host) { & $Cli install $Host } else { & $Cli install }
  exit $LASTEXITCODE
} finally {
  Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $Tmp
}
