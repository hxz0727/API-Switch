# api-switch-cc

> Claude Code 多模型代理 — 一键切换 DeepSeek、Qwen（通义千问）、GLM（智谱）、Moonshot（月之暗面）等国产大模型。协议自动转换，零配置即用。
> LLM API proxy for Claude Code — route to DeepSeek, Qwen, GLM, Moonshot and more with automatic protocol conversion.

```
npm install -g api-switch-cc

api-switch provider add deepseek --key sk-xxx
api-switch use deepseek-chat
api-switch serve
```

## Usage

```bash
# Install globally
npm install -g api-switch-cc

# Or run directly
npx api-switch-cc provider add qwen --key sk-xxx
npx api-switch-cc serve
```

See the [full documentation](https://github.com/hxz0727/API-Switch) on GitHub.

## Supported platforms

- Linux (amd64, arm64)
- macOS (amd64, arm64)
- Windows (amd64)
