# Claude Code 最被低估的痛点：模型切换，我开源了一个工具解决它

> 3 条命令，无缝切换 DeepSeek、Qwen、GLM 等 10+ 国产大模型，零重启，零感知。

---

用 Claude Code 的人都有一个共同的体验：**贵**。

随便聊一个下午，几十块钱就没了。想切到 DeepSeek 省点钱？对不起，Claude Code 只认 Anthropic 格式，你得手改配置文件、手写协议转换、还得自己搭代理。

市面上也有方案，比如 CC Switch，功能很全，但它是**桌面应用**——服务器上用不了，CI 里跑不了，而且你得打开一个 GUI 去点来点去。

我想要的很简单：**终端里敲一行命令，模型就换了，继续干活**。

于是花了一个周末，写了 **API-Switch**。

---

## 这是什么？

一句话：**Claude Code 的模型切换代理**。

```
claude → api-switch → ├─ claude-*  → Anthropic API（透传）
                       ├─ deepseek  → DeepSeek API（自动转协议）
                       ├─ qwen      → 通义千问（自动转协议）
                       └─ gpt-4o    → OpenAI API（自动转协议）
```

它做的事情很简单：

1. 你正常用 `claude` 命令
2. 请求先到 API-Switch
3. 根据当前激活的模型，自动路由到对应厂商
4. Anthropic ↔ OpenAI 协议自动转换，包括流式 SSE、工具调用、图片
5. 你**完全无感知**

---

## 有多简单？

```bash
# 安装（秒完成）
npm install -g api-switch-cc

# 配置 DeepSeek
api-switch setup deepseek --key sk-xxx

# 启动
api-switch serve

# 切换模型（Claude Code 即时生效，无需重启）
api-switch use qwen-plus
api-switch use deepseek-chat
api-switch use glm-4-plus
```

**就三步。** 不用改配置文件，不用写转换逻辑，不用重启 Claude Code。

---

## 协议转换到底做了什么？

这是整个工具最硬核的部分。Anthropic 和 OpenAI 是两套完全不同的 API 协议：

| 差异 | Anthropic | OpenAI |
|---|---|---|
| 请求格式 | `messages` 数组 + `system` 字符串 | `messages` 数组（含 system role） |
| 工具调用 | `tool_use` content block | `tool_calls` 数组 |
| 流式格式 | SSE event 类型（message_start/delta/stop） | SSE choices delta |
| 图片 | base64 `source` 对象 | `image_url` 对象 |
| 停止原因 | `stop_reason` 字段 | `finish_reason` 字段 |

API-Switch 实现了**完整的双向协议转换**，覆盖了上面每一个差异点。而且流式 SSE 的转换是**逐 token 转发**的——你不会等到全部生成完才看到第一个字。

这意味着什么？你在 Claude Code 里用 DeepSeek，体验和用原生 Claude 几乎一模一样：工具调用正常、流式输出正常、图片理解正常。

---

## 不只是切换器

虽然"模型切换"是核心卖点，但实际用起来你会发现它远不止于此：

### 10+ 厂商预设，一条命令添加

```
deepseek · qwen · moonshot · glm · kimi · yi · step
ernie · hunyuan · apifree（免费 API 聚合）
```

每个预设都自动填好了 base_url、协议类型、建议模型。你只需要提供 API Key。

### 实时监控

```bash
# Web 仪表盘
api-switch monitor --web

# 终端实时日志
api-switch monitor
```

每条请求的模型、厂商、耗时、状态一目了然。

### 用量统计

```bash
api-switch usage
```

输出：

```
  API-Switch 用量统计
  =======================================================

  日期                请求数     Token数   缓存命中       出错
  ------------------------------------------------------------
  2026-06-08         23      89000 8 次/32K        -
  2026-06-07         18      62000 5 次/18K        1

  总用量：
    请求数:        41
    Token数:       151000 (输入 98000 + 输出 53000)
    缓存命中:      13 次 (50K tokens)
    缓存命中率:    31.7%
```

