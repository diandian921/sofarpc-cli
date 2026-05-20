#!/usr/bin/env bash
# Cross-compile the release matrix and emit OS-appropriate archives plus a
# single SHA256SUMS. tar.gz for macOS/Linux (preserves the executable bit);
# zip for Windows (native). Each archive carries both binaries, README.md, and
# the thin bootstrap scripts.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# Module is at repo root: release tags are vX.Y.Z.
VERSION="${VERSION:-$(git -C "$REPO_ROOT" describe --tags --match 'v*' --always 2>/dev/null || echo dev)}"
DIST_DIR="$REPO_ROOT/dist"

# Default matrix; override with PLATFORMS="os/arch os/arch ...".
PLATFORMS="${PLATFORMS:-darwin/arm64 darwin/amd64 linux/amd64 windows/amd64}"

command -v go >/dev/null || { echo "error: go not found in PATH" >&2; exit 1; }
command -v tar >/dev/null || { echo "error: tar not found in PATH" >&2; exit 1; }
case " $PLATFORMS " in
    *" windows/"*) command -v zip >/dev/null || { echo "error: zip not found in PATH" >&2; exit 1; } ;;
esac

mkdir -p "$DIST_DIR"
ARCHIVES=()

for platform in $PLATFORMS; do
    GOOS_VALUE="${platform%/*}"
    GOARCH_VALUE="${platform#*/}"
    EXT=""
    [ "$GOOS_VALUE" = "windows" ] && EXT=".exe"

    WORK_DIR="$DIST_DIR/sofarpc-$VERSION-$GOOS_VALUE-$GOARCH_VALUE"
    rm -rf "$WORK_DIR"
    mkdir -p "$WORK_DIR"

    echo "[build] $GOOS_VALUE/$GOARCH_VALUE"
    (cd "$REPO_ROOT" && GOOS="$GOOS_VALUE" GOARCH="$GOARCH_VALUE" \
        go build -ldflags "-X main.BuildVersion=$VERSION" -o "$WORK_DIR/sofarpc$EXT" ./cmd/sofarpc)
    (cd "$REPO_ROOT" && GOOS="$GOOS_VALUE" GOARCH="$GOARCH_VALUE" \
        go build -ldflags "-X main.BuildVersion=$VERSION" -o "$WORK_DIR/sofarpc-mcp$EXT" ./cmd/sofarpc-mcp)

    cp "$REPO_ROOT/README.md" "$WORK_DIR/README.md"
    cp "$REPO_ROOT/scripts/install.sh" "$WORK_DIR/install.sh"
    cp "$REPO_ROOT/scripts/install.ps1" "$WORK_DIR/install.ps1"

    base="sofarpc-$VERSION-$GOOS_VALUE-$GOARCH_VALUE"
    if [ "$GOOS_VALUE" = "windows" ]; then
        archive="$DIST_DIR/$base.zip"
        rm -f "$archive"
        (cd "$WORK_DIR" && zip -qr "$archive" .)
    else
        archive="$DIST_DIR/$base.tar.gz"
        rm -f "$archive"
        tar -czf "$archive" -C "$DIST_DIR" "$base"
    fi
    ARCHIVES+=("$archive")
    echo "[pack]  $archive"
done

echo "[sums]  $DIST_DIR/SHA256SUMS"
(
    cd "$DIST_DIR"
    : > SHA256SUMS
    for a in "${ARCHIVES[@]}"; do
        name="$(basename "$a")"
        if command -v sha256sum >/dev/null; then
            sha256sum "$name" >> SHA256SUMS
        else
            shasum -a 256 "$name" >> SHA256SUMS
        fi
    done
)

printf '%s\n' "${ARCHIVES[@]}"
echo "$DIST_DIR/SHA256SUMS"
