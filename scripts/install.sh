#!/usr/bin/env bash
# Network bootstrap. Designed to be served via raw.githubusercontent.com and
# piped to bash:
#
#   curl -fsSL https://raw.githubusercontent.com/diandian921/sofarpc-mcp/main/scripts/install.sh | bash -s -- codex
#   curl -fsSL https://raw.githubusercontent.com/diandian921/sofarpc-mcp/main/scripts/install.sh | bash -s -- --version v0.1.0-beta.5 all
#
# The script ONLY acquires the release archive (detect OS/arch, download,
# verify SHA256, extract) and hands control to `./sofarpc install <host>`.
# It knows nothing about host config formats and duplicates no install logic.
#
# Falls back to a local from-repo build when invoked from a working tree that
# contains cmd/sofarpc/ (e.g. `bash scripts/install.sh codex` during dev).
set -euo pipefail

REPO="diandian921/sofarpc-mcp"

usage() {
    cat <<EOF
Usage: install.sh [--version vX.Y.Z] [claude|codex|all]

  (no host)        Install only; do not register any host.
  claude           Install and register the MCP server with Claude Code.
  codex            Install and register the MCP server with Codex.
  all              Install and register both.
  --version TAG    Pin a release tag instead of resolving the latest.
EOF
}

version=""
host=""
while [ $# -gt 0 ]; do
    case "$1" in
        -h|--help) usage; exit 0 ;;
        --version)
            shift
            [ $# -gt 0 ] || { echo "error: --version requires a tag" >&2; exit 2; }
            version="$1"
            ;;
        claude|codex|all)
            host="$1"
            ;;
        *)
            echo "error: unknown argument $1" >&2
            usage >&2
            exit 2
            ;;
    esac
    shift
done

# Dev path: if invoked from a repo checkout that has cmd/sofarpc, build
# locally and hand off. Skips download/checksum entirely.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd 2>/dev/null || pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." 2>/dev/null && pwd || true)"
if [ -n "${REPO_ROOT:-}" ] && [ -d "$REPO_ROOT/cmd/sofarpc" ] && command -v go >/dev/null; then
    BUILD_DIR="$(mktemp -d)"
    trap 'rm -rf "$BUILD_DIR"' EXIT
    BIN_VERSION="$(git -C "$REPO_ROOT" describe --tags --match 'v*' --always 2>/dev/null || echo dev)"
    (cd "$REPO_ROOT" && go build -ldflags "-X main.BuildVersion=$BIN_VERSION" -o "$BUILD_DIR/sofarpc" ./cmd/sofarpc)
    if [ -n "$host" ]; then
        exec "$BUILD_DIR/sofarpc" install "$host"
    fi
    exec "$BUILD_DIR/sofarpc" install
fi

# Network path: detect platform.
case "$(uname -s)" in
    Darwin) goos="darwin" ;;
    Linux)  goos="linux" ;;
    *)
        echo "error: unsupported OS '$(uname -s)'. On Windows use scripts/install.ps1." >&2
        exit 1
        ;;
esac
case "$(uname -m)" in
    x86_64|amd64) goarch="amd64" ;;
    arm64|aarch64) goarch="arm64" ;;
    *)
        echo "error: unsupported architecture '$(uname -m)'" >&2
        exit 1
        ;;
esac

command -v curl >/dev/null || { echo "error: curl is required" >&2; exit 1; }
command -v tar  >/dev/null || { echo "error: tar is required"  >&2; exit 1; }

# Resolve latest tag via the redirect, no API call (avoids rate limits).
if [ -z "$version" ]; then
    redirect_url="$(curl -sIL -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest")"
    version="${redirect_url##*/tag/}"
    if [ -z "$version" ] || [ "$version" = "$redirect_url" ]; then
        echo "error: could not resolve latest release tag" >&2
        exit 1
    fi
fi

archive="sofarpc-$version-$goos-$goarch.tar.gz"
base_url="https://github.com/$REPO/releases/download/$version"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $archive ..."
curl -fsSL -o "$tmp/$archive"     "$base_url/$archive"
curl -fsSL -o "$tmp/SHA256SUMS"   "$base_url/SHA256SUMS"

echo "Verifying SHA256 ..."
if command -v sha256sum >/dev/null; then
    expected="$(awk -v a="$archive" '$2 == a || $2 == "*"a {print $1}' "$tmp/SHA256SUMS")"
    actual="$(sha256sum "$tmp/$archive" | awk '{print $1}')"
else
    expected="$(awk -v a="$archive" '$2 == a || $2 == "*"a {print $1}' "$tmp/SHA256SUMS")"
    actual="$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')"
fi
if [ -z "$expected" ]; then
    echo "error: $archive missing from SHA256SUMS" >&2
    exit 1
fi
if [ "$expected" != "$actual" ]; then
    echo "error: checksum mismatch for $archive" >&2
    echo "  expected: $expected" >&2
    echo "  actual:   $actual"   >&2
    exit 1
fi

echo "Extracting ..."
tar -xzf "$tmp/$archive" -C "$tmp"
extracted_dir="$tmp/sofarpc-$version-$goos-$goarch"
sofarpc_bin="$extracted_dir/sofarpc"
[ -x "$sofarpc_bin" ] || { echo "error: archive did not contain $sofarpc_bin" >&2; exit 1; }

if [ -n "$host" ]; then
    exec "$sofarpc_bin" install "$host"
fi
exec "$sofarpc_bin" install