哪个模型烧钱最快，缓存命中率多少，一目了然。

### 一键诊断

```bash
api-switch doctor
```

自动检查：Go 版本、配置文件、API Key 格式、网络连通性、模型路由、Claude Code 配置。出问题了不用猜，一键定位。

### 后台运行

```bash
api-switch start       # 后台启动
api-switch stop        # 停止
api-switch status      # 查看状态
api-switch logs        # 查看日志
api-switch restart     # 重启
```

关掉终端也不会停。适合服务器上长期跑。

---

## 技术架构

```
                    ┌─ Anthropic Client（透传）──→ Anthropic API
                    │
claude ──→ /v1/messages ──┤
       POST               │
                           └─ OpenAI Client ──→ OpenAI/DeepSeek/Qwen...
                                ↑
                          协议转换层（请求/响应/SSE/工具调用/图片）
```

全 Go 实现，单二进制，无运行时依赖。编译出来 10MB，启动内存 < 20MB。

**核心模块：**

| 模块 | 功能 |
|---|---|
| `proxy/handler.go` | HTTP 代理，请求分发 |
| `proxy/converter.go` | Anthropic → OpenAI 请求转换 |
| `proxy/response.go` | OpenAI → Anthropic 响应转换 |
| `streaming/sse.go` | SSE 流式双向转换 |
| `config/doctor.go` | 一键诊断引擎 |
| `usage/tracker.go` | Token 用量持久化追踪 |
| `daemon/daemon.go` | 后台进程管理 |

---

## 和 CC Switch 的区别

CC Switch 是桌面 GUI 应用，功能全面（支持 5 种 CLI 工具、MCP 管理、云同步），适合在个人电脑上"管理"多个 AI 工具。

API-Switch 是**纯 CLI 工具**，专注做一件事：**在终端里用一行命令切换 Claude Code 的模型**。

| | API-Switch | CC Switch |
|---|---|---|
| 形态 | CLI | 桌面 GUI |
| 平台 | macOS/Linux/Windows | macOS/Linux/Windows |
| 目标用户 | 终端重度用户、服务器部署 | 桌面用户、多工具管理 |
| Claude Code 支持 | ✅ 核心功能 | ✅ |
| 其他 CLI 工具 | ❌ | ✅ Codex/Gemini/OpenCode 等 |
| 安装方式 | npm/go install/源码 | brew/msi/deb/rpm |
| 国内镜像 | ✅ Gitee + goproxy.cn | ❌ |
| 二进制大小 | ~10MB | ~50MB+ |
| 后台运行 | ✅ daemon | ✅ 系统托盘 |

---

## 开源 & 安装

**GitHub**: https://github.com/hxz0727/API-Switch  
**Gitee 镜像**: https://gitee.com/776311606/API-Switch  
**npm**: `npm install -g api-switch-cc`  
**版本**: v0.4.0（2026-06-08）

### 三种安装方式

```bash
# 方式一：npm（推荐，秒装）
npm install -g api-switch-cc

# 方式二：Go 安装（国内走 goproxy.cn 加速）
GOPROXY=https://goproxy.cn,direct go install github.com/hxz0727/API-Switch/cmd/api-switch@v0.4.0

# 方式三：一键脚本（国内优先 Gitee 镜像）
curl -sSL https://gitee.com/776311606/API-Switch/raw/master/install.sh | bash
```

---

## 写在最后

这个项目的初衷很简单：**Claude Code 很好用，但我不想被 Anthropic 绑定**。

DeepSeek 便宜 10 倍，Qwen 中文更好，GLM 代码能力不差——为什么不能想用哪个用哪个？

API-Switch 就是答案。它不是什么"平台"或"生态"，就是一个**专注做好一件事的小工具**：让你在 Claude Code 里自由切换任何模型。

如果你也在用 Claude Code，试试它。如果觉得好用，给个 Star ⭐。

有问题直接提 Issue，PR 欢迎。

---

*项目地址：https://github.com/hxz0727/API-Switch*  
*国内镜像：https://gitee.com/776311606/API-Switch*
