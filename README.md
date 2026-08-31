# vikunja-skill

[![CI](https://github.com/LanceLoo/vikunja-skill/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/LanceLoo/vikunja-skill/actions/workflows/ci.yml) [![Release](https://img.shields.io/github/v/release/LanceLoo/vikunja-skill)](https://github.com/LanceLoo/vikunja-skill/releases) [![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)](https://go.dev/) [![Vikunja API](https://img.shields.io/badge/Vikunja%20API-v2-7D4CDB)](https://vikunja.io/) [![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

为 Vikunja 提供 Go 命令行工具 `vikunja-ops`，面向 Agent 调用，也可由人类用户直接使用。

## 功能概述

- 连接诊断：`doctor`
- 项目：`projects list|get|create|update|delete`
- 标签：`labels list`
- 任务：`tasks list|get|create|update|complete|bulk-update|delete`
- 任务关系：`tasks relations`、`tasks relations create|delete`
- 任务资源：`tasks labels`、`tasks comments`
- 附件：`tasks attachments`、`tasks attachments download|upload|delete`
- 写入保护：默认预览，执行写入时须显式使用 `--apply`

## 运行要求

| 要求 | 说明 |
| --- | --- |
| 执行方式 | 使用 GitHub Releases 提供的可执行文件；从源码构建或运行时需要 Go |
| Vikunja | 可访问的 Vikunja v2 API |
| 网络 | 生产环境或非完全受信的网络应使用 HTTPS |

## 快速开始

### 1. 获取程序

从 [GitHub Releases](https://github.com/LanceLoo/vikunja-skill/releases) 下载适合当前平台的压缩包并解压。Windows 程序名为 `vikunja-ops.exe`，Linux 程序名为 `vikunja-ops`。

也可以从源码构建：

```sh
go build ./cmd/vikunja-ops
```

或在源码目录直接运行：

```sh
go run ./cmd/vikunja-ops -- doctor
```

### 2. 配置连接

在准备运行命令的目录中，以 `.env.example` 为参考创建 `./.env`，填写完整的服务地址和 Token。

### 3. 检查连接

Linux：

```sh
./vikunja-ops doctor
```

Windows PowerShell：

```powershell
.\vikunja-ops.exe doctor
```

### 4. 执行只读查询

```sh
./vikunja-ops projects list
./vikunja-ops tasks list --page 1 --per-page 20
```

如果程序已加入 `PATH`，可将示例中的 `./vikunja-ops` 或 `.\vikunja-ops.exe` 简写为 `vikunja-ops`。

## 配置

`vikunja-ops` 只读取进程当前工作目录中的 `./.env`。该文件必须同时包含 `VIKUNJA_URL` 和 `VIKUNJA_TOKEN`：

```dotenv
VIKUNJA_URL=https://vikunja.example.com
VIKUNJA_TOKEN=replace-with-your-api-token
```

配置规则：

- 进程环境变量中的同名值会被忽略。
- 不搜索父目录、可执行文件目录、源码目录或 Skill 目录。
- 应先进入存放受信 `./.env` 的目录，再运行程序。
- Token 不通过命令行参数配置。
- 不要提交包含真实凭据的 `.env`，也不要在日志或对话中输出 Token。
- 使用满足操作需要的最小权限 Token。
- 除明确受信的本地或隔离网络外，应使用 HTTPS。

## 使用约定

成功结果默认输出紧凑 JSON。需要缩进格式时，将全局 `--pretty` 放在命令之前：

```sh
vikunja-ops --pretty projects list
```

查看帮助和版本：

```sh
vikunja-ops --help
vikunja-ops projects --help
vikunja-ops tasks --help
vikunja-ops --version
```

顶层帮助写入 stdout 并以退出码 `0` 结束。顶层命令缺失或参数格式错误时，错误写入 stderr 并以退出码 `2` 结束。

## 常用命令

以下 ID 和内容均为示例。先使用只读命令核对目标，再执行写入预览。

### 诊断与只读查询

```sh
# 连接诊断
vikunja-ops doctor

# 项目、标签和任务
vikunja-ops projects list --page 1 --per-page 50
vikunja-ops projects get 123
vikunja-ops labels list --q "重要"
vikunja-ops tasks list --project 123 --query "报告"
vikunja-ops tasks get 456
```

### 项目管理

```sh
# 创建根项目预览
vikunja-ops projects create --title "新项目"

# 确认预览后执行
vikunja-ops projects create --title "新项目" --apply

# 更新根项目标题预览
vikunja-ops projects update 123 --title "新标题"

# 递归删除预览，输出 confirmation_token
vikunja-ops projects delete 123

# 确认预览后执行删除
vikunja-ops projects delete 123 --apply --confirm <preview-confirmation-token>
```

### 任务管理

```sh
# 创建、更新和完成预览
vikunja-ops tasks create 123 --title "整理周报" --priority 2
vikunja-ops tasks update 456 --title "整理月报"
vikunja-ops tasks complete 456

# 不需要 confirmation token 的写入
vikunja-ops tasks create 123 --title "整理周报" --priority 2 --apply
vikunja-ops tasks complete 456 --apply

# 批量更新预览与执行
vikunja-ops tasks bulk-update --ids 456,457 --priority 3
vikunja-ops tasks bulk-update --ids 456,457 --priority 3 --apply --confirm <preview-confirmation-token>

# 单任务删除预览与执行
vikunja-ops tasks delete --id 456 --project-id 123
vikunja-ops tasks delete --id 456 --project-id 123 --apply --confirm <preview-confirmation-token>
```

### 关系、标签与评论

```sh
# 只读
vikunja-ops tasks relations 456
vikunja-ops tasks labels 456 --page 1 --per-page 50
vikunja-ops tasks comments 456 --order-by desc

# 创建关系预览与执行
vikunja-ops tasks relations create --task-id 456 --other-task-id 457 --relation-kind related
vikunja-ops tasks relations create --task-id 456 --other-task-id 457 --relation-kind related --apply --confirm <preview-confirmation-token>

# 删除关系预览
vikunja-ops tasks relations delete --task-id 456 --other-task-id 457 --relation-kind related
```

### 附件

```sh
# 附件列表
vikunja-ops tasks attachments 456 --page 1 --per-page 50

# 下载预览与执行，目标文件必须尚不存在
vikunja-ops tasks attachments download --task-id 456 --attachment-id 12 --output ./report.pdf
vikunja-ops tasks attachments download --task-id 456 --attachment-id 12 --output ./report.pdf --apply

# 上传预览与执行
vikunja-ops tasks attachments upload --task-id 456 --file ./report.pdf
vikunja-ops tasks attachments upload --task-id 456 --file ./report.pdf --apply --confirm <preview-confirmation-token>

# 删除预览与执行
vikunja-ops tasks attachments delete --task-id 456 --attachment-id 12
vikunja-ops tasks attachments delete --task-id 456 --attachment-id 12 --apply --confirm <preview-confirmation-token>
```

## 写入安全

`doctor` 和读取命令仅发送 GET 请求。会改变远程数据或写入本地文件的命令默认只输出预览；只有显式提供 `--apply` 才执行操作。

以下操作还要求 `--confirm`：

- `projects delete`
- `tasks delete`
- `tasks bulk-update`
- `tasks relations create`、`tasks relations delete`
- `tasks attachments upload`、`tasks attachments delete`

先运行不带 `--apply` 的命令，从预览结果取得 `confirmation_token`。核对目标和变更内容后，将该值传给同一操作的 `--confirm`。confirmation token 只表示对该次预览意图的确认，不是并发锁或服务端状态锁；目标在预览与执行之间仍可能变化。

`projects create`、`projects update`、`tasks create`、`tasks update`、`tasks complete` 和附件下载不使用 confirmation token，但仍应先预览，再决定是否使用 `--apply`。

### 递归删除项目

`projects delete <id>` 的范围是目标项目及其递归后代，不包括祖先或兄弟项目。操作必须先预览范围，再使用预览返回的 token 配合 `--apply --confirm` 执行。

项目删除非原子，不自动重试，也不提供回滚。执行前应确认目标、后代范围和当前权限，并避免在预览与执行之间修改相关项目结构。

## 故障排除

| 问题 | 检查与处理 |
| --- | --- |
| 提示缺少配置或无法读取 `.env` | 确认当前工作目录存在 `./.env`，且同时填写了 `VIKUNJA_URL` 和 `VIKUNJA_TOKEN`；不要依赖进程环境变量或其他目录中的文件 |
| Token 无效或权限不足 | 检查 Token 是否有效，并确认其具备目标项目或任务所需权限；必要时创建权限范围合适的新 Token |
| 无法连接 Vikunja | 检查 URL、DNS、端口、代理、防火墙、TLS 证书和 Vikunja 服务状态，然后运行 `doctor` |
| 命令只输出预览 | 核对预览后添加 `--apply`；需要 confirmation token 的操作还要使用本次预览返回的 `--confirm` 值 |
| confirmation 校验失败 | 重新运行预览，确认参数与目标未变化，并使用新预览返回的完整 token；不要复用旧 token |

## Agent 集成

Agent 的调用边界、安全流程和场景速查见 [SKILL.md](SKILL.md)。

## 许可证

本项目采用 [MIT License](LICENSE)。

## 贡献

欢迎通过 [Issues](https://github.com/LanceLoo/vikunja-skill/issues) 报告问题，或提交 Pull Request。

## 开发

开发命令应在仓库根目录运行，需要 Go `1.24`：

```sh
go test ./...
go vet ./...
gofmt -l .
go build ./...
```

`gofmt -l .` 必须不产生任何输出。以上验证命令不需要真实凭据；手动运行 CLI 时，需要按前文配置仓库根目录中受信的 `./.env`。
