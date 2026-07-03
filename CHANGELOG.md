# Changelog

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
