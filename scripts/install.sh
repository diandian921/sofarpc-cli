#!/usr/bin/env bash
# Thin bootstrap. All install logic lives in `sofarpc self-install` (one tested
# Go path); this script only produces a sofarpc binary and hands off to it.
# It calls the freshly built/extracted binary by absolute path, never a
# PATH-resolved name, to avoid the go-install/self-install PATH trap.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ -x "$SCRIPT_DIR/sofarpc" ] && [ -x "$SCRIPT_DIR/sofarpc-mcp" ]; then
    exec "$SCRIPT_DIR/sofarpc" self-install --mcp-path "$SCRIPT_DIR/sofarpc-mcp" "$@"
fi

command -v go >/dev/null || { echo "error: go not found and no packaged binaries present" >&2; exit 1; }
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# Subdirectory module: match cli/vX.Y.Z tags (not a stray root tag), strip
# the prefix for the stamped name.
VERSION="$(git -C "$REPO_ROOT" describe --tags --match 'cli/v*' --always 2>/dev/null || echo dev)"
VERSION="${VERSION#cli/}"
BUILD_DIR="$(mktemp -d)"
trap 'rm -rf "$BUILD_DIR"' EXIT
(cd "$REPO_ROOT/cli" && go build -ldflags "-X main.BuildVersion=$VERSION" -o "$BUILD_DIR/sofarpc" ./cmd/sofarpc)
(cd "$REPO_ROOT/cli" && go build -ldflags "-X main.BuildVersion=$VERSION" -o "$BUILD_DIR/sofarpc-mcp" ./cmd/sofarpc-mcp)
exec "$BUILD_DIR/sofarpc" self-install --mcp-path "$BUILD_DIR/sofarpc-mcp" "$@"
