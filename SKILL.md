---
name: vikunja-ops
description: "Use vikunja-ops for Vikunja doctor checks; read-only queries of projects, labels, tasks, relations, comments, and attachments; and preview/apply controlled writes for the implemented project, task, relation, and attachment operations."
compatibility: "Requires a caller-provided vikunja-ops binary or a Go source execution environment explicitly authorized by the caller, plus a reachable Vikunja server configured by VIKUNJA_URL and VIKUNJA_TOKEN in ./.env."
---

# vikunja-ops AI 调用指南

## 适用范围

本指南供 AI 调用当前项目实现的 `vikunja-ops` CLI，用于 Vikunja 的诊断、只读查询和受控写入。命令事实与参数以 CLI 的 `--help` 和 `cmd/vikunja-ops/usage.go` 为准，不推断未实现能力。

## 配置与敏感信息

运行需要：

- `VIKUNJA_URL`
- `VIKUNJA_TOKEN`

当前实现只读取进程当前工作目录的 `./.env`；该文件必须同时包含有效的 `VIKUNJA_URL` 和 `VIKUNJA_TOKEN`。进程环境变量会被忽略；CLI 不搜索可执行文件、源码或 skill 所在目录。

不得从任意或无关用户项目目录调用绝对路径 binary 并依赖该目录的 `.env`；不得依赖进程环境变量，不得把 Token 作为命令参数传递，也不得自动读取内容、复制或创建 `.env`。在确认当前目录的 `.env` 由受信来源完整配置前，不得运行 `doctor`，因为它会使用 Token 发送 GET 请求。

不得提交或输出真实 URL、Token、`Authorization` 请求头或敏感响应；引用执行结果前先移除敏感信息。生产环境或非完全受信网络应使用 HTTPS；Bearer Token 配合 HTTP 仅限明确受信的本机或隔离开发环境。

## 选择执行入口

1. 优先使用调用方明确提供的 `vikunja-ops` 可执行文件，包括 `PATH` 中的命令或指定的绝对路径；仍须遵守上述当前工作目录配置边界。
2. 仅当位于本项目源码环境且调用方明确授权使用 Go 时，使用 `go run ./cmd/vikunja-ops -- <args>`。不得自动构建；如需本地构建产物，必须另行取得对构建及输出位置的明确授权。
3. 环境不明确时，不下载 release、不假定 Go 已安装，也不自行切换入口；请求调用方确定执行方式。

`doctor` 检查配置、OpenAPI 可访问性、Token 验证和项目读取能力，且仅发送 GET；它不证明二进制版本或发行完整性。

全局 `--pretty` 必须位于命令之前：`vikunja-ops --pretty <command> ...`，不能放在命令之后。

`vikunja-ops --version` 可读取程序版本。顶层 `--help` 写入 stdout；顶层参数错误或缺失命令写入 stderr，并以退出码 `2` 结束。调用方不得把错误文本当作成功 JSON。

## 场景速查

| 场景 | 命令 | 操作要求 |
| --- | --- | --- |
| 诊断、确认连接 | `doctor` | 只读 |
| 查项目 | `projects list`、`projects get` | 只读 |
| 查标签 | `labels list` | 只读 |
| 查任务 | `tasks list`、`tasks get` | 只读 |
| 查任务关系 | `tasks relations` | 只读 |
| 查任务标签、评论、附件元数据 | `tasks labels`、`tasks comments`、`tasks attachments` | 只读 |
| 下载附件 preview/apply | `tasks attachments download` | 先 preview，后经用户明确确认才 `--apply`；apply 不使用 confirmation token，写入单个本地文件且不覆盖已有目标 |
| 创建、更新、完成任务 | `tasks create`、`tasks update`、`tasks complete` | 先 preview，后经用户明确确认才 `--apply` |
| 创建、更新项目 | `projects create`、`projects update` | 先 preview，后经用户明确确认才 `--apply` |
| 批量更新任务 | `tasks bulk-update` | 先 preview，后经用户明确确认才 `--apply` |
| 写入任务关系 | `tasks relations create`、`tasks relations delete` | 先 preview，后经用户明确确认才 `--apply` |
| 上传、删除附件 | `tasks attachments upload`、`tasks attachments delete` | 先 preview，后经用户明确确认才 `--apply` |
| 删除任务 | `tasks delete` | 先 preview，后经用户明确确认才 `--apply` |
| 删除项目 | `projects delete` | 先 preview，后经用户明确确认才 `--apply` |

