#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
INSTALL_ROOT="${SOFARPC_HOME:-$HOME/.sofarpc}"
BIN_DIR="$INSTALL_ROOT/bin"
LIB_DIR="$INSTALL_ROOT/lib"
STATE_DIR="$INSTALL_ROOT/state"
LOG_DIR="$INSTALL_ROOT/logs"
CACHE_DIR="$INSTALL_ROOT/cache"
CONFIG_FILE="$INSTALL_ROOT/config.json"

usage() {
    cat <<EOF
Usage: install.sh [--uninstall]

  (default)     Build and install sofarpc-mcp, sofarpc-cli, and sofarpc-engine.jar
  --uninstall   Stop Engine if possible, then remove installed binaries and jar
EOF
}

check_java() {
    command -v java >/dev/null || { echo "error: java not found in PATH; JDK/JRE 8+ is required" >&2; exit 1; }
    local version
    version="$(java -version 2>&1 | awk -F '"' '/version/ {print $2; exit}')"
    local major="${version%%.*}"
    if [ "$major" = "1" ]; then
        major="$(echo "$version" | cut -d. -f2)"
    fi
    if [ -n "$major" ] && [ "$major" -lt 8 ] 2>/dev/null; then
        echo "error: Java 8+ is required, found: $version" >&2
        exit 1
    fi
}

uninstall() {
    if [ -x "$BIN_DIR/sofarpc-cli" ]; then
        "$BIN_DIR/sofarpc-cli" daemon stop >/dev/null 2>&1 || true
    fi
    rm -f "$BIN_DIR/sofarpc-cli" "$BIN_DIR/sofarpc-mcp" "$LIB_DIR/sofarpc-engine.jar"
    echo "Uninstalled binaries and jar. Kept config, state, logs, token, and cache under $INSTALL_ROOT."
}

case "${1:-}" in
    -h|--help) usage; exit 0 ;;
    --uninstall) uninstall; exit 0 ;;
    "") ;;
    *) usage; exit 2 ;;
esac

check_java

VERSION="$(git -C "$REPO_ROOT" describe --tags --always 2>/dev/null || echo dev)"
PACKAGED=false
if [ -f "$SCRIPT_DIR/sofarpc-engine.jar" ] && [ -x "$SCRIPT_DIR/sofarpc-cli" ] && [ -x "$SCRIPT_DIR/sofarpc-mcp" ]; then
    PACKAGED=true
fi

if [ "$PACKAGED" = true ]; then
    echo "[1/5] Using packaged artifacts..."
    JAR_SRC="$SCRIPT_DIR/sofarpc-engine.jar"
    CLI_SRC="$SCRIPT_DIR/sofarpc-cli"
    MCP_SRC="$SCRIPT_DIR/sofarpc-mcp"
else
    command -v go >/dev/null || { echo "error: go not found in PATH" >&2; exit 1; }
    command -v mvn >/dev/null || { echo "error: mvn not found in PATH" >&2; exit 1; }

    echo "[1/5] Building Java Engine..."
    mvn -f "$REPO_ROOT/daemon/pom.xml" -q -DskipTests package
    JAR_SRC="$REPO_ROOT/daemon/target/sofarpc-engine.jar"
    [ -f "$JAR_SRC" ] || { echo "error: $JAR_SRC not produced" >&2; exit 1; }

    echo "[2/5] Building Go binaries..."
    (cd "$REPO_ROOT/cli" && go build -ldflags "-X main.BuildVersion=$VERSION" -o sofarpc-cli ./cmd/sofarpc)
    (cd "$REPO_ROOT/cli" && go build -ldflags "-X main.BuildVersion=$VERSION" -o sofarpc-mcp ./cmd/sofarpc-mcp)
    CLI_SRC="$REPO_ROOT/cli/sofarpc-cli"
    MCP_SRC="$REPO_ROOT/cli/sofarpc-mcp"
fi

echo "[3/5] Preparing install layout..."
mkdir -p "$BIN_DIR" "$LIB_DIR" "$STATE_DIR" "$LOG_DIR" "$CACHE_DIR/schema"
if [ ! -f "$CONFIG_FILE" ]; then
    cat > "$CONFIG_FILE" <<'JSON'
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
JSON
fi

echo "[4/5] Stopping old Engine if needed..."
if [ -x "$BIN_DIR/sofarpc-cli" ]; then
    "$BIN_DIR/sofarpc-cli" daemon stop >/dev/null 2>&1 || true
fi

echo "[5/5] Installing to $INSTALL_ROOT..."
install -m 0755 "$CLI_SRC" "$BIN_DIR/sofarpc-cli"
install -m 0755 "$MCP_SRC" "$BIN_DIR/sofarpc-mcp"
install -m 0644 "$JAR_SRC" "$LIB_DIR/sofarpc-engine.jar"

echo "Installed:"
echo "  $BIN_DIR/sofarpc-cli"
echo "  $BIN_DIR/sofarpc-mcp"
echo "  $LIB_DIR/sofarpc-engine.jar"
echo ""
if ! echo ":$PATH:" | grep -q ":$BIN_DIR:"; then
    echo "Add this to your shell rc (~/.zshrc or ~/.bashrc):"
    echo "  export PATH=\"\$HOME/.sofarpc/bin:\$PATH\""
fi
echo "Verify: sofarpc-cli version"
