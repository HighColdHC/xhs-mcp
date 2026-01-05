#!/bin/bash

# macOS 跨平台构建脚本
# 注意：这个脚本需要在 macOS 上运行，或者使用 osxcross

set -e

echo "=========================================="
echo "  XHS MCP Desktop - macOS 构建脚本"
echo "=========================================="
echo ""

# 检查当前系统
if [[ "$OSTYPE" != "darwin"* ]]; then
    echo "❌ 错误：此脚本必须在 macOS 上运行"
    echo ""
    echo "替代方案："
    echo "1. 使用 GitHub Actions 自动构建"
    echo "2. 使用 MacStadium 的 macOS CI"
    echo "3. 借用 Mac 电脑"
    exit 1
fi

# 检查 Go 和 Node.js
if ! command -v go &> /dev/null; then
    echo "❌ 错误：未找到 Go"
    exit 1
fi

if ! command -v npm &> /dev/null; then
    echo "❌ 错误：未找到 npm"
    exit 1
fi

echo "✅ 环境检查通过"
echo "   Go: $(go version)"
echo "   Node: $(node --version)"
echo "   npm: $(npm --version)"
echo ""

# 进入后端目录
cd "$(dirname "$0")/backend"

# 编译 macOS 后端 (支持 Intel 和 Apple Silicon)
echo "📦 编译 macOS 后端..."

# Intel 版本
echo "  - 编译 x86_64 (Intel)..."
GOOS=darwin GOARCH=amd64 go build -o xhs-mcp-amd64 .

# Apple Silicon 版本
echo "  - 编译 arm64 (Apple Silicon)..."
GOOS=darwin GOARCH=arm64 go build -o xhs-mcp-arm64 .

# 通用二进制文件
echo "  - 合并通用二进制文件..."
lipo -create -output xhs-mcp-mac xhs-mcp-amd64 xhs-mcp-arm64
rm xhs-mcp-amd64 xhs-mcp-arm64

echo "✅ 后端编译完成"
echo ""

# 进入桌面目录
cd "../desktop"

# 安装依赖（如果需要）
if [ ! -d "node_modules" ]; then
    echo "📦 安装 npm 依赖..."
    npm install
fi

# 打包 macOS 应用
echo "📦 打包 macOS 应用..."
npm run build:mac

echo ""
echo "=========================================="
echo "✅ macOS 构建完成！"
echo ""
echo "输出文件位置:"
ls -lh dist/*.dmg 2>/dev/null || echo "  (未找到 .dmg 文件)"
echo "=========================================="