## ID 与目标边界

项目 ID、任务 ID 和附件 ID 表示不同资源，不能互换。操作前应使用对应的只读命令确认目标；附件可通过 `tasks attachments <task-id>` 确认元数据。`tasks delete` 除任务 ID 外还要求对应的项目 ID；`projects delete` 只接受正整数项目 ID。`projects create` 仅创建根项目；`projects update` 仅更新根项目标题。

## 必须遵守的调用流程

1. 先确认执行入口，并在不输出值的前提下确认当前工作目录的 `./.env` 同时包含由同一受信来源提供的 `VIKUNJA_URL` 和 `VIKUNJA_TOKEN`；不能确认时停止并请求调用方配置。
2. 配置来源确认后运行 `doctor`。
3. 写入前先用只读命令确认项目、任务和附件等目标 ID。
4. 对任何写入或删除先运行 preview，不带 `--apply`。
5. 检查 preview 中的目标与影响范围，并取得用户对该次操作的明确确认。
6. 确认后才运行 `--apply`。不得自行发送 DELETE，不得复用旧 confirmation token。
7. apply 后只陈述实际响应和实际回读观察。不能由 DELETE 204 或随后 GET 404 推断删除因果或完整级联。

所有写入默认 preview，但仅在目标子命令要求时使用 confirmation token：

- **apply 不要求 confirmation token：**`projects create|update`、`tasks create|update|complete`、`tasks attachments download`。
- **apply 要求当前 preview 匹配的 confirmation token：**`projects delete`、`tasks delete`、`tasks bulk-update`、`tasks relations create|delete`、`tasks attachments upload|delete`。

confirmation token 不是服务端状态锁，不能阻止 preview 与 apply 之间的并发变更。

## 安全示例

以下假定调用方已明确提供 `vikunja-ops`。ID、标题和 token 均须替换为用户确认的值。

```sh
# 环境检查（仅 GET）
vikunja-ops doctor

# 只读确认
vikunja-ops projects list
vikunja-ops projects get 123
vikunja-ops tasks get 456

# 创建任务 preview：不带 --apply
vikunja-ops tasks create 123 --title "待用户确认的标题"

# 项目删除 preview：先检查输出范围和 confirmation_token
vikunja-ops projects delete 123
```

只有用户明确确认该次 preview 后，才可使用该次预览返回的 token：

```sh
vikunja-ops projects delete 123 --apply --confirm <preview-confirmation-token>
```

不得把占位符替换为旧 token，也不得自动化确认或删除步骤。

## 项目删除的特殊边界

- 为验证层级，安全扫描读取调用者可见的项目全集，可能包含祖先或兄弟项目元数据。
- 删除意图和 preview 影响清单只覆盖目标项目及其递归后代；任务、视图和桶只扫描该选中子树。
- confirmation token 绑定该 qualified snapshot/intent，并额外绑定实例身份和安全扫描取得的默认项目身份；它不批准删除祖先或兄弟项目。默认项目落入选中子树时，安全扫描会拒绝该操作。
- preview 是 **qualified caller-observable scope**：它只描述通过安全检查的调用者可观察范围，不能证明服务端没有不可见级联。
- 每次 DELETE 传输尝试后都会独立执行一次 GET；成功输出要求 DELETE 204 且该 GET 404。404 只表示目标当前不可读，不能证明删除因果或完整级联。
- 操作不重试、非原子、无并发控制且无回滚。

## 已知限制

- CLI 不实现应用层自动重试、并发控制或回滚。
- 删除命令不支持一次指定多个独立删除目标，也不支持按筛选条件选择删除目标；这不否定 `projects delete` 对已批准目标子树的影响范围。
- 受限 Token 的 HTTP 403 权限边界尚待补充验证。

这些限制是当前使用边界，不得将其解释为对服务端行为、操作结果或未来实现的保证。
