# Changelog

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
