#!/bin/bash
INSTALL_DIR="$HOME/.sofarpc"
mkdir -p "$INSTALL_DIR"

# Copy jar
cp target/sofarpc.jar "$INSTALL_DIR/sofarpc.jar"

# Create shell wrapper
cat > "$INSTALL_DIR/sofarpc" << 'EOF'
#!/bin/bash
exec java -jar "$HOME/.sofarpc/sofarpc.jar" "$@"
EOF
chmod +x "$INSTALL_DIR/sofarpc"

# Copy SKILL.md
cp SKILL.md "$INSTALL_DIR/SKILL.md"

echo "✅ 安装完成，请将以下内容加入你的 ~/.zshrc 或 ~/.bashrc："
echo ""
echo '  export PATH="$HOME/.sofarpc:$PATH"'
echo ""
echo "执行后重新打开终端，即可使用 sofarpc 命令。"
