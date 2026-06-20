# api-switch-cc

> Claude Code 多模型代理 — 一键切换 DeepSeek、Qwen、GLM、Moonshot、Agnes AI 等大模型。协议自动转换，零配置即用。
> LLM API proxy for Claude Code — route to DeepSeek, Qwen, GLM, Moonshot, Agnes AI and more with automatic protocol conversion.

```
npm install -g api-switch-cc

api-switch setup deepseek --key sk-xxx
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

- **11 built-in provider presets** — DeepSeek, Qwen, GLM, Moonshot, Agnes AI (free), Kimi, Yi, StepFun, ERNIE, Hunyuan, APIFree
- **Auto-update** — checks for new versions on startup, auto-downloads and restarts
- **Graceful shutdown** — SIGINT/SIGTERM signal handling
- **Protocol conversion** — Anthropic ↔ OpenAI bidirectional (SSE streaming, tool calls, images)
- **Hot-reload config** — changes take effect without restart
- **Web dashboard** — real-time request monitoring at `/admin/`
- **Usage tracking** — daily token statistics with cache hit rates

See the [full documentation](https://github.com/hxz0727/API-Switch) on GitHub.

## Supported platforms

- Linux (amd64, arm64)
- macOS (amd64, arm64)
- Windows (amd64)
