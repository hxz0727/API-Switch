#!/usr/bin/env bash
set -euo pipefail

# ──────────────────────────────────────────────────
# API-Switch 快速安装脚本
# 支持 Linux / macOS
# ──────────────────────────────────────────────────

BOLD='\033[1m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; }
step()  { echo -e "\n${BOLD}═══ $* ═══${NC}"; }

REPO="https://github.com/hxz0727/API-Switch.git"
INSTALL_DIR="${INSTALL_DIR:-$HOME/api-switch}"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"

# ── 0. 设置 Go 代理（国内用户使用 goproxy.cn） ─────
if [ -z "${GOPROXY:-}" ]; then
    if curl -sI --max-time 5 "https://proxy.golang.org" >/dev/null 2>&1; then
        :  # 默认 proxy 可达，使用默认值
    else
        info "Go 官方代理不可达，使用 goproxy.cn（中国镜像）"
        export GOPROXY="https://goproxy.cn,direct"
    fi
fi

# 禁止 Go 自动下载 toolchain（避免 toolchain 下载失败）
export GOTOOLCHAIN=local

# ── 1. 检查 Go ────────────────────────────────────
step "1/4  检查 Go 环境"

if command -v go &>/dev/null; then
    go_version=$(go version | grep -oP 'go\K[0-9]+\.[0-9]+')
    info "Go 版本: $go_version"
    # 需要 Go 1.22+
    major=$(echo "$go_version" | cut -d. -f1)
    minor=$(echo "$go_version" | cut -d. -f2)
    if [ "$major" -lt 1 ] || { [ "$major" -eq 1 ] && [ "$minor" -lt 22 ]; }; then
        error "Go 版本过低，需要 >= 1.22，当前: $go_version"
        exit 1
    fi
else
    warn "未检测到 Go，尝试自动安装..."
    if command -v brew &>/dev/null; then
        brew install go
    elif command -v apt-get &>/dev/null; then
        apt-get update -qq && apt-get install -y -qq golang-go 2>/dev/null || {
            warn "apt-get golang-go 可能版本过旧，尝试从官网下载..."
            install_go_from_source
        }
    elif command -v yum &>/dev/null; then
        yum install -y golang
    else
        install_go_from_source
    fi
    go version
fi

install_go_from_source() {
    local ver="1.23.0"
    local arch="amd64"
    [ "$(uname -m)" = "aarch64" ] && arch="arm64"
    local os="linux"
    [ "$(uname)" = "Darwin" ] && os="darwin"
    local pkg="go${ver}.${os}-${arch}.tar.gz"
    info "下载 Go $ver ($os/$arch)..."
    curl -sL "https://go.dev/dl/${pkg}" -o "/tmp/${pkg}"
    rm -rf /usr/local/go
    tar -C /usr/local -xzf "/tmp/${pkg}"
    export PATH="/usr/local/go/bin:$PATH"
    info "Go 安装完成"
}

# ── 2. 下载项目 ────────────────────────────────────
step "2/4  下载 API-Switch"

if [ -d "$INSTALL_DIR" ]; then
    info "目录已存在: $INSTALL_DIR，尝试更新..."
    cd "$INSTALL_DIR"
    git pull --ff-only 2>/dev/null || {
        warn "更新失败，请手动处理：cd $INSTALL_DIR && git pull"
    }
else
    info "克隆仓库到 $INSTALL_DIR ..."
    git clone --depth=1 "$REPO" "$INSTALL_DIR"
    cd "$INSTALL_DIR"
fi

# ── 3. 编译 ────────────────────────────────────────
step "3/4  编译二进制"

info "下载依赖..."
cd "$INSTALL_DIR"
go mod tidy

info "编译 api-switch ..."
go build -ldflags="-s -w" -o "$INSTALL_DIR/api-switch" ./cmd/api-switch/

info "二进制大小: $(du -h "$INSTALL_DIR/api-switch" | cut -f1)"

# 安装到 PATH
mkdir -p "$BIN_DIR"
ln -sf "$INSTALL_DIR/api-switch" "$BIN_DIR/api-switch"

# 添加到 PATH（如果不在）
if [[ ":$PATH:" != *":$BIN_DIR:"* ]]; then
    shell_rc="$HOME/.bashrc"
    [ -n "$ZSH_VERSION" ] && shell_rc="$HOME/.zshrc"
    [ -f "$HOME/.zshrc" ] && shell_rc="$HOME/.zshrc"
    echo "export PATH=\"\$PATH:$BIN_DIR\"" >> "$shell_rc"
    info "已将 $BIN_DIR 添加到 PATH，请执行: source $shell_rc"
fi

export PATH="$PATH:$BIN_DIR"
info "安装路径: $(which api-switch)"

# ── 4. 验证 ────────────────────────────────────────
step "4/4  验证安装"

api-switch --help >/dev/null 2>&1 && info "安装成功! 🎉" || error "安装失败"

echo ""
echo -e "${BOLD}快速开始${NC}"
echo ""
echo "  # 1. 添加 provider（支持预设模板）"
echo "  api-switch provider add deepseek --key sk-xxx"
echo ""
echo "  # 2. 添加模型"
echo "  api-switch model import deepseek"
echo ""
echo "  # 3. 切换模型并启动"
echo "  api-switch use deepseek-chat"
echo "  api-switch serve"
echo ""
echo "  # 更多帮助"
echo "  api-switch --help"
echo "  api-switch doctor    # 一键诊断"
echo ""

# 检测 Claude Code
if command -v claude &>/dev/null; then
    info "检测到 Claude Code，配置代理..."
    echo ""
    echo "  在 Claude Code 中执行:"
    echo "  /settings  → 设置 ANTHROPIC_BASE_URL = http://localhost:8080"
    echo "  或运行:"
    echo "  api-switch generate-claude-config"
    echo "  api-switch use deepseek-chat"
fi
