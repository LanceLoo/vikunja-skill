package main

import (
	"fmt"
	"io"
)

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "vikunja-ops: Vikunja CLI")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "提供只读的 Vikunja 诊断与资源查询。")
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
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "读取 Vikunja 任务；仅发送 GET 请求。")
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
