# API-Switch

> Claude Code 多模型代理 — 一键切换 DeepSeek、Qwen（通义千问）、GLM（智谱）、Moonshot（月之暗面）等国产大模型，协议自动转换，零配置即用。

接收 Anthropic 格式请求，根据模型名自动路由到对应提供商，协议自动转换，模型即切即用。

```
claude → api-switch → ├─ claude-*  → Anthropic API (透传)
                       ├─ gpt-4o    → 转 OpenAI 协议 → OpenAI API → 转回 Anthropic 格式
                       ├─ deepseek  → 转 OpenAI 协议 → DeepSeek API → 转回 Anthropic 格式
                       └─ qwen      → 转 OpenAI 协议 → DashScope API → 转回 Anthropic 格式
```

## ✨ 特性

- **零配置切换模型** — `api-switch use <model>` 即刻切换，Claude Code 热加载
- **多提供商支持** — Anthropic、OpenAI、DeepSeek、Qwen、Moonshot 等任意 OpenAI 兼容 API
- **协议自动转换** — Anthropic ↔ OpenAI 双向转换，含流式 SSE、工具调用、图片
- **Provider 模板** — 内置 9+ 国产厂商预设，一条命令添加
- **实时监控** — Web 仪表盘 + 终端实时日志
- **热加载配置** — 修改配置文件自动生效，无需重启
- **一键诊断** — `api-switch doctor` 快速定位配置问题
- **批量导入** — 自动拉取 API 可用模型列表
- **端到端测试** — `api-switch test [model]` 验证代理是否正常工作
- **provider 连通性检测** — `api-switch provider test <name>` 测试厂商 API 是否可达

## ⚡ 快速开始

### 方式一：setup 一条命令（推荐）

```bash
# 安装
npm install -g @hxz0727/api-switch-cc

# DeepSeek — 自动填充 base_url、type、建议模型
api-switch setup deepseek --key sk-xxx

# 启动
api-switch serve
```

### 方式二：分步操作

```bash
# 1. 添加提供商
api-switch provider add deepseek --key sk-xxx

# 2. 导入模型
api-switch model import deepseek

# 3. 切换模型，启动代理
api-switch use deepseek-chat
api-switch serve
```

之后正常使用 `claude` 命令即可，所有请求自动通过代理路由。

## 📦 安装

### npm 安装（推荐）

```bash
# 全局安装
npm install -g @hxz0727/api-switch-cc

# 或直接使用（无需安装）
npx @hxz0727/api-switch-cc serve
```

npm 包自动下载对应平台的预编译二进制，无需 Go 环境。

### 一键安装脚本

```bash
curl -sSL https://raw.githubusercontent.com/hxz0727/API-Switch/master/install.sh | bash
```

脚本会自动：检查/安装 Go → 克隆仓库 → 编译二进制 → 添加到 PATH。

### Docker 部署

```bash
docker build -t api-switch .
docker run -d -p 8080:8080 \
  -v ~/.api-switch.yaml:/root/.api-switch.yaml \
  -v ~/.claude:/root/.claude \
  api-switch
```

### 手动编译

```bash
git clone https://github.com/hxz0727/API-Switch.git
cd API-Switch
make build              # 或 go build -o api-switch ./cmd/api-switch/
make install            # 安装到 ~/.local/bin/
```

## 🎯 命令参考

### 无参数运行（新手引导）

```bash
$ api-switch

API-Switch — LLM API proxy for Claude Code
================================================

快速开始：
  # 1. 添加一个供应商（支持已知厂商预设）
  api-switch provider add deepseek --key sk-xxx
  ...
```

### Provider 管理

