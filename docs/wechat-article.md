# Claude Code 一年花了我 2000 块，直到我用上这个开源工具

用 Claude Code 有一段时间了，最大的感受是：**好用，但真的贵**。

一个下午写代码，几十块钱就没了。想省点切换到 DeepSeek？你会发现这不是"改个 API Key"这么简单。

Claude Code 底层用的是 Anthropic Messages API，和 OpenAI 系的 API（DeepSeek、Qwen、GLM 等）是完全不同的两套协议。不是换个 base_url 就能通的，你需要在中间加一个**协议转换层**。

市面上有方案，比如 CC Switch，功能挺全，但它是桌面 GUI 应用，服务器上用不了。有时候你只是想在终端里敲一行命令换个模型，并不想打开一个窗口点来点去。

后来在 GitHub 上看到一个叫 **API-Switch** 的工具，思路很清晰：起一个本地代理，Claude Code 的请求先经过它，它根据当前激活的模型自动路由到对应厂商，同时做协议转换。全命令行操作，Go 写的，一个二进制文件，没有运行时依赖。

---

## 它做了什么

先说核心逻辑：

```
claude → api-switch → claude-*  → Anthropic API（透传）
                    → deepseek  → DeepSeek API（自动转协议）
                    → qwen      → 通义千问（自动转协议）
                    → gpt-4o    → OpenAI API（自动转协议）
```

API-Switch 在本地监听一个端口（默认 8080），Claude Code 的所有请求先打到这个端口上，它根据请求中的模型名来决定路由到哪个厂商。

Anthropic 原生模型（claude-*）直接透传，不做任何修改。非 Anthropic 的模型，它会自动完成 Anthropic ↔ OpenAI 的双向协议转换。

---

## 协议转换具体做了什么

这可能是整个工具最硬核的部分。Anthropic 和 OpenAI 的 API 差异远不止字段名不同：

**请求层面：**
- Anthropic 的 `system` 是请求体的一个独立字段，OpenAI 系把 system 放在 `messages` 数组里作为一个 role
- Anthropic 的工具调用定义在 `tools` 字段里，返回时是 `tool_use` content block；OpenAI 系用 `tool_calls` 数组，结构完全不同
- 图片的表示方式也不一样：Anthropic 用 `source` 对象嵌 base64，OpenAI 系用 `image_url` 对象

**响应层面：**
- 流式 SSE 的事件结构完全不一样：Anthropic 用 `message_start`、`content_block_delta`、`message_delta`、`message_stop` 四种事件类型，OpenAI 系用 `choices[0].delta` 的增量更新
- Anthropic 的 `stop_reason` vs OpenAI 的 `finish_reason`
- Token 用量统计字段也不同

API-Switch 的协议转换覆盖了以上所有差异点，而且是**逐 token 转发**的——不是等全部生成完才吐结果，流式体验和原生 Anthropic 几乎一致。

工具调用也能正常转换。你在 Claude Code 里让 DeepSeek 帮你读文件、执行命令，流程不会断。

---

## 上手体验

安装很简单：

```bash
npm install -g api-switch-cc
```

npm 安装是秒级的——它没有 postinstall 阻塞步骤，二进制在首次运行时才自动下载。

然后配置一个厂商：

```bash
api-switch setup deepseek --key sk-xxx
```

这个命令会自动完成三件事：添加 provider、导入可用模型、生成 Claude Code 的配置文件。如果你用的是已知厂商（deepseek、qwen、moonshot、glm 等），连 base_url 和协议类型都不用手填。

启动：

```bash
api-switch serve
```

然后正常用 `claude` 命令就行。想换模型：

```bash
api-switch use qwen-plus
```

Claude Code 即时生效，不用重启。

---

## 还有几个实用功能

**用量统计**

```bash
api-switch usage
```

按天展示 Token 消耗，包含输入/输出拆分、缓存命中次数和命中率。能直观看到哪个模型烧钱最快。

**后台运行**

```bash
api-switch start    # 启动
api-switch stop     # 停止
api-switch status   # 状态
api-switch restart  # 重启
api-switch logs     # 日志
```

关掉终端也不会停，适合长期跑在服务器上。

**一键诊断**

```bash
api-switch doctor
```

自动检查 Go 版本、配置文件完整性、API Key 格式、网络连通性、模型路由是否正确。出问题了不用挨个排查，一个命令全搞定。

**实时监控**

```bash
api-switch monitor        # 终端实时日志
api-switch monitor --web  # Web 仪表盘
```

能看到每条请求的模型、厂商、耗时、状态，排查问题很方便。

**国内友好**

npm 安装如果 GitHub 下载失败，会自动走 Gitee 克隆编译。go install 也会走 goproxy.cn 代理。Gitee 上有完整的镜像仓库，和 GitHub 自动同步。

---

## 和 CC Switch 的定位差异

这不是一个"谁更好"的问题，是使用场景不同。

CC Switch 是一个桌面应用，适合在个人电脑上统一管理多个 AI CLI 工具（Claude Code、Codex、Gemini CLI 等），有 GUI、有 MCP 管理、有云同步。

API-Switch 是纯命令行的，只做一件事：在终端里快速切换 Claude Code 的模型。没有 GUI，没有多工具管理，就是一行命令。如果你习惯在服务器上跑、在 CI 里跑、或者单纯不想用 GUI，它更轻量。

---

## 项目信息

- GitHub：https://github.com/hxz0727/API-Switch
- Gitee 镜像：https://gitee.com/776311606/API-Switch
- npm 包：`api-switch-cc`
- 版本：v0.4.7
- 语言：Go，单二进制 ~10MB，启动内存 < 20MB
- 许可：MIT

```bash
# npm（推荐）
npm install -g api-switch-cc

# Go 安装（国内走 goproxy.cn）
GOPROXY=https://goproxy.cn,direct go install github.com/hxz0727/API-Switch/cmd/api-switch@v0.4.7

# 一键脚本（优先 Gitee 镜像）
curl -sSL https://gitee.com/776311606/API-Switch/raw/master/install.sh | bash
```

项目还比较早期，但核心的协议转换和路由已经跑通了。如果你也在用 Claude Code，可以试试看。有 Bug 提 Issue，有好想法提 PR，欢迎一起完善。
