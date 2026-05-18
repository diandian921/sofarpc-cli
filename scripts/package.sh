#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VERSION="${VERSION:-$(git -C "$REPO_ROOT" describe --tags --always 2>/dev/null || echo dev)}"
GOOS_VALUE="${GOOS:-$(go env GOOS)}"
GOARCH_VALUE="${GOARCH:-$(go env GOARCH)}"
DIST_DIR="$REPO_ROOT/dist"
WORK_DIR="$DIST_DIR/sofarpc-$VERSION-$GOOS_VALUE-$GOARCH_VALUE"
EXT=""
if [ "$GOOS_VALUE" = "windows" ]; then
    EXT=".exe"
fi

command -v go >/dev/null || { echo "error: go not found in PATH" >&2; exit 1; }
command -v zip >/dev/null || { echo "error: zip not found in PATH" >&2; exit 1; }

rm -rf "$WORK_DIR"
mkdir -p "$WORK_DIR" "$DIST_DIR"

echo "[1/3] Building Go binaries for $GOOS_VALUE/$GOARCH_VALUE..."
(cd "$REPO_ROOT/cli" && GOOS="$GOOS_VALUE" GOARCH="$GOARCH_VALUE" go build -ldflags "-X main.BuildVersion=$VERSION" -o "$WORK_DIR/sofarpc-cli$EXT" ./cmd/sofarpc)
(cd "$REPO_ROOT/cli" && GOOS="$GOOS_VALUE" GOARCH="$GOARCH_VALUE" go build -ldflags "-X main.BuildVersion=$VERSION" -o "$WORK_DIR/sofarpc-mcp$EXT" ./cmd/sofarpc-mcp)

echo "[2/3] Adding install scripts..."
cp "$REPO_ROOT/scripts/install.sh" "$WORK_DIR/install.sh"
cp "$REPO_ROOT/scripts/install.ps1" "$WORK_DIR/install.ps1"

echo "[3/3] Creating package..."
PACKAGE="$DIST_DIR/sofarpc-$VERSION-$GOOS_VALUE-$GOARCH_VALUE.zip"
rm -f "$PACKAGE"
(cd "$WORK_DIR" && zip -qr "$PACKAGE" .)

echo "$PACKAGE"