```bash
# 从预设模板添加（自动填充 base_url 和 type，未传 --key 会交互式输入）
api-switch provider add deepseek --key sk-xxx
api-switch provider add qwen
# → Enter API key for "qwen": _

# 支持的厂商
# deepseek  → https://api.deepseek.com
# qwen      → https://dashscope.aliyuncs.com/compatible-mode/v1
# moonshot  → https://api.moonshot.cn/v1
# glm       → https://open.bigmodel.cn/api/paas/v4
# kimi      → https://api.moonshot.cn/v1
# yi        → https://api.lingyiwanwu.com/v1
# step      → https://api.stepfun.com/v1
# ernie     → https://aip.baidubce.com/rpc/2.0/ai_custom/v1/wenxinworkshop/chat
# hunyuan   → https://api.hunyuan.cloud.tencent.com/v1

# 自定义提供商
api-switch provider add my-provider \
  --url https://my-api.com/v1 \
  --type openai \
  --key sk-xxx

# 查看所有提供商
api-switch provider list          # 也可用 provider ls

# 测试提供商连通性
api-switch provider test deepseek # 也可用 provider ping
```

### 模型管理

```bash
# 列出所有模型
api-switch model list          # 也可用 model ls

# 添加模型路由
api-switch model add deepseek-chat deepseek

# 批量导入（自动查询 API 的 /v1/models）
api-switch model import deepseek
api-switch model import openai --filter gpt-4   # 只导入 gpt-4 相关模型

# 删除模型
api-switch model remove deepseek-chat
```

### 模型切换

```bash
# 查看所有可用模型（→ 标记当前激活的）
api-switch use

# 切换到指定模型
api-switch use gpt-4o
api-switch use deepseek-chat
api-switch use qwen-plus
```

### setup 一键配置

```bash
# 已知厂商（自动填充 type、url、建议模型）
api-switch setup deepseek --key sk-xxx

# 自定义厂商
api-switch setup --name custom --type openai --url https://... --key sk-xxx --models m1,m2

# 以上命令会自动：
#   1. 添加 provider 到配置
#   2. 添加模型到路由表
#   3. 生成 Claude Code settings.json
```

### 端到端测试

```bash
# 测试当前激活的模型
api-switch test

# 测试指定模型
api-switch test deepseek-chat
# → ✓ Response received (stop_reason=end_turn, input=56, output=10)
```

### 启动服务

```bash
# 默认 8080 端口
api-switch serve

# 自定义端口 + 日志级别
api-switch serve -p 9090 -vv          # debug 级别日志
api-switch serve -q                     # 仅显示错误
api-switch serve -v                     # 带请求详情

# 配置热加载：修改 ~/.api-switch.yaml 后自动生效
```

### 监控与诊断

```bash
# Web 仪表盘（浏览器打开 http://localhost:8080/admin/）
api-switch monitor --web

# 终端实时监控
api-switch monitor

# 一键诊断
api-switch doctor
```

### 配置管理

```bash
api-switch config show                # 查看配置（key 脱敏）
api-switch config cat                 # 同上，别名
api-switch config set providers.openai.api_key sk-xxx
api-switch config init                # 创建默认配置
```

## 🌐 Web 仪表盘

启动服务后访问 `http://localhost:8080/admin/`：

- 实时请求列表（SSE 推送）
- 请求总数、平均耗时、模型分布统计
- 每条请求的模型、Provider、耗时、状态

## 🔧 场景示例

### 混用 Anthropic + DeepSeek

```bash
# 配置
api-switch provider add anthropic --key sk-ant-xxx --url https://api.anthropic.com --type anthropic
api-switch provider add deepseek --key sk-xxx

# 使用
api-switch use deepseek-chat   # 切换到 DeepSeek
api-switch serve
# Claude Code 会话中随时切换：
api-switch use claude-sonnet-4-20250514  # 切回 Claude
```

### 只用国产模型（Qwen + DeepSeek）

```bash
api-switch setup qwen --key sk-xxx
api-switch setup deepseek --key sk-xxx
api-switch use qwen-plus
api-switch serve
```

### 实时切换工作流

```bash
# 终端 1（代理服务）
api-switch serve

# 终端 2（随时切换模型）
api-switch use gpt-4o           # 切换到 GPT-4o
# Claude Code 自动生效
api-switch use deepseek-chat    # 切换到 DeepSeek
# 也立即生效
```

## 📊 架构

