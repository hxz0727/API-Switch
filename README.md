# API-Switch

面向 Claude Code 的 LLM API 代理工具。接收 Anthropic 格式请求，根据模型名自动路由到对应提供商，协议自动转换，模型即切即用。

```
claude → api-switch → ├─ claude-*  → Anthropic API (透传)
                       ├─ gpt-4o    → 转 OpenAI 协议 → OpenAI API → 转回 Anthropic 格式
                       └─ deepseek  → 转 OpenAI 协议 → DeepSeek API → 转回 Anthropic 格式
```

## 快速开始

```bash
# 1. 添加一个提供商（Anthropic 直连）
api-switch setup \
  --name anthropic --type anthropic \
  --url https://api.anthropic.com \
  --key sk-ant-xxx \
  --models claude-sonnet-4-20250514

# 2. 添加另一个提供商（OpenAI，自动协议转换）
api-switch setup \
  --name openai --type openai \
  --url https://api.openai.com \
  --key sk-xxx \
  --models gpt-4o

# 3. 切换模型，启动代理
api-switch use gpt-4o   # Claude Code settings.json 自动更新
api-switch serve         # 启动本地代理
```

然后正常使用 `claude` 即可。Claude Code 的请求自动通过代理，路由到对应模型。

### 切换模型

```bash
# 查看所有可用模型
api-switch use

# 切换到指定模型（自动更新 ~/.claude/settings.json）
api-switch use gpt-4o
api-switch use claude-sonnet-4-20250514
api-switch use deepseek-chat

# Claude Code 热加载配置，立即生效
```

## 核心概念

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

### settings.json 示例

```json
{
  "$schema": "https://json.schemastore.org/claude-code-settings.json",
  "model": "gpt-4o",
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:8080",
    "ANTHROPIC_API_KEY": "use-api-switch",
    "ANTHROPIC_CUSTOM_MODEL_OPTION": "gpt-4o"
  }
}
```

- `ANTHROPIC_BASE_URL` — 指向本地代理
- `ANTHROPIC_API_KEY` — 占位值，真实 key 在 API-Switch 配置中
- `ANTHROPIC_CUSTOM_MODEL_OPTION` — 非 Claude 模型显示在 `/model` 选择器中
- `model` — 当前激活模型

## 命令参考

### `api-switch use [model]`

切换 Claude Code 的当前模型，自动更新 `~/.claude/settings.json`。

```bash
api-switch use                # 列出所有模型，标记当前激活的
api-switch use gpt-4o         # 切换到 gpt-4o
api-switch use deepseek-chat  # 切换到 DeepSeek
```

### `api-switch setup`

添加一个提供商及其模型到配置。

```bash
api-switch setup --name openai --type openai \
  --url https://api.openai.com --key sk-xxx \
  --models gpt-4o,gpt-4
```

| 参数 | 说明 |
|---|---|
| `--name` | 提供商名称 |
| `--type` | `anthropic` 或 `openai` |
| `--url` | API 基础 URL |
| `--key` | API Key |
| `--models` | 逗号分隔的模型名 |
| `-p` | 代理端口（默认 8080） |

### `api-switch serve`

启动代理服务。

```bash
api-switch serve          # 默认 8080
api-switch serve -p 9090  # 自定义端口
```

### `api-switch model`

```bash
api-switch model list                      # 列出所有模型
api-switch model add gpt-4o openai         # 添加模型路由
api-switch model remove gpt-4o             # 移除模型
```

### `api-switch provider`

```bash
api-switch provider list  # 列出所有提供商及 key 状态
```

### `api-switch config`

```bash
api-switch config show                           # 显示配置（key 脱敏）
api-switch config set providers.openai.api_key sk-xxx
api-switch config init                           # 创建默认配置
```

## 场景示例

### 只用 OpenAI

```bash
api-switch setup --name openai --type openai \
  --url https://api.openai.com --key sk-xxx \
  --models gpt-4o

api-switch use gpt-4o
api-switch serve
```

### 混用 Anthropic + DeepSeek

```bash
api-switch setup --name anthropic --type anthropic \
  --url https://api.anthropic.com --key sk-ant-xxx \
  --models claude-sonnet-4-20250514

api-switch setup --name deepseek --type openai \
  --url https://api.deepseek.com --key sk-xxx \
  --models deepseek-chat

api-switch use deepseek-chat   # 切换到 DeepSeek
api-switch serve
```

### 在 Claude Code 中实时切换

```bash
# 终端 1（永久运行）
api-switch serve

# 终端 2 或直接在 Claude Code 中
api-switch use gpt-4o          # 切换到 GPT-4o
# Claude Code 热加载，立即生效

api-switch use claude-sonnet   # 切回 Claude
# 也立即生效
```

## 常见问题

### 切换模型后需要重启 Claude Code 吗？

不需要。Claude Code 热加载 `~/.claude/settings.json`，`api-switch use` 切换后立即生效。

### 代理验证

```bash
curl http://localhost:8080/health
# → ok

curl http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: use-api-switch" \
  -d '{"model":"gpt-4o","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'
```

### 修改端口

```bash
# 方案一
api-switch config set server.port 9090
api-switch generate-claude-config -p 9090

# 方案二
api-switch serve -p 9090
api-switch use gpt-4o    # 自动使用配置中的端口
```

### 更新 API Key

```bash
api-switch config set providers.openai.api_key sk-new-key
```

## 项目结构

```
cmd/api-switch/main.go              # CLI 入口
internal/config/config.go           # API-Switch 配置
internal/config/claude_config.go    # Claude Code 配置管理
internal/proxy/handler.go           # HTTP 代理 + 请求处理
internal/proxy/converter.go         # Anthropic→OpenAI 请求转换
internal/proxy/response.go          # OpenAI→Anthropic 响应转换
internal/proxy/router.go            # 模型路由
internal/provider/anthropic.go      # Anthropic API 客户端
internal/provider/openai.go         # OpenAI API 客户端
internal/streaming/sse.go           # SSE 流式转换
pkg/anthropic/types.go              # Anthropic 类型
pkg/openai/types.go                 # OpenAI 类型
```
