# Changelog

## v0.9.3 (2026-07-19)

### 关键修复: 多轮工具调用

- **CRITICAL: 现代 tool_result 格式被丢弃**: Claude Code 发送 `tool_result` 块在 `user` role 内，转换器只处理旧版顶层 `tool_result` 格式，导致所有多轮工具调用在一轮后失效。修复：从 user 消息中提取 tool_result 块并生成独立的 `tool` role OpenAI 消息
- **CRITICAL: 孤立 tool_calls 被误删**: 与上一条组合，valid assistant tool_calls 被剥离。修复：检查后续任意位置是否存在 tool 消息，而不仅看相邻位置
- **`is_error` 标志**: tool_result 的 `is_error: true` 现在添加 `[Tool Error]` 前缀

### 连接池和性能

- **HTTP Transport 调优**: 自定义 `MaxIdleConns=100/per-host=20`、TLSHandshakeTimeout、ResponseHeaderTimeout，防止连接池耗尽
- **Goroutine 泄漏修复**: `closeOnCancel` 改用 `context.AfterFunc`，避免每次流请求泄漏一个 goroutine
- **Anthropic ctx 传播**: 新增 `StreamMessageWithContext`，handler 传入客户端 ctx，客户端断开立即取消上游

### 版本管理修复

- **update/version 无限循环**: npm 包装器用严格版本比对 (`===`)，`api-switch update` 升级二进制后，下一次 `api-switch version` 检测到版本不匹配又会回退旧版。修复：改用 `semverGte()`，二进制版本 ≥ npm 包版本时直接接受
- **Gitee build 版本标签**: `tryGiteeBuild` 从克隆代码时嵌入 git tag 版本而非 npm 包版本

### SenseNova 兼容性

- **流式 reasoning 字段**: `textDelta = Content || Reasoning`，自动回退
- **非流式 reasoning 字段**: 新增 `Reasoning` 字段到 `openai.Message`，非流式响应也支持回退

### 测试覆盖

- 集成测试 27 个（多轮、错误、混合块、速率限制、SSE 取消等）
- 端到端冒烟测试 2 个（真实 httptest + 真实 Server）
- Provider 单元测试 29 个（HTTP 客户端 87% 覆盖）
- Config 单元测试 16 个（86.8% 覆盖）
- Admin 单元测试 22 个（85.5% 覆盖）

---

## v0.9.2 (2026-07-14)

### 安全加固 (代码审计)

- **速率限制**: 修复按连接而非 IP 限速的绕过漏洞，改用滑动窗口算法
- **Token 比对**: 使用时序安全的 `crypto/subtle.ConstantTimeCompare`
- **并发安全**: 使用 `atomic.Value` + `sync.Once` 消除全局状态和 config 热点数据竞争
- **IPv6 检测**: 修复 `::1` 前缀匹配误判非 loopback 地址，统一使用 `net.IP.IsLoopback()`
- **密钥保护**: 密钥文件损坏时拒绝静默覆盖，报错并提示手动删除

### 流稳定性

- **写错误检测**: SSE 流写入错误不再静默丢弃，检测到立即终止
- **服务端超时**: 配置 `WriteTimeout=0` 保证长连接，`ReadTimeout=30s` 防止慢速攻击
- **上游取消**: OpenAI 流式传输新增 `closeOnCancel`，客户端断开时取消上游
- **Anthropic 流**: StreamMessage 先拷贝再设 `Stream=true`，不再污染调用方

### 转换修复

- **图片转换**: Anthropic image block → OpenAI image_url 双向支持 base64 和 URL
- **tool_result**: 修复 plain string 格式丢失 ToolCallID，正确处理嵌套 content block
- **top_k**: 保留 Anthropic 的 top_k 参数传递给 OpenAI 兼容 API
- **空字符串加密**: 区分"未加密空值"和"加密空值"，消除歧义
- **更新降级**: checksums.txt 不可用时自动降级为直接下载，不阻断更新

