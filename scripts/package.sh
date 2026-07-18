#!/usr/bin/env bash
# Cross-compile the release matrix and emit OS-appropriate archives plus a
# single SHA256SUMS. tar.gz for macOS/Linux (preserves the executable bit);
# zip for Windows (native). Each archive carries the single sofarpc binary,
# README.md, and its README hero SVG — the user just runs
# `./sofarpc install <host>` directly. The network bootstrap
# (scripts/install.{sh,ps1}) is served from raw.githubusercontent.com and not
# shipped inside archives.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# Module is at repo root: release tags are vX.Y.Z.
VERSION="${VERSION:-$(git -C "$REPO_ROOT" describe --tags --match 'v*' --always 2>/dev/null || echo dev)}"
DIST_DIR="${DIST_DIR:-$REPO_ROOT/dist}"

# Default matrix; override with PLATFORMS="os/arch os/arch ...".
PLATFORMS="${PLATFORMS:-darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64}"

command -v go >/dev/null || { echo "error: go not found in PATH" >&2; exit 1; }
command -v tar >/dev/null || { echo "error: tar not found in PATH" >&2; exit 1; }
case " $PLATFORMS " in
    *" windows/"*) command -v zip >/dev/null || { echo "error: zip not found in PATH" >&2; exit 1; } ;;
esac

create_tar_archive() {
    local archive="$1" base="$2" tar_version
    tar_version="$(tar --version 2>&1 || true)"
    if [[ "$tar_version" == *bsdtar* || "$tar_version" == *libarchive* ]]; then
        COPYFILE_DISABLE=1 tar --uid 0 --gid 0 --uname root --gname root \
            -czf "$archive" -C "$DIST_DIR" "$base"
    else
        COPYFILE_DISABLE=1 tar --owner=0 --group=0 --numeric-owner --sort=name \
            -czf "$archive" -C "$DIST_DIR" "$base"
    fi
}

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
    # CGO_ENABLED=0 for statically linked release binaries (consistent with
    # build-mcpb.sh); a glibc-linked build would not run on musl/Nix hosts.
    (cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS="$GOOS_VALUE" GOARCH="$GOARCH_VALUE" \
        go build -ldflags "-X main.BuildVersion=$VERSION" -o "$WORK_DIR/sofarpc$EXT" ./cmd/sofarpc)

    cp "$REPO_ROOT/README.md" "$WORK_DIR/README.md"
    mkdir -p "$WORK_DIR/docs"
    cp "$REPO_ROOT/docs/readme-hero.svg" "$WORK_DIR/docs/readme-hero.svg"

    base="sofarpc-$VERSION-$GOOS_VALUE-$GOARCH_VALUE"
    if [ "$GOOS_VALUE" = "windows" ]; then
        archive="$DIST_DIR/$base.zip"
        rm -f "$archive"
        (cd "$DIST_DIR" && zip -qr "$archive" "$base")
    else
        archive="$DIST_DIR/$base.tar.gz"
        rm -f "$archive"
        create_tar_archive "$archive" "$base"
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
