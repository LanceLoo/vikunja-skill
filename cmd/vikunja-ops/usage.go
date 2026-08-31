package main

import (
	"fmt"
	"io"
)

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "vikunja-ops: Vikunja CLI")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "提供 Vikunja 只读查询和需显式 --apply 的任务写入。")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops --help")
	fmt.Fprintln(out, "  vikunja-ops --version")
	fmt.Fprintln(out, "  vikunja-ops --pretty <command> ...")
	fmt.Fprintln(out, "  vikunja-ops doctor")
	fmt.Fprintln(out, "  vikunja-ops projects --help")
	fmt.Fprintln(out, "  vikunja-ops labels --help")
	fmt.Fprintln(out, "  vikunja-ops tasks --help")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "全局选项:")
	fmt.Fprintln(out, "  --pretty  必须置于命令前；以两空格缩进输出成功的 JSON 结果。")
	fmt.Fprintln(out, "  --version 输出版本。")
}
func printDoctorUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops doctor")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "检查配置、OpenAPI 可访问性和 Token 验证；仅发送 GET 请求。")
}
func printProjectsUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops projects list [--page N] [--per-page N]")
	fmt.Fprintln(out, "  vikunja-ops projects get <id>  (id 为非负整数)")
	fmt.Fprintln(out, "  vikunja-ops projects create --title TEXT [--apply]")
	fmt.Fprintln(out, "  vikunja-ops projects update <positive-id> --title TEXT [--apply]")
	fmt.Fprintln(out, "  vikunja-ops projects delete <positive-id> [--apply --confirm sha256:<64lowerhex>]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "读取 Vikunja 项目；create、update 与 delete 均需显式 --apply。projects delete 使用严格 v2 可见范围扫描和确认 token；默认项目身份会在删除安全扫描中检查。")
}
func printProjectsDeleteUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops projects delete <positive-id> [--apply --confirm sha256:<64lowerhex>]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "仅支持一个项目，可为任意层级。删除范围仅为目标项目及其递归后代，不包括祖先或兄弟项目。预览执行受限的 v2 安全扫描，输出调用者可见范围及确认 token；--apply 必须使用同一快照的 token。删除最多一次，且仅接受 204，随后恰好一次 GET 回读必须为 404；不重试、非原子，服务端可能有不可见级联。")
}
func printProjectsListUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops projects list [--page N] [--per-page N]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "page 必须大于等于 1；per-page 必须在 1 到 1000 之间。")
}
func printProjectsGetUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops projects get <id>  (id 为非负整数)")
}
func printProjectsCreateUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops projects create --title TEXT [--apply]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "仅创建根项目。title 必填，不能全为空白，最多 250 个 Unicode 字符。默认输出预览且不发送请求；--apply 只发送一次创建请求和一次 GET 回读，不重试。")
}
func printProjectsUpdateUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops projects update <positive-id> --title TEXT [--apply]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "仅更新根项目标题。title 必填，去除首尾空白后为 1 至 250 个 Unicode 字符。默认执行一次严格预检 GET；--apply 在标题不同才执行一次 PATCH 和一次回读。不重试；最后写入生效且没有并发前置条件。API 未暴露默认项目身份。")
}
func printTasksUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops tasks list [--project N] [--page N] [--per-page N] [--query TEXT] [--filter EXPR] [--filter-timezone TZ] [--include-nulls] [--sort-by FIELD]... [--order-by asc|desc]...")
	fmt.Fprintln(out, "  vikunja-ops tasks get <id>  (id 为正整数)")
	fmt.Fprintln(out, "  vikunja-ops tasks relations <task-id>")
	fmt.Fprintln(out, "  vikunja-ops tasks relations create --task-id N --other-task-id N --relation-kind KIND [--apply --confirm sha256:<64lowerhex>]")
	fmt.Fprintln(out, "  vikunja-ops tasks relations delete --task-id N --other-task-id N --relation-kind KIND [--apply --confirm sha256:<64lowerhex>]")
	fmt.Fprintln(out, "  vikunja-ops tasks labels <task-id> [--page N] [--per-page N] [--q TEXT]")
	fmt.Fprintln(out, "  vikunja-ops tasks comments <task-id> [--page N] [--per-page N] [--q TEXT] [--order-by asc|desc]")
	fmt.Fprintln(out, "  vikunja-ops tasks attachments <task-id> [--page N] [--per-page N]")
	fmt.Fprintln(out, "  vikunja-ops tasks attachments download --task-id N --attachment-id N --output PATH [--apply]")
	fmt.Fprintln(out, "  vikunja-ops tasks attachments upload --task-id N --file PATH [--apply --confirm sha256:<64 lowercase hex>]")
	fmt.Fprintln(out, "  vikunja-ops tasks attachments delete --task-id N --attachment-id N [--apply --confirm TOKEN]")
	fmt.Fprintln(out, "  vikunja-ops tasks create <project-id> --title TEXT [--description TEXT] [--priority N] [--due-date RFC3339] [--apply]")
	fmt.Fprintln(out, "  vikunja-ops tasks update <id> [--title TEXT] [--description TEXT] [--priority N] [--due-date RFC3339] [--apply]")
	fmt.Fprintln(out, "  vikunja-ops tasks complete <id> [--apply]")
	fmt.Fprintln(out, "  vikunja-ops tasks bulk-update --ids 12,34,56 [--title TEXT] [--description TEXT] [--priority N] [--due-date RFC3339] [--apply --confirm TOKEN]")
	fmt.Fprintln(out, "  vikunja-ops tasks delete --id N --project-id N [--apply --confirm TOKEN]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "list、get、relations 读取、labels、comments 与 attachments 列表为只读命令，仅发送 GET 请求；relations create/delete 预览执行一次源任务 GET，apply 执行一次预检 GET、一次受确认保护的写入和一次源任务回读（需 confirmation、不重试）。")
	fmt.Fprintln(out, "create、update 和 complete 默认输出 JSON 预览且不写入；仅 --apply 会执行写入。")
	fmt.Fprintln(out, "bulk-update 与 delete 默认输出预览；--apply 与 --confirm 必须同时提供才执行写入。bulk-update 不支持 DELETE。")
}
func printTasksCreateUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops tasks create <project-id> --title TEXT [--description TEXT] [--priority N] [--due-date RFC3339] [--apply]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "默认仅输出 JSON 预览，不发送写请求；--apply 才创建任务。project-id 为正整数，title 必填。")
}
func printTasksUpdateUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops tasks update <id> [--title TEXT] [--description TEXT] [--priority N] [--due-date RFC3339] [--apply]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "至少指定一个可变字段。默认读取当前任务并输出 JSON 预览；--apply 才更新任务。")
}
func printTasksCompleteUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops tasks complete <id> [--apply]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "默认读取当前任务并输出 JSON 预览；--apply 才将任务标为完成。")
}
func printTasksBulkUpdateUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops tasks bulk-update --ids 12,34,56 [--title TEXT] [--description TEXT] [--priority N] [--due-date RFC3339] [--apply --confirm TOKEN]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "--ids 必填：逗号分隔的正整数 ID（最多 100 个），仅支持显式 ID，不支持位置参数、filter、all、ids-file 或 stdin。")
	fmt.Fprintln(out, "至少指定一个变更字段。默认读取所有目标任务并输出 JSON 预览（含 confirmation_token），不发送写请求。")
	fmt.Fprintln(out, "--apply 与 --confirm TOKEN 必须同时提供，且 TOKEN 必须与本次规范化计划算出的预览 token 完全一致，才会执行写入。")
	fmt.Fprintln(out, "写入为单一服务端批量请求（PUT /tasks/bulk）：服务端权限或校验失败时整批不执行，客户端无逐项部分成功或回滚，不重试。")
	fmt.Fprintln(out, "confirmation token 仅绑定已批准的规范化操作意图（目标 ID + 变更字段），不是服务端状态快照。")
	fmt.Fprintln(out, "更新端点不支持条件写，预览与 apply 不是原子操作；apply 前的重新读取仅验证目标当前可读且返回 ID 与请求一致，不防止并发修改。本命令不支持 DELETE。")
}
func printTasksDeleteUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops tasks delete --id N --project-id N [--apply --confirm TOKEN]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "--id 与 --project-id 均为必填的正整数；仅支持单个显式 ID，不支持位置参数、批量 ID、filter、--all、stdin 或文件输入。")
	fmt.Fprintln(out, "默认读取目标任务并输出 JSON 预览（含 confirmation_token 与 maximum_affected: 1），仅发送 GET，不发送 DELETE。")
	fmt.Fprintln(out, "--apply 与 --confirm TOKEN 必须同时提供，且 TOKEN 必须为 sha256:<64 位小写十六进制> 并与本次预览 token 完全一致，才会执行删除。")
	fmt.Fprintln(out, "删除为单个 bodyless DELETE /api/v2/tasks/{id}，仅接受 HTTP 204；最多影响 1 个任务，无自动重试、无并发、无回滚。")
	fmt.Fprintln(out, "confirmation token 仅绑定已批准的规范化操作意图（operation + 任务 ID + 项目 ID），不是服务端状态快照。")
	fmt.Fprintln(out, "删除端点不支持条件写，预览与 apply 不是原子操作；apply 前的预检仅验证目标当前可读且返回的任务 ID 与项目 ID 与请求一致。")
	fmt.Fprintln(out, "204 后执行一次 GET 回读：404 仅表示当前任务不可读（不据此推断由本命令删除），200 报告删除结果与预期不符，其他回读失败报告删除已获服务端确认但回读未验证。")
}
func printLabelsUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops labels list [--page N] [--per-page N] [--q TEXT] [--format html|markdown]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "读取 Vikunja 标签；仅发送 GET 请求。")
}
func printLabelsListUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops labels list [--page N] [--per-page N] [--q TEXT] [--format html|markdown]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "page 必须大于等于 1；per-page 必须在 1 到 1000 之间。")
	fmt.Fprintln(out, "format 仅可为 html 或 markdown；省略时不发送该参数。")
}
func printTaskLabelsUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops tasks labels <task-id> [--page N] [--per-page N] [--q TEXT]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "task-id 必须是正整数；page 必须大于等于 1；per-page 必须在 1 到 1000 之间。")
}
func printTaskCommentsUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops tasks comments <task-id> [--page N] [--per-page N] [--q TEXT] [--order-by asc|desc]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "task-id 必须是正整数；page 必须大于等于 1；per-page 必须在 1 到 1000 之间；order-by 仅可为 asc 或 desc。")
}
func printTaskAttachmentsUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops tasks attachments <task-id> [--page N] [--per-page N]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "task-id 必须是正整数；page 必须大于等于 1；per-page 必须在 1 到 1000 之间。")
}
func printTaskAttachmentDownloadUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops tasks attachments download --task-id N --attachment-id N --output PATH [--apply]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "--task-id、--attachment-id 为必填正整数；--output 必填；不支持位置参数、--confirm、批量 ID、--all、文件或 stdin 输入。")
	fmt.Fprintln(out, "默认仅预览：恰好一次附件元数据 GET，不下载字节、不创建本地文件。")
	fmt.Fprintln(out, "--apply 恰好执行一次元数据 GET 和一次字节 GET（仅接受 HTTP 200）；单文件硬上限 100 MiB，传输超时 10 分钟，拒绝重定向，不重试。")
	fmt.Fprintln(out, "输出目标必须不存在，父目录必须已存在且不是符号链接；不自动创建目录；服务端文件名与 Content-Disposition 不影响目标路径。")
}
func printTaskAttachmentUploadUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops tasks attachments upload --task-id N --file PATH [--apply --confirm sha256:<64 lowercase hex>]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "仅支持一个本地普通文件（0 至 100 MiB），不支持位置参数、重复 --file、stdin 或批量输入。")
	fmt.Fprintln(out, "默认预览会计算文件指纹并执行一次任务 GET。上传是远程写入：--apply 必须使用预览的 confirmation token。")
}
func printTaskAttachmentDeleteUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops tasks attachments delete --task-id N --attachment-id N [--apply --confirm TOKEN]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "仅支持一个明确任务和附件 ID，不支持位置参数、批量、filter 或 stdin 输入。预览执行完整附件元数据扫描（分页时可有多个 GET），不发送 DELETE。")
	fmt.Fprintln(out, "--apply 与预览的 --confirm TOKEN 必须同时提供。删除为单个无请求体 DELETE，仅接受 HTTP 204；之后执行完整分页元数据回读；不重试、无并发、无回滚。")
}
func printTasksListUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops tasks list [--project N] [--page N] [--per-page N] [--query TEXT] [--filter EXPR] [--filter-timezone TZ] [--include-nulls] [--sort-by FIELD]... [--order-by asc|desc]...")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "project 为可选的正整数；page 必须大于等于 1；per-page 必须在 1 到 1000 之间。")
	fmt.Fprintln(out, "sort-by 与 order-by 的数量必须相同，且各项不能为空。")
	fmt.Fprintln(out, "仅发送 GET 请求。")
}
func printTasksGetUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops tasks get <id>  (id 为正整数)")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "读取 Vikunja 任务详情；仅发送 GET 请求。")
}
func printTaskRelationsUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops tasks relations <task-id>")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "读取任务详情中的 related_tasks；task-id 必须是正整数；仅发送 GET 请求。")
}

func printTaskRelationsCreateUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops tasks relations create --task-id N --other-task-id N --relation-kind KIND [--apply --confirm sha256:<64lowerhex>]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "ID 必须为不同的正整数。KIND 仅可为 subtask、parenttask、related、duplicateof、duplicates、blocking、blocked、precedes、follows、copiedfrom、copiedto。预览仅一次源任务 GET；apply 需要 confirmation，执行一次写入和一次源任务回读，不重试。")
}

func printTaskRelationsDeleteUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops tasks relations delete --task-id N --other-task-id N --relation-kind KIND [--apply --confirm sha256:<64lowerhex>]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "ID 必须为不同的正整数。KIND 仅可为 subtask、parenttask、related、duplicateof、duplicates、blocking、blocked、precedes、follows、copiedfrom、copiedto。预览仅一次源任务 GET；apply 需要 confirmation，执行一次写入和一次源任务回读，不重试。")
}
