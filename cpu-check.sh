#!/bin/bash
# cpu-check.sh - 查看 CPU 信息的 Shell 脚本
# 用法: ./cpu-check.sh [选项]
#   -u, --usage     查看 CPU 实时使用率
#   -i, --info      查看 CPU 详细信息
#   -l, --load      查看系统负载
#   -t, --top       查看 CPU 占用最高的进程 (Top 10)
#   -a, --all       查看以上所有信息
#   -h, --help      显示帮助信息

# ── 颜色定义 ──
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# ── 分隔线 ──
print_header() {
    echo ""
    echo -e "${BOLD}══════════════════════════════════════════${NC}"
    echo -e "${BOLD}  $1${NC}"
    echo -e "${BOLD}══════════════════════════════════════════${NC}"
    echo ""
}

# ── CPU 使用率 ──
show_usage() {
    print_header "📊 CPU 实时使用率"

    # 使用 top 获取一次快照
    local cpu_line
    cpu_line=$(top -bn1 | grep "Cpu(s)" | sed "s/.*, \([0-9.]*\)%\s*id.*/\1/")

    if [[ -z "$cpu_line" ]]; then
        # 兼容不同版本的 top
        cpu_line=$(top -bn1 | grep "%Cpu" | head -1 | awk '{for(i=1;i<=NF;i++) if($i=="id") print $(i-1)}' | tr -d ',')
    fi

    if [[ -n "$cpu_line" ]]; then
        local usage
        usage=$(echo "$cpu_line" | awk '{printf "%.1f", 100 - $1}')
        echo -e "  CPU 空闲: ${CYAN}${cpu_line}%${NC}"
        echo -e "  CPU 使用: ${YELLOW}${usage}%${NC}"

        # 根据使用率给出提示
        local int_usage=${usage%.*}
        if (( int_usage > 90 )); then
            echo -e "  ⚠️  状态: ${RED}CPU 使用率过高！${NC}"
        elif (( int_usage > 70 )); then
            echo -e "  ⚠️  状态: ${YELLOW}CPU 使用率偏高${NC}"
        else
            echo -e "  ✅ 状态: ${GREEN}CPU 使用率正常${NC}"
        fi
    else
        echo -e "  ${RED}无法获取 CPU 使用率信息${NC}"
    fi

    # 显示各核心使用率
    echo ""
    echo -e "  ${BOLD}各核心使用率:${NC}"
    if command -v mpstat &>/dev/null; then
        mpstat 1 1 | awk '/^[[:space:]]*[0-9]/ || /^Average/ {printf "    Core %-4s: %s%%\n", $1, $NF}'
    elif [[ -f /proc/stat ]]; then
        local core_count
        core_count=$(nproc)
        for (( i=0; i<core_count; i++ )); do
            if [[ -f /sys/devices/system/cpu/cpu${i}/cpuinfo.online ]] && [[ $(cat /sys/devices/system/cpu/cpu${i}/cpuinfo.online) == "1" ]]; then
                echo -e "    Core ${i}: ${CYAN}在线${NC}"
            fi
        done
    else
        echo -e "    ${YELLOW}需要安装 sysstat 包 (mpstat) 以显示核心详情${NC}"
    fi
}

# ── CPU 详细信息 ──
show_info() {
    print_header "🖥️  CPU 详细信息"

    if [[ -f /proc/cpuinfo ]]; then
        echo -e "  ${BOLD}处理器名称:${NC} $(grep -m1 'model name' /proc/cpuinfo | cut -d: -f2 | xargs)"
        echo -e "  ${BOLD}物理核心数:${NC} $(grep -c '^processor' /proc/cpuinfo)"
        echo -e "  ${BOLD}逻辑核心数:${NC} $(nproc)"
        echo -e "  ${BOLD}CPU 频率:${NC} $(grep -m1 'cpu MHz' /proc/cpuinfo | cut -d: -f2 | xargs) MHz"
        echo -e "  ${BOLD}架构:${NC} $(uname -m)"
        echo -e "  ${BOLD}CPU 型号:${NC} $(lscpu 2>/dev/null | grep 'Model name' | cut -d: -f2 | xargs || echo 'N/A')"
        echo -e "  ${BOLD}缓存:${NC} $(grep -m1 'cache size' /proc/cpuinfo | cut -d: -f2 | xargs || echo 'N/A')"

        # 显示 CPU 标志
        echo -e ""
        echo -e "  ${BOLD}CPU 特性标志:${NC}"
        local flags
        flags=$(grep -m1 'flags' /proc/cpuinfo | cut -d: -f2 | xargs)
        if [[ -n "$flags" ]]; then
            echo "$flags" | tr ' ' '\n' | head -10 | while read -r flag; do
                echo -e "    • ${flag}"
            done
            local total_flags
            total_flags=$(echo "$flags" | wc -w)
            echo -e "    ... 共 ${total_flags} 个标志"
        fi
    else
        echo -e "  ${RED}/proc/cpuinfo 不可用${NC}"
    fi
}

