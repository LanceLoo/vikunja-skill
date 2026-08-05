package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"vikunja-opencode-skill/internal/client"
	"vikunja-opencode-skill/internal/config"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("vikunja-ops", flag.ContinueOnError)
	flags.SetOutput(stdout)
	flags.Usage = func() {
		printUsage(flags.Output())
	}

	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	remaining := flags.Args()
	if len(remaining) == 0 {
		printUsage(stdout)
		return 0
	}
	if remaining[0] == "projects" {
		return runProjects(remaining[1:], stdout, stderr)
	}
	if remaining[0] == "labels" {
		return runLabels(remaining[1:], stdout, stderr)
	}
	if remaining[0] == "tasks" {
		return runTasks(remaining[1:], stdout, stderr)
	}
	if remaining[0] != "doctor" {
		printUsage(stderr)
		return 2
	}
	if len(remaining) > 1 {
		if len(remaining) == 2 && remaining[1] == "--help" {
			printDoctorUsage(stdout)
			return 0
		}
		printDoctorUsage(stderr)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "doctor: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "配置有效")
	c := client.New(nil)
	if err := c.CheckOpenAPI(cfg.BaseURL); err != nil {
		fmt.Fprintf(stderr, "doctor: OpenAPI 不可访问: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "OpenAPI 可访问")
	if err := c.VerifyTokenV1(cfg.BaseURL, cfg.Token); err != nil {
		switch {
		case errors.Is(err, client.ErrUnauthorized):
			fmt.Fprintln(stderr, "doctor: Token 验证失败：服务未接受 Token")
		case errors.Is(err, client.ErrForbidden):
			fmt.Fprintln(stderr, "doctor: Token 验证失败：Token 已被识别，但被实例或权限策略拒绝")
		default:
			fmt.Fprintf(stderr, "doctor: Token 验证失败: %v\n", err)
		}
		return 1
	}
	fmt.Fprintln(stdout, "Token 验证通过")
	if err := c.CheckProjectsRead(cfg.BaseURL, cfg.Token); err != nil {
		switch {
		case errors.Is(err, client.ErrUnauthorized):
			fmt.Fprintln(stderr, "doctor: v2 项目读取失败：Token 本身有效，但缺少或未匹配 v2 projects 读取权限，或服务存在兼容性问题")
		case errors.Is(err, client.ErrForbidden):
			fmt.Fprintln(stderr, "doctor: v2 项目读取失败：Token 已被识别，但项目读取被实例或权限策略拒绝")
		default:
			fmt.Fprintf(stderr, "doctor: v2 项目读取失败: %v\n", err)
		}
		return 1
	}
	fmt.Fprintln(stdout, "v2 项目读取权限可用")
	return 0
}

func runProjects(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printProjectsUsage(stderr)
		return 2
	}
	if len(args) == 1 && args[0] == "--help" {
		printProjectsUsage(stdout)
		return 0
	}

	switch args[0] {
	case "list":
		return runProjectsList(args[1:], stdout, stderr)
	case "get":
		return runProjectsGet(args[1:], stdout, stderr)
	default:
		printProjectsUsage(stderr)
		return 2
	}
}

func runProjectsList(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("projects list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	page, perPage := bindPaginationFlags(flags)
	flags.Usage = func() { printProjectsListUsage(flags.Output()) }
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			printProjectsListUsage(stdout)
			return 0
		}
		fmt.Fprintln(stderr, err)
		printProjectsListUsage(stderr)
		return 2
	}
	if flags.NArg() != 0 || !validPagination(*page, *perPage) {
		printProjectsListUsage(stderr)
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "projects list: %v\n", err)
		return 1
	}
	projects, err := client.New(nil).ListProjects(cfg.BaseURL, cfg.Token, *page, *perPage)
	if err != nil {
		return printProjectsError("projects list", err, stderr)
	}
	return writeJSON("projects list", projects, stdout, stderr)
}

func runProjectsGet(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--help" {
		printProjectsGetUsage(stdout)
		return 0
	}
	if len(args) != 1 {
		printProjectsGetUsage(stderr)
		return 2
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id < 0 {
		fmt.Fprintln(stderr, "projects get: id 必须是非负整数")
		printProjectsGetUsage(stderr)
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "projects get: %v\n", err)
		return 1
	}
	project, err := client.New(nil).GetProject(cfg.BaseURL, cfg.Token, id)
	if err != nil {
		return printProjectsError("projects get", err, stderr)
	}
	return writeJSON("projects get", project, stdout, stderr)
}

func printProjectsError(command string, err error, stderr io.Writer) int {
	return printResourceError(command, err, "项目读取被拒绝", "项目不存在", stderr)
}

func bindPaginationFlags(flags *flag.FlagSet) (page, perPage *int) {
	return flags.Int("page", 1, "页码（默认 1）"), flags.Int("per-page", 50, "每页条目数（默认 50）")
}

func validPagination(page, perPage int) bool {
	return page >= 1 && perPage >= 1 && perPage <= 1000
}

