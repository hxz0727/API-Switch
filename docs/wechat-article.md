# Claude Code 一个隐藏痛点：模型切换

用 Claude Code 有一段时间了，最大的感受是：**好用，但真的贵**。

一个下午写代码，几十块钱就没了。想省点切换到 DeepSeek？你会发现这不是"改个 API Key"这么简单。

---

## 为什么会这么麻烦？

Claude Code 底层用的是 Anthropic Messages API，请求/响应的格式和 OpenAI 系（DeepSeek、Qwen、GLM 等）完全不同：

- Anthropic 的 `system` 是独立字段，OpenAI 系放在 `messages` 里
- Anthropic 的工具调用叫 `tool_use`，OpenAI 系叫 `tool_calls`
- 流式 SSE 的事件结构完全不一样
- 图片、停止原因……细节差异一堆

所以你想在 Claude Code 里用 DeepSeek，光配置 API Key 是不够的。你需要在中间加一个**协议转换层**，把 Claude Code 发出的 Anthropic 格式请求转成 OpenAI 格式，再把响应转回来。

市面上有方案。CC Switch 是个桌面应用，功能挺全，但它是 GUI 的，服务器上用不了。而且有时候你只是想在终端里敲一行命令换个模型，并不想打开一个窗口点来点去。

---

## 我的解决方案

花了一个周末写了个小工具，叫 **API-Switch**。

思路很简单：起一个本地代理，Claude Code 的请求先经过它，它根据当前激活的模型自动路由到对应厂商，同时做协议转换。

```
claude → api-switch → claude-*  → Anthropic API（透传）
                    → deepseek  → DeepSeek API（自动转协议）
                    → qwen      → 通义千问（自动转协议）
                    → gpt-4o    → OpenAI API（自动转协议）
```

用起来是这样的：

```bash
npm install -g api-switch-cc
api-switch setup deepseek --key sk-xxx
api-switch serve
```

然后正常用 `claude` 命令就行。想换模型：

```bash
api-switch use qwen-plus
```

一行命令，Claude Code 即时生效，不用重启。

---

## 顺便做了几个实用功能

写完核心的协议转换之后，又加了一些日常用得到的东西：

**用量统计** — 按天看 Token 消耗，哪个模型烧钱最快、缓存命中率多少，一目了然。

**一键诊断** — `api-switch doctor` 自动检查 Go 版本、配置文件、API Key 格式、网络连通性，出问题不用猜。

**后台运行** — `api-switch start/stop/status/restart`，关掉终端也不会停。

**实时监控** — 终端实时日志 + Web 仪表盘，能看到每条请求的路由和耗时。

**国内友好** — Gitee 镜像同步，npm 安装失败会自动走 Gitee 克隆编译，go install 走 goproxy.cn 代理。

---

## 和 CC Switch 的区别

这不是一个"谁更好"的问题，是**使用场景不同**。

CC Switch 适合在个人电脑上统一管理多个 AI CLI 工具，有 GUI、有 MCP 管理、有云同步。

API-Switch 是纯命令行的，专注一件事：在终端里快速切换 Claude Code 的模型。如果你习惯在服务器上跑、在 CI 里跑、或者单纯不喜欢 GUI，可能更适合你。

---

## 项目信息

GitHub：https://github.com/hxz0727/API-Switch  
Gitee 镜像：https://gitee.com/776311606/API-Switch  
npm：`npm install -g api-switch-cc`  
当前版本：v0.4.0，Go 实现，单二进制 ~10MB

安装方式：

```bash
# npm（最快）
npm install -g api-switch-cc

# Go（国内走代理）
GOPROXY=https://goproxy.cn,direct go install github.com/hxz0727/API-Switch/cmd/api-switch@v0.4.0

# 一键脚本
curl -sSL https://gitee.com/776311606/API-Switch/raw/master/install.sh | bash
```

---

项目开源，欢迎提 Issue 和 PR。如果觉得有用，给个 Star。
