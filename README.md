# vikunja-ops

`vikunja-ops` 是面向 Vikunja 的 Go CLI。本文只说明当前实现；完整参数与约束以 `vikunja-ops --help`、各命令的 `--help` 及 `cmd/vikunja-ops/usage.go` 为准。

## 配置与构建

当前实现只读取进程当前工作目录中的 `./.env`。`VIKUNJA_URL` 和 `VIKUNJA_TOKEN` 必须同时存在且有效；缺少任一项都会报错。进程环境变量会被忽略，CLI 也不会按可执行文件、源码或 skill 所在目录搜索 `.env`。

本地操作者可参考 `.env.example` 配置完整的一对值，例如：

```dotenv
VIKUNJA_URL=http://your-vikunja-server:3456
VIKUNJA_TOKEN=
```

生产环境或非完全受信网络应使用 HTTPS。由于 CLI 使用 Bearer Token 请求服务，HTTP 仅适合明确受信的本机或隔离开发环境。

不要提交真实凭据，也不要在文档、日志或对话中写入真实 URL、Token、`Authorization` 请求头或含敏感信息的响应。

不要依赖进程环境变量，也不要把 Token 作为命令参数传递。在源码仓库中使用 Go 时，应将完整 `.env` 放在该源码仓库根目录，并从该目录运行 `go run ./cmd/vikunja-ops -- doctor`。若完整 skill 位于 `.agents/skills/vikunja-ops/`，只有已获准使用的 `vikunja-ops.exe` 实际位于该 skill 目录时，才可将 `.env` 放在其中并从该目录调用该二进制；不能假定该目录存在 `cmd/`，或把它作为 `go run` 的工作目录。不应从无关项目目录通过绝对路径调用该可执行文件并依赖该目录的 `.env`。

调用环境若已通过 `PATH` 或绝对路径提供 `vikunja-ops`，可直接调用。源码环境可构建：

```sh
go build ./cmd/vikunja-ops
```

在源码环境且明确选择使用 Go 时，也可直接运行，例如：

```sh
go run ./cmd/vikunja-ops -- doctor
```

## 已实现命令

- `doctor`
- `projects list|get|create|update|delete`
- `labels list`
- `tasks list|get|create|update|complete|bulk-update|delete`
- `tasks relations`，以及 `tasks relations create|delete`
- `tasks labels`
- `tasks comments`
- `tasks attachments`，以及 `tasks attachments download|upload|delete`

使用 `vikunja-ops <group> --help` 查看参数详情。全局 `--pretty` 必须放在命令前；它会将成功的 JSON 结果以两空格缩进输出：

```sh
vikunja-ops --pretty projects list
```

顶层 `vikunja-ops --version` 输出程序版本。顶层 `--help` 输出到 stdout；无法识别的顶层参数、缺失命令等错误输出到 stderr，并以退出码 `2` 结束。

## Release 制品

Release workflow 只接受严格的最终版本标签 `vMAJOR.MINOR.PATCH`，不接受预发布或附加元数据标签。流程在构建时注入版本，并打包以下目标：

- Windows amd64
- Linux amd64
- Linux arm64

每个压缩包包含对应二进制以及 `README.md`、`SKILL.md`、`.env.example`、`LICENSE`，同时发布校验和。流程会校验制品清单、校验和，以及解压后的 Linux amd64 二进制。

Windows amd64 二进制已经交叉编译并完成本地测试，但当前没有原生远程 Windows CI job；这是 v1 发布接受的限制，后续应补充原生 Windows CI 验证，不能将现状描述为 Windows CI 已验证。

## 安全模型

- `doctor` 仅发送 GET 请求。只读命令也仅发送 GET 请求。
- 写入命令默认执行 preview；只有显式提供 `--apply` 才执行写入。
- `projects create|update`、`tasks create|update|complete` 和 `tasks attachments download` 的 `--apply` 不使用 confirmation token。
- `projects delete`、`tasks delete`、`tasks bulk-update`、`tasks relations create|delete`、`tasks attachments upload|delete` 的 apply 必须使用与当前预览匹配的 confirmation token。
- confirmation token 不是服务端状态锁；预览与 apply 之间仍可能发生并发变更。

### 项目删除边界

为验证项目层级，`projects delete` 的安全扫描会读取调用者可见的项目全集，其中可能包含祖先或兄弟项目的元数据。删除意图和 preview 影响清单只覆盖目标项目及其递归后代；任务、视图和桶也只扫描该选中子树。confirmation token 绑定该 qualified snapshot/intent，并额外绑定实例身份和安全扫描取得的默认项目身份；它不批准删除祖先或兄弟项目。安全扫描会拒绝默认项目落入选中子树的操作。

预览描述的是 **qualified caller-observable scope**（通过安全检查的、调用者可观察范围）；服务端仍可能存在 CLI 不可见的级联。每次 DELETE 传输尝试后都会独立执行一次 GET；成功输出要求 DELETE 返回 HTTP 204 且该 GET 返回 404。GET 404 只表示目标当前对调用者不可读，不能证明由本次命令导致，也不能证明级联完整。

项目删除不重试、非原子、无并发控制、无回滚。

## 已知限制

- CLI 不实现应用层自动重试、并发控制或回滚。
- 删除命令不支持一次指定多个独立删除目标，也不支持按筛选条件选择删除目标；这不否定 `projects delete` 对已批准目标子树的影响范围。
- 受限 Token 的 HTTP 403 权限边界仍待补充验证。
