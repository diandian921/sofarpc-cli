#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
INSTALL_ROOT="${SOFARPC_HOME:-$HOME/.sofarpc}"
BIN_DIR="$INSTALL_ROOT/bin"
CACHE_DIR="$INSTALL_ROOT/cache"
CONFIG_FILE="$INSTALL_ROOT/config.json"

usage() {
    cat <<EOF
Usage: install.sh [--uninstall]

  (default)     Build and install sofarpc-mcp and sofarpc-cli
  --uninstall   Remove installed binaries
EOF
}

uninstall() {
    rm -f "$BIN_DIR/sofarpc-cli" "$BIN_DIR/sofarpc-mcp"
    echo "Uninstalled binaries. Kept config and cache under $INSTALL_ROOT."
}

case "${1:-}" in
    -h|--help) usage; exit 0 ;;
    --uninstall) uninstall; exit 0 ;;
    "") ;;
    *) usage; exit 2 ;;
esac

VERSION="$(git -C "$REPO_ROOT" describe --tags --always 2>/dev/null || echo dev)"
PACKAGED=false
if [ -x "$SCRIPT_DIR/sofarpc-cli" ] && [ -x "$SCRIPT_DIR/sofarpc-mcp" ]; then
    PACKAGED=true
fi

if [ "$PACKAGED" = true ]; then
    echo "[1/4] Using packaged artifacts..."
    CLI_SRC="$SCRIPT_DIR/sofarpc-cli"
    MCP_SRC="$SCRIPT_DIR/sofarpc-mcp"
else
    command -v go >/dev/null || { echo "error: go not found in PATH" >&2; exit 1; }

    echo "[1/4] Building Go binaries..."
    (cd "$REPO_ROOT/cli" && go build -ldflags "-X main.BuildVersion=$VERSION" -o sofarpc-cli ./cmd/sofarpc)
    (cd "$REPO_ROOT/cli" && go build -ldflags "-X main.BuildVersion=$VERSION" -o sofarpc-mcp ./cmd/sofarpc-mcp)
    CLI_SRC="$REPO_ROOT/cli/sofarpc-cli"
    MCP_SRC="$REPO_ROOT/cli/sofarpc-mcp"
fi

echo "[2/4] Preparing install layout..."
mkdir -p "$BIN_DIR" "$CACHE_DIR/schema"
if [ ! -f "$CONFIG_FILE" ]; then
    cat > "$CONFIG_FILE" <<'JSON'
{
  "projects": {},
  "servers": {}
}
JSON
fi

echo "[3/4] Installing to $INSTALL_ROOT..."
install -m 0755 "$CLI_SRC" "$BIN_DIR/sofarpc-cli"
install -m 0755 "$MCP_SRC" "$BIN_DIR/sofarpc-mcp"

echo "[4/4] Done."
echo "Installed:"
echo "  $BIN_DIR/sofarpc-cli"
echo "  $BIN_DIR/sofarpc-mcp"
echo ""
if ! echo ":$PATH:" | grep -q ":$BIN_DIR:"; then
    echo "Add this to your shell rc (~/.zshrc or ~/.bashrc):"
    echo "  export PATH=\"\$HOME/.sofarpc/bin:\$PATH\""
fi
echo "Verify: sofarpc-cli version"