```
                    ┌─ Anthropic Client (透传) ──→ Anthropic API
                    │
claude ──→ /v1/messages ──┤
       POST               │                          ┌─ 请求转换 ──┐
                           └─ OpenAI Client ──→  ──→ │  OpenAI 协议  │ ──→ OpenAI/DeepSeek 等
                                                     └─ 响应转换 ──┘

管理端点:
  /health          → 健康检查 (JSON: models, providers, requests)
  /admin/          → Web 仪表盘
  /admin/stats     → JSON 统计
  /admin/events    → SSE 实时事件
  /admin/reload    → 热加载配置 (POST)
```

## ⚙️ 核心概念

### 配置文件

| 文件 | 说明 |
|---|---|
| `~/.api-switch.yaml` | API-Switch 配置（提供商、模型路由） |
| `~/.claude/settings.json` | Claude Code 配置（由 `api-switch use` 管理） |

### 两种提供商类型

| 类型 | 说明 |
|---|---|
| `anthropic` | 直连 Anthropic API，无协议转换 |
| `openai` | 走 OpenAI 兼容协议，请求/响应自动双向转换 |

### 协议转换支持

| 特性 | 支持 | 说明 |
|---|---|---|
| Text 消息 | ✅ | 双向转换 |
| 流式 SSE | ✅ | OpenAI ↔ Anthropic SSE |
| 工具调用 | ✅ | tools / tool_choice / tool_use / tool_result |
| 流式工具调用 | ✅ | input_json_delta streaming |
| 图片 | ✅ | Anthropic image → OpenAI image_url |
| System 消息 | ✅ | 自动置顶 |

## 🩺 常见问题

### 如何验证代理是否正常工作？

```bash
# 一键测试（推荐）
api-switch test deepseek-chat

# 健康检查
curl http://localhost:8080/health

# 完整诊断
api-switch doctor
```

### 修改配置后需要重启吗？

不需要。API-Switch 会自动监听 `~/.api-switch.yaml` 的文件变更，500ms 防抖后自动重载。也可以手动触发：

```bash
curl -X POST http://localhost:8080/admin/reload
```

### 如何查看实时流量？

```bash
# Web 仪表盘
api-switch monitor --web

# 终端实时日志
api-switch monitor
```

### 切换模型后需要重启 Claude Code 吗？

不需要。Claude Code 热加载 `~/.claude/settings.json`，`api-switch use` 切换后立即生效。

### 如何修改端口？

```bash
# 方案一：配置文件
api-switch config set server.port 9090

# 方案二：运行时指定（推荐）
api-switch serve -p 9090
api-switch use gpt-4o    # 自动使用配置中的端口
```

### 如何更新 API Key？

```bash
api-switch config set providers.openai.api_key sk-new-key
```

### 配置热加载不生效？

确保文件是保存到原路径而非临时文件。大部分编辑器（vim/nano）直接保存可以触发。如果使用 VS Code Remote，可能需要手动触发：

```bash
curl -X POST http://localhost:8080/admin/reload
```

## 📁 项目结构

```
cmd/api-switch/main.go              # CLI 入口
internal/
├── config/
│   ├── config.go                   # 配置类型、加载、保存、路由
│   ├── claude_config.go            # Claude Code 配置管理
│   └── doctor.go                   # 一键诊断
├── logutil/
│   └── logger.go                   # 分级日志
├── monitor/
│   └── tracker.go                  # 请求事件追踪
├── provider/
│   ├── anthropic.go                # Anthropic API 客户端
│   └── openai.go                   # OpenAI API 客户端
├── proxy/
│   ├── handler.go                  # HTTP 代理 + 请求处理
│   ├── admin.go                    # 管理端点（仪表盘、SSE、监控）
│   ├── converter.go                # Anthropic ↔ OpenAI 请求转换
│   ├── response.go                 # OpenAI → Anthropic 响应转换
│   └── router.go                   # 模型路由
└── streaming/
    └── sse.go                      # SSE 流式转换
pkg/
├── anthropic/types.go              # Anthropic 类型定义
└── openai/types.go                 # OpenAI 类型定义
```