func writeJSON(command string, value any, stdout, stderr io.Writer) int {
	if err := json.NewEncoder(stdout).Encode(value); err != nil {
		fmt.Fprintf(stderr, "%s: 无法输出 JSON: %v\n", command, err)
		return 1
	}
	return 0
}

func printResourceError(command string, err error, forbiddenMessage, notFoundMessage string, stderr io.Writer) int {
	switch {
	case errors.Is(err, client.ErrUnauthorized):
		fmt.Fprintf(stderr, "%s: 认证失败：服务未接受 Token\n", command)
	case errors.Is(err, client.ErrForbidden):
		fmt.Fprintf(stderr, "%s: 权限不足：%s\n", command, forbiddenMessage)
	case errors.Is(err, client.ErrNotFound):
		fmt.Fprintf(stderr, "%s: %s\n", command, notFoundMessage)
	default:
		fmt.Fprintf(stderr, "%s: %v\n", command, err)
	}
	return 1
}

type stringSlice []string

func (values *stringSlice) String() string {
	return ""
}

func (values *stringSlice) Set(value string) error {
	if value == "" {
		return errors.New("值不能为空")
	}
	*values = append(*values, value)
	return nil
}

func runTasks(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printTasksUsage(stderr)
		return 2
	}
	if len(args) == 1 && args[0] == "--help" {
		printTasksUsage(stdout)
		return 0
	}

	switch args[0] {
	case "list":
		return runTasksList(args[1:], stdout, stderr)
	case "get":
		return runTasksGet(args[1:], stdout, stderr)
	case "labels":
		return runTaskLabels(args[1:], stdout, stderr)
	case "comments":
		return runTaskComments(args[1:], stdout, stderr)
	case "attachments":
		return runTaskAttachments(args[1:], stdout, stderr)
	default:
		printTasksUsage(stderr)
		return 2
	}
}

func runTasksList(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("tasks list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	projectID := flags.Int64("project", 0, "项目 ID")
	page, perPage := bindPaginationFlags(flags)
	query := flags.String("query", "", "搜索文本")
	filter := flags.String("filter", "", "筛选表达式")
	filterTimezone := flags.String("filter-timezone", "", "筛选时区")
	includeNulls := flags.Bool("include-nulls", false, "筛选中包含空值")
	var sortBy, orderBy stringSlice
	flags.Var(&sortBy, "sort-by", "排序字段（可重复）")
	flags.Var(&orderBy, "order-by", "排序方向（可重复）")
	flags.Usage = func() { printTasksListUsage(flags.Output()) }
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			printTasksListUsage(stdout)
			return 0
		}
		fmt.Fprintln(stderr, err)
		printTasksListUsage(stderr)
		return 2
	}
	projectSpecified := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "project" {
			projectSpecified = true
		}
	})
	if flags.NArg() != 0 || (projectSpecified && *projectID < 1) || !validPagination(*page, *perPage) || len(sortBy) != len(orderBy) || !validTaskSortOptions(sortBy, orderBy) {
		printTasksListUsage(stderr)
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "tasks list: %v\n", err)
		return 1
	}
	tasks, err := client.New(nil).ListTasks(cfg.BaseURL, cfg.Token, client.TaskListOptions{
		Page:               *page,
		PerPage:            *perPage,
		ProjectID:          *projectID,
		Query:              *query,
		Filter:             *filter,
		FilterTimezone:     *filterTimezone,
		FilterIncludeNulls: *includeNulls,
		SortBy:             sortBy,
		OrderBy:            orderBy,
	})
	if err != nil {
		return printTasksError("tasks list", err, stderr)
	}
	return writeJSON("tasks list", tasks, stdout, stderr)
}

func validTaskSortOptions(sortBy, orderBy []string) bool {
	for i := range sortBy {
		if strings.TrimSpace(sortBy[i]) == "" || strings.TrimSpace(orderBy[i]) == "" {
			return false
		}
		if orderBy[i] != "asc" && orderBy[i] != "desc" {
			return false
		}
	}
	return true
}

func runTasksGet(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--help" {
		printTasksGetUsage(stdout)
		return 0
	}
	if len(args) != 1 {
		printTasksGetUsage(stderr)
		return 2
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id < 1 {
		fmt.Fprintln(stderr, "tasks get: id 必须是正整数")
		printTasksGetUsage(stderr)
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "tasks get: %v\n", err)
		return 1
	}
	task, err := client.New(nil).GetTask(cfg.BaseURL, cfg.Token, id)
	if err != nil {
		return printTasksError("tasks get", err, stderr)
	}
	return writeJSON("tasks get", task, stdout, stderr)
}

func printTasksError(command string, err error, stderr io.Writer) int {
	return printResourceError(command, err, "任务读取被拒绝", "任务或项目不存在", stderr)
}

func runLabels(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--help" {
		printLabelsUsage(stdout)
		return 0
	}
	if len(args) == 0 || args[0] != "list" {
		printLabelsUsage(stderr)
		return 2
	}
	return runLabelsList(args[1:], stdout, stderr)
}