### 工具链

- **release.sh**: 全新 6 阶段自动发布脚本，含前置检查（测试/vet/版本一致性）

- **图片转换**: Anthropic image block → OpenAI image_url 双向支持 base64 和 URL
- **tool_result**: 修复 plain string 格式丢失 ToolCallID，正确处理嵌套 content block
- **top_k**: 保留 Anthropic 的 top_k 参数传递给 OpenAI 兼容 API
- **空字符串加密**: 区分"未加密空值"和"加密空值"，消除歧义

### 文档更新

- 更新 README 项目结构和快速开始
- 添加服务器安全配置章节
- npm README 新增安全功能说明

---

## v0.9.1 (2026-07-14)

### 安全增强

- **API Key 加密存储**: 使用 AES-256-GCM 加密算法存储 API 密钥，密钥文件权限 0600
- **Bearer Token 认证**: 支持为 API 端点配置认证 Token (`server.auth_token`)
- **更新安全验证**: 自动更新时验证 SHA256 校验和，防止供应链攻击
- **管理端点加固**: 检查 X-Forwarded-For 和 X-Real-IP 头，防止反向代理绕过
- **速率限制**: 每IP每分钟 100 请求限制，防止 DoS 攻击 (`server.rate_limit`)
- **TLS/HTTPS 支持**: 支持配置 TLS 证书和密钥 (`server.tls_cert`, `server.tls_key`)

### 功能改进

- **图片转换**: 实现完整的 Anthropic image block → OpenAI image_url 格式转换
  - 支持 base64 图片数据 (`data:image/png;base64,...`)
  - 支持 URL 图片直接传递
  - 完整的多模态 Vision 功能支持
- **status 命令增强**: 显示当前激活模型和配置统计信息
- **配置选项扩展**: 新增 `rate_limit`, `tls_cert`, `tls_key` 配置项

### 代码重构

- **main.go 模块化**: 将 1672 行拆分为 8 个独立文件
  - `main.go`: 命令定义和入口
  - `serve.go`: serve/daemon 相关命令
  - `provider.go`: provider/model 管理命令
  - `use.go`: use/setup 命令
  - `config_cmd.go`: 配置管理命令
  - `update.go`: 更新命令
  - `doctor.go`: 诊断命令
  - `monitor.go`: 实时监控命令

### 新增文件

- `internal/secrets/encrypt.go`: API Key 加密模块
- `internal/secrets/encrypt_test.go`: 加密模块测试
- `internal/proxy/ratelimit.go`: 请求速率限制中间件

### 测试更新

- 修复工具调用测试用例（添加 tool_result 消息）
- 更新图片转换测试用例（验证多模态格式）
- 更新更新测试用例（SHA256 校验和验证）

### 配置文件示例

```yaml
server:
  port: 8080
  auth_token: ""      # API 认证 token (可选)
  rate_limit: 100     # 每分钟每IP请求数限制 (0=禁用)
  tls_cert: ""        # TLS 证书路径 (可选)
  tls_key: ""         # TLS 密钥路径 (可选)
```

---

## v0.8.0 (2026-07-03)

### 稳定性修复

- **SSE 流转换**: 修复 content_block index 错误（文本+工具调用共存、纯工具调用起始 index）
- **SSE 流终止**: 修复 `[DONE]` 无 finish_reason 时客户端永久挂起
- **路由空指针**: 修复缺失 provider client 时 Route() 返回 nil, nil 导致崩溃
- **DeepSeek 兼容**: 自动清除孤立 tool_calls（DeepSeek 要求每个 tool_calls 后必须有 tool 消息）
- **URL 构建**: 修复 base_url 以 `/v1/chat/completions/` 结尾时的错误 URL 拼接
- **错误信息泄漏**: 错误响应截断至 500 字符，移除解码错误中的完整原始 body

### 安全加固

