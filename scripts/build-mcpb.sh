#!/usr/bin/env bash
# Build per-platform MCPB bundles (Anthropic MCP Bundle, manifest spec 0.3) for
# one-click install into Claude Desktop. A .mcpb is a ZIP of:
#
#   manifest.json          # spec 0.3, server.type=binary
#   README.md              # short bundle note
#   server/sofarpc[.exe]   # the cross-compiled MCP server binary
#
# Each bundle is single-platform (native binary, no wrong-arch payload), mirror-
# ing scripts/package.sh. Packing uses the official `mcpb` CLI when present (it
# validates the manifest); otherwise it falls back to `zip` — a .mcpb is just a
# ZIP, so the result is identical and no Node toolchain is required.
#
#   bash scripts/build-mcpb.sh
#   PLATFORMS="darwin/arm64" bash scripts/build-mcpb.sh   # subset
#   MCPB_VERSION=0.2.0 VERSION=v0.2.0 bash scripts/build-mcpb.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# VERSION stamps the binary (full git describe). MCPB_VERSION is the manifest
# "version" and must be plain semver, so it defaults to the nearest vX.Y.Z tag.
VERSION="${VERSION:-$(git -C "$REPO_ROOT" describe --tags --match 'v*' --always 2>/dev/null || echo dev)}"
DEFAULT_MCPB_VERSION="$(git -C "$REPO_ROOT" describe --tags --match 'v*' --abbrev=0 2>/dev/null || echo 0.0.0)"
MCPB_VERSION="${MCPB_VERSION:-${DEFAULT_MCPB_VERSION#v}}"

DIST_DIR="$REPO_ROOT/dist/mcpb"
PLATFORMS="${PLATFORMS:-darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64}"

command -v go >/dev/null || { echo "error: go not found in PATH" >&2; exit 1; }
command -v zip >/dev/null || { echo "error: zip not found in PATH" >&2; exit 1; }

# Maps a Go GOOS to the MCPB compatibility platform identifier.
mcpb_platform() {
    case "$1" in
        darwin) echo "darwin" ;;
        windows) echo "win32" ;;
        linux) echo "linux" ;;
        *) echo "$1" ;;
    esac
}

# write_manifest <dest> <binary_name> <platform_id>
write_manifest() {
    local dest="$1" binary_name="$2" platform_id="$3"
    cat > "$dest" <<JSON
{
  "manifest_version": "0.3",
  "name": "sofarpc-mcp",
  "display_name": "SofaRPC Direct Invoker",
  "version": "${MCPB_VERSION}",
  "description": "MCP-first SofaRPC test toolkit: direct BOLT/Hessian2 generic invocation plus config, resolve, probe, describe, and diagnostics. Pure Go, no Java runtime.",
  "author": { "name": "diandian921", "url": "https://github.com/diandian921/sofarpc-mcp" },
  "repository": { "type": "git", "url": "https://github.com/diandian921/sofarpc-mcp" },
  "homepage": "https://github.com/diandian921/sofarpc-mcp",
  "keywords": ["sofarpc", "bolt", "hessian2", "rpc", "java", "mcp"],
  "server": {
    "type": "binary",
    "entry_point": "server/${binary_name}",
    "mcp_config": {
      "command": "\${__dirname}\${/}server\${/}${binary_name}",
      "args": ["mcp"],
      "env": {}
    }
  },
  "tools": [
    { "name": "sofarpc_config_list", "description": "List configured projects and servers." },
    { "name": "sofarpc_config_save_project", "description": "Add or replace a project (workspace root + service prefixes)." },
    { "name": "sofarpc_config_save_server", "description": "Add or replace a server (address, project, protocol, timeout)." },
    { "name": "sofarpc_config_remove_project", "description": "Remove a configured project (requires confirm=true)." },
    { "name": "sofarpc_config_remove_server", "description": "Remove a configured server (requires confirm=true)." },
    { "name": "sofarpc_resolve", "description": "Resolve the project, server, and endpoint without touching the network." },
    { "name": "sofarpc_probe", "description": "Probe TCP reachability for a server or explicit address." },
    { "name": "sofarpc_describe", "description": "Search local Java source or describe methods and DTO fields for a service FQN." },
    { "name": "sofarpc_invoke_plan", "description": "Validate an invocation (endpoint, argument types) without sending a request." },
    { "name": "sofarpc_invoke", "description": "Invoke a SofaRPC method over direct BOLT/Hessian2." },
    { "name": "sofarpc_doctor", "description": "Run structured diagnostics for config, source schema, and invoke prerequisites." }
  ],
  "compatibility": {
    "platforms": ["${platform_id}"]
  }
}
JSON
}

# write_readme <dest>
write_readme() {
    cat > "$1" <<'MD'
# SofaRPC Direct Invoker (MCPB bundle)

MCP-first SofaRPC test toolkit. One self-contained Go binary speaks the MCP
stdio protocol (official `modelcontextprotocol/go-sdk`) and invokes SofaRPC
services directly over BOLT/Hessian2 — no Java process or sidecar.

After installing, add a server with the `sofarpc_config_save_server` tool, then
use `sofarpc_resolve` / `sofarpc_probe` / `sofarpc_describe` / `sofarpc_invoke`.
Config and cache live under `~/.sofarpc/` (override with the `SOFARPC_HOME` env).

This is a local developer tool: it dials addresses you configure. See
https://github.com/diandian921/sofarpc-mcp for full docs.
MD
}

# pack_bundle <dir> <out.mcpb>
pack_bundle() {
    local dir="$1" out="$2"
    rm -f "$out"
    if command -v mcpb >/dev/null 2>&1; then
        mcpb pack "$dir" "$out" >/dev/null
    else
        (cd "$dir" && zip -qr "$out" .)
    fi
}

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"
ARTIFACTS=()

for platform in $PLATFORMS; do
    goos="${platform%/*}"
    goarch="${platform#*/}"
    ext=""
    [ "$goos" = "windows" ] && ext=".exe"
    binary_name="sofarpc${ext}"

    work_dir="$DIST_DIR/work-$goos-$goarch"
    rm -rf "$work_dir"
    mkdir -p "$work_dir/server"

    echo "[build] $goos/$goarch"
    (cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
        go build -ldflags "-X main.BuildVersion=$VERSION" -o "$work_dir/server/$binary_name" ./cmd/sofarpc)

    write_manifest "$work_dir/manifest.json" "$binary_name" "$(mcpb_platform "$goos")"
    write_readme "$work_dir/README.md"

    artifact="$DIST_DIR/sofarpc-mcp-$MCPB_VERSION-$goos-$goarch.mcpb"
    pack_bundle "$work_dir" "$artifact"
    rm -rf "$work_dir"
    ARTIFACTS+=("$artifact")
    echo "[pack]  $artifact"
done

echo "[sums]  $DIST_DIR/SHA256SUMS"
(
    cd "$DIST_DIR"
    if command -v shasum >/dev/null 2>&1; then
        shasum -a 256 *.mcpb > SHA256SUMS
    else
        sha256sum *.mcpb > SHA256SUMS
    fi
)

echo "Done. ${#ARTIFACTS[@]} bundle(s) in $DIST_DIR"
