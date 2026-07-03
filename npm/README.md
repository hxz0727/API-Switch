# api-switch-cc

> Claude Code 多模型代理 — 一键切换 DeepSeek、Qwen、GLM、SenseNova、NVIDIA 等大模型。协议自动转换，零配置即用。
> LLM API proxy for Claude Code — route to DeepSeek, Qwen, GLM, SenseNova, NVIDIA and more with automatic protocol conversion.

```
npm install -g api-switch-cc

api-switch setup deepseek --key sk-xxx
api-switch provider known              # 查看 13 个内置厂商
api-switch use deepseek-chat
api-switch serve
```

## Usage

```bash
# Install globally
npm install -g api-switch-cc

# Or run directly
npx api-switch-cc setup agnes --key sk-xxx
npx api-switch-cc serve
```

## Features

- **13 built-in provider presets** — DeepSeek, Qwen, GLM, SenseNova (商汤), NVIDIA (英伟达), Moonshot, Agnes AI (free), Kimi, Yi, StepFun, ERNIE, Hunyuan, APIFree
- **Auto sync settings.json** — serve startup automatically syncs Claude Code base URL
- **Port persistence** — `serve -p <port>` saves to config; `--no-save-port` to skip
- **Auto-update** — checks for new versions on startup, auto-downloads and restarts
- **Graceful shutdown** — SIGINT/SIGTERM signal handling
- **Protocol conversion** — Anthropic ↔ OpenAI bidirectional (SSE streaming, tool calls, text)
- **DeepSeek compatible** — auto-strips orphaned tool_calls for strict providers
- **Hot-reload config** — changes take effect without restart
- **Web dashboard** — real-time request monitoring at `/admin/` (localhost only)
- **Usage tracking** — daily token statistics with cache hit rates
- **Context retention** — tested across all configured models

## Quick Start

```bash
# 1. Install
npm install -g api-switch-cc

# 2. See available providers
api-switch provider known

# 3. Add a provider
api-switch provider add deepseek --key sk-xxx

# 4. Import models
api-switch model import deepseek

# 5. Switch model & start
api-switch use deepseek-chat
api-switch serve               # default :8080, auto-syncs settings.json
```

## Commands

| Command | Description |
|---------|-------------|
| `api-switch serve` | Start proxy (default :8080) |
| `api-switch serve -p 9000` | Start on custom port, persist to config |
| `api-switch use <model>` | Switch active model in Claude Code |
| `api-switch provider known` | List 13 built-in providers |
| `api-switch provider list` | List configured providers |
| `api-switch provider add` | Add provider (supports presets) |
| `api-switch doctor` | Run diagnostics |
| `api-switch monitor` | View real-time traffic |
| `api-switch usage` | Token usage statistics |
| `api-switch update` | Update to latest version |

See the [full documentation](https://github.com/hxz0727/API-Switch) on GitHub.

## Supported platforms

- Linux (amd64, arm64)
- macOS (amd64, arm64)
- Windows (amd64)