- **Admin 端点**: 限制仅 localhost 可访问（/admin/reload, /admin/stats, /admin/events, /admin/）
- **Health 端点**: 添加读写锁防止数据竞争
- **工具调用格式**: input_json_delta 使用 partial_json 字段（修正 text 字段错误）
- **未知 finish_reason**: 使用 "end_turn" 作为兜底（不再透传 provider 特定值）

### 新功能

- **端口自动同步**: `serve` 启动时自动同步 `settings.json` 的 ANTHROPIC_BASE_URL
- **端口持久化**: `serve -p <port>` 自动写入配置文件（`--no-save-port` 跳过）
- **默认端口**: 不指定 -p 时始终使用 8080
- **StreamMessageWithContext**: 支持 context 取消，不再修改调用方 struct
- **MaxTokens 保底**: double-zero 时默认 1024，避免无限生成

### 厂商支持

- 新增 **SenseNova（商汤 商量）** 和 **NVIDIA（英伟达）** 到已知厂商
- **`provider known`** 命令显示 13 个内置厂商预设
- NVIDIA 模型: mimimax-m3, step-3.7-flash, kimi-k2.6, glm-5.1
- SenseNova 模型: sensenova-6.7-flash-lite, deepseek-v4-flash
- 移除 models.yaml 中的 anthropic/moonshot/yi/step/ernie/openai

### 转换优化

- **reasoning 字段**: 支持 SenseNova reasoner 模型的 reasoning delta
- **tool_choice**: 畸形 JSON 或空 type 兜底为 "auto"
- **tool_result**: 多 block content 全部保留（不再只取第一个）
- **系统消息**: 支持 Anthropic system 数组格式（Claude Code standard）

### 测试

- `test_context.py`: 全模型上下文保持测试脚本
- `models.yaml`: 完整配置模板（占位 key）
- 多轮对话 + 工具调用端到端验证通过

### 修复清单

- 移除 `closeOnCancel` 防止连接关闭错误
- `mapFinishReason` 未知值兜底
- `usage` 在 finish_reason 前正确捕获
- `GenerateMessageID` 移除碰撞风险说明
- Daemon 管理二进制参数校验
- 编译时的 `strings` 去重优化

---

## v0.5.0 (2026-06-22)

### 安装与更新重构

本次版本彻底修复了安装和更新流程中的多个长期问题，大幅提升国内用户体验。

#### 安装优先级调整
- **Gitee 优先**：安装时优先从 Gitee 镜像下载二进制，国内用户无需等待 GitHub 超时
- **版本化目录**：Gitee release 分支采用 `release/vX.Y.Z/` 目录结构，每个版本的二进制独立存放，杜绝版本错配
- **版本严格校验**：下载后校验二进制魔数（ELF/Mach-O/PE），防止 HTML 错误页面被当作二进制执行；正则精确匹配版本号，不再使用宽松的 `includes`

#### 更新命令修复
- 修复 `api-switch update` 因 `.bin` 目录不存在导致临时文件创建失败
- 修复 `go build -o` 遇到上次下载残留文件时报 `already exists and is not an object file`
- 修复 `go install` 因模块缓存返回旧版本的问题，增加 `go clean -modcache`
- 修复 Gitee API 版本检查顺序（Gitee 优先于 GitHub）

#### 代码质量
- 修复 `bash -c` 命令注入风险，改用 `exec.Command` + 环境变量
- `install.js` 中 `tryGoInstall` 和 `tryGiteeBuild` 的错误不再被静默吞掉
- 修复 `install.js` 中硬编码的旧版本号 `v0.2.1`
- `bump.sh` 中 Gitee release 分支不再 `rm -rf *` 清空，改为追加版本子目录

#### 文档
- 更新 README 版本说明、安装方式、更新机制描述

---

## v0.4.8

- 修复 npm wrapper 更新循环问题

## v0.4.7

- 新增 Agnes AI 支持
- 自动更新机制
- 优雅关闭（SIGINT/SIGTERM）
- 全面单元测试覆盖