# ── 系统负载 ──
show_load() {
    print_header "📈 系统负载"

    if [[ -f /proc/loadavg ]]; then
        local load1 load5 load15 running procs
        read load1 load5 load15 running procs < /proc/loadavg
        echo -e "  ${BOLD}1 分钟平均负载:${NC} ${load1}"
        echo -e "  ${BOLD}5 分钟平均负载:${NC} ${load5}"
        echo -e "  ${BOLD}15 分钟平均负载:${NC} ${load15}"
        echo -e "  ${BOLD}运行/总进程数:${NC} ${running} / ${procs}"

        local cores
        cores=$(nproc)
        echo -e ""
        echo -e "  ${BOLD}负载/核心比:${NC}"
        echo -e "    1 分钟: $(echo "$load1 $cores" | awk '{printf "%.2f", $1/$2}') (每核心)"
        echo -e "    5 分钟: $(echo "$load5 $cores" | awk '{printf "%.2f", $1/$2}') (每核心)"
        echo -e "    15 分钟: $(echo "$load15 $cores" | awk '{printf "%.2f", $1/$2}') (每核心)"

        # 负载评估
        echo -e ""
        local load_val=${load1%.*}
        if (( load_val > cores * 2 )); then
            echo -e "  ⚠️  状态: ${RED}系统负载过高！${NC}"
        elif (( load_val > cores )); then
            echo -e "  ⚠️  状态: ${YELLOW}系统负载偏高${NC}"
        else
            echo -e "  ✅ 状态: ${GREEN}系统负载正常${NC}"
        fi
    else
        echo -e "  ${RED}无法读取系统负载${NC}"
    fi
}

# ── Top 进程 ──
show_top() {
    print_header "🔝 CPU 占用 Top 10 进程"

    echo -e "  ${BOLD}PID${NC}  ${BOLD}USER${NC}  ${BOLD}CPU%${NC}  ${BOLD}MEM%${NC}  ${BOLD}COMMAND${NC}"
    echo -e "  ─────────────────────────────────────────────────"

    ps aux --sort=-%cpu | awk 'NR>1 && NR<=11 {
        printf "  %-8s %-10s %-8s %-8s %s\n", $2, $1, $3, $4, $11
    }'
}

# ── 全部信息 ──
show_all() {
    show_usage
    show_info
    show_load
    show_top
}

# ── 帮助信息 ──
show_help() {
    echo ""
    echo -e "${BOLD}CPU 信息查看工具${NC}"
    echo ""
    echo "用法: $0 [选项]"
    echo ""
    echo "选项:"
    echo -e "  ${GREEN}-u, --usage${NC}    查看 CPU 实时使用率"
    echo -e "  ${GREEN}-i, --info${NC}     查看 CPU 详细信息"
    echo -e "  ${GREEN}-l, --load${NC}     查看系统负载"
    echo -e "  ${GREEN}-t, --top${NC}      查看 CPU 占用最高的进程 (Top 10)"
    echo -e "  ${GREEN}-a, --all${NC}      查看以上所有信息 (默认)"
    echo -e "  ${GREEN}-h, --help${NC}     显示帮助信息"
    echo ""
    echo "示例:"
    echo "  $0              # 显示全部信息"
    echo "  $0 -u           # 仅查看 CPU 使用率"
    echo "  $0 -i -l        # 查看 CPU 信息和系统负载"
    echo ""
}

# ── 主逻辑 ──
main() {
    local show_all_flag=false

    case "${1:-}" in
        -u|--usage)
            show_usage
            ;;
        -i|--info)
            show_info
            ;;
        -l|--load)
            show_load
            ;;
        -t|--top)
            show_top
            ;;
        -a|--all|"")
            show_all
            ;;
        -h|--help)
            show_help
            ;;
        *)
            echo -e "${RED}错误: 未知选项 '${1}'${NC}"
            echo ""
            show_help
            exit 1
            ;;
    esac

    echo ""
    echo -e "${BOLD}──────────────────────────────────────────${NC}"
    echo -e "  查看时间: $(date '+%Y-%m-%d %H:%M:%S')"
    echo -e "${BOLD}──────────────────────────────────────────${NC}"
    echo ""
}

main "$@"