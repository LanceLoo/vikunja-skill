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
	fmt.Fprintln(out, "  vikunja-ops doctor")
	fmt.Fprintln(out, "  vikunja-ops projects --help")
	fmt.Fprintln(out, "  vikunja-ops labels --help")
	fmt.Fprintln(out, "  vikunja-ops tasks --help")
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
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "读取 Vikunja 项目；仅发送 GET 请求。")
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
func printTasksUsage(out io.Writer) {
	fmt.Fprintln(out, "用法:")
	fmt.Fprintln(out, "  vikunja-ops tasks list [--project N] [--page N] [--per-page N] [--query TEXT] [--filter EXPR] [--filter-timezone TZ] [--include-nulls] [--sort-by FIELD]... [--order-by asc|desc]...")
	fmt.Fprintln(out, "  vikunja-ops tasks get <id>  (id 为正整数)")
	fmt.Fprintln(out, "  vikunja-ops tasks labels <task-id> [--page N] [--per-page N] [--q TEXT]")
	fmt.Fprintln(out, "  vikunja-ops tasks comments <task-id> [--page N] [--per-page N] [--q TEXT] [--order-by asc|desc]")
	fmt.Fprintln(out, "  vikunja-ops tasks attachments <task-id> [--page N] [--per-page N]")
	fmt.Fprintln(out, "  vikunja-ops tasks create <project-id> --title TEXT [--description TEXT] [--priority N] [--due-date RFC3339] [--apply]")
	fmt.Fprintln(out, "  vikunja-ops tasks update <id> [--title TEXT] [--description TEXT] [--priority N] [--due-date RFC3339] [--apply]")
	fmt.Fprintln(out, "  vikunja-ops tasks complete <id> [--apply]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "list、get、labels、comments 和 attachments 为只读命令，仅发送 GET 请求。")
	fmt.Fprintln(out, "create、update 和 complete 默认输出 JSON 预览且不写入；仅 --apply 会执行写入。")
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
