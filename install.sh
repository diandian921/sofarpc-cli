#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
JAR_SRC="$SCRIPT_DIR/target/sofarpc.jar"
INSTALL_DIR="$HOME/.sofarpc"

if [ ! -f "${JAR_SRC}" ]; then
    echo "❌ 未找到 ${JAR_SRC}，请先执行: mvn clean package -DskipTests" >&2
    exit 1
fi

mkdir -p "${INSTALL_DIR}"
cp "${JAR_SRC}" "${INSTALL_DIR}/sofarpc.jar"

cat > "$INSTALL_DIR/sofarpc" << 'EOF'
#!/bin/bash
exec java -jar "$HOME/.sofarpc/sofarpc.jar" "$@"
EOF
chmod +x "$INSTALL_DIR/sofarpc"

echo "✅ 安装完成，请将以下内容加入你的 ~/.zshrc 或 ~/.bashrc："
echo ""
echo '  export PATH="$HOME/.sofarpc:$PATH"'
echo ""
echo "执行后重新打开终端，即可使用 sofarpc 命令。"
