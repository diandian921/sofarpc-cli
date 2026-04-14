#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
INSTALL_DIR="$HOME/.sofarpc"
BIN_DST="$INSTALL_DIR/sofarpc"
JAR_DST="$INSTALL_DIR/sofarpcd.jar"

usage() {
    cat <<EOF
Usage: install.sh [--uninstall]

  (default)     Build and install sofarpc (Go binary) + sofarpcd.jar to $INSTALL_DIR
  --uninstall   Stop daemon if running, then remove $INSTALL_DIR/sofarpc{,d.jar}
EOF
}

uninstall() {
    if [ -x "$BIN_DST" ]; then
        "$BIN_DST" daemon stop >/dev/null 2>&1 || true
    fi
    rm -f "$BIN_DST" "$JAR_DST"
    echo "Uninstalled. Runtime state at $INSTALL_DIR/daemon/ and $INSTALL_DIR/servers.json are kept."
}

case "${1:-}" in
    -h|--help) usage; exit 0 ;;
    --uninstall) uninstall; exit 0 ;;
    "") ;;
    *) usage; exit 2 ;;
esac

command -v go >/dev/null || { echo "error: go not found in PATH" >&2; exit 1; }
command -v mvn >/dev/null || { echo "error: mvn not found in PATH" >&2; exit 1; }
command -v java >/dev/null || { echo "error: java not found in PATH" >&2; exit 1; }

echo "[1/4] Building daemon jar..."
mvn -f "$REPO_ROOT/daemon/pom.xml" -q -DskipTests package

JAR_SRC="$REPO_ROOT/daemon/target/sofarpcd.jar"
[ -f "$JAR_SRC" ] || { echo "error: $JAR_SRC not produced" >&2; exit 1; }

echo "[2/4] Building Go client..."
(cd "$REPO_ROOT/cli" && go build -ldflags "-X main.BuildVersion=$(git -C "$REPO_ROOT" describe --tags --always 2>/dev/null || echo dev)" -o sofarpc ./cmd/sofarpc)
BIN_SRC="$REPO_ROOT/cli/sofarpc"

echo "[3/4] Handling existing daemon..."
if [ -x "$BIN_DST" ] && "$BIN_DST" daemon status 2>/dev/null | grep -q '"running":true'; then
    cat >&2 <<EOF
⚠  Existing sofarpcd detected. It will be stopped so the new jar takes effect.
   If a batch/invoke is running in another terminal, it will be interrupted.
   Press Ctrl-C within 3s to skip the stop.
EOF
    sleep 3
    "$BIN_DST" daemon stop >/dev/null 2>&1 || true
fi

echo "[4/4] Installing to $INSTALL_DIR ..."
mkdir -p "$INSTALL_DIR"
install -m 0755 "$BIN_SRC" "$BIN_DST"
install -m 0644 "$JAR_SRC" "$JAR_DST"

echo "✅ Installed:"
echo "   $BIN_DST"
echo "   $JAR_DST"
echo ""
if ! echo ":$PATH:" | grep -q ":$INSTALL_DIR:"; then
    echo "Add this to your shell rc (~/.zshrc or ~/.bashrc):"
    echo ""
    echo "   export PATH=\"\$HOME/.sofarpc:\$PATH\""
    echo ""
fi
echo "Verify: sofarpc version"