func runLabelsList(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("labels list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	page, perPage := bindPaginationFlags(flags)
	query := flags.String("q", "", "搜索文本")
	format := flags.String("format", "", "描述格式（html 或 markdown）")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			printLabelsListUsage(stdout)
			return 0
		}
		fmt.Fprintln(stderr, err)
		printLabelsListUsage(stderr)
		return 2
	}
	if flags.NArg() != 0 || !validPagination(*page, *perPage) || !validLabelFormat(*format) {
		printLabelsListUsage(stderr)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "labels list: %v\n", err)
		return 1
	}
	labels, err := client.New(nil).ListLabels(cfg.BaseURL, cfg.Token, *page, *perPage, *query, *format)
	if err != nil {
		return printLabelsError("labels list", err, stderr)
	}
	return writeJSON("labels list", labels, stdout, stderr)
}

func runTaskLabels(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--help" {
		printTaskLabelsUsage(stdout)
		return 0
	}
	if len(args) == 0 {
		printTaskLabelsUsage(stderr)
		return 2
	}
	taskID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || taskID < 1 {
		fmt.Fprintln(stderr, "tasks labels: task-id 必须是正整数")
		printTaskLabelsUsage(stderr)
		return 2
	}
	flags := flag.NewFlagSet("tasks labels", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	page, perPage := bindPaginationFlags(flags)
	query := flags.String("q", "", "搜索文本")
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			printTaskLabelsUsage(stdout)
			return 0
		}
		fmt.Fprintln(stderr, err)
		printTaskLabelsUsage(stderr)
		return 2
	}
	if flags.NArg() != 0 || !validPagination(*page, *perPage) {
		printTaskLabelsUsage(stderr)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "tasks labels: %v\n", err)
		return 1
	}
	labels, err := client.New(nil).ListTaskLabels(cfg.BaseURL, cfg.Token, taskID, *page, *perPage, *query)
	if err != nil {
		return printLabelsError("tasks labels", err, stderr)
	}
	return writeJSON("tasks labels", labels, stdout, stderr)
}

func runTaskComments(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--help" {
		printTaskCommentsUsage(stdout)
		return 0
	}
	if len(args) == 0 {
		printTaskCommentsUsage(stderr)
		return 2
	}
	taskID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || taskID < 1 {
		fmt.Fprintln(stderr, "tasks comments: task-id 必须是正整数")
		printTaskCommentsUsage(stderr)
		return 2
	}
	flags := flag.NewFlagSet("tasks comments", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	page, perPage := bindPaginationFlags(flags)
	query := flags.String("q", "", "搜索文本")
	orderBy := flags.String("order-by", "", "排序方向（asc 或 desc）")
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			printTaskCommentsUsage(stdout)
			return 0
		}
		fmt.Fprintln(stderr, err)
		printTaskCommentsUsage(stderr)
		return 2
	}
	if flags.NArg() != 0 || !validPagination(*page, *perPage) || !validOrderBy(*orderBy) {
		printTaskCommentsUsage(stderr)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "tasks comments: %v\n", err)
		return 1
	}
	comments, err := client.New(nil).ListTaskComments(cfg.BaseURL, cfg.Token, taskID, *page, *perPage, *query, *orderBy)
	if err != nil {
		return printCommentsError("tasks comments", err, stderr)
	}
	return writeJSON("tasks comments", comments, stdout, stderr)
}

func runTaskAttachments(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--help" {
		printTaskAttachmentsUsage(stdout)
		return 0
	}
	if len(args) == 0 {
		printTaskAttachmentsUsage(stderr)
		return 2
	}
	taskID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || taskID < 1 {
		fmt.Fprintln(stderr, "tasks attachments: task-id 必须是正整数")
		printTaskAttachmentsUsage(stderr)
		return 2
	}
	flags := flag.NewFlagSet("tasks attachments", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	page, perPage := bindPaginationFlags(flags)
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			printTaskAttachmentsUsage(stdout)
			return 0
		}
		fmt.Fprintln(stderr, err)
		printTaskAttachmentsUsage(stderr)
		return 2
	}
	if flags.NArg() != 0 || !validPagination(*page, *perPage) {
		printTaskAttachmentsUsage(stderr)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "tasks attachments: %v\n", err)
		return 1
	}
	attachments, err := client.New(nil).ListTaskAttachments(cfg.BaseURL, cfg.Token, taskID, *page, *perPage)
	if err != nil {
		return printAttachmentsError("tasks attachments", err, stderr)
	}
	return writeJSON("tasks attachments", attachments, stdout, stderr)
}

func printLabelsError(command string, err error, stderr io.Writer) int {
	return printResourceError(command, err, "标签读取被拒绝", "标签或任务不存在", stderr)
}

func printCommentsError(command string, err error, stderr io.Writer) int {
	return printResourceError(command, err, "评论读取被拒绝", "任务或评论不存在", stderr)
}

func printAttachmentsError(command string, err error, stderr io.Writer) int {
	return printResourceError(command, err, "附件读取被拒绝", "任务或附件不存在", stderr)
}

func validLabelFormat(format string) bool {
	return format == "" || format == "html" || format == "markdown"
}

func validOrderBy(orderBy string) bool {
	return orderBy == "" || orderBy == "asc" || orderBy == "desc"
}

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
