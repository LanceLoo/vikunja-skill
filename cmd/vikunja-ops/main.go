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
	page := flags.Int("page", 1, "页码（默认 1）")
	perPage := flags.Int("per-page", 50, "每页条目数（默认 50）")
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
	if flags.NArg() != 0 || *page < 1 || *perPage < 1 || *perPage > 1000 {
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
	if err := json.NewEncoder(stdout).Encode(projects); err != nil {
		fmt.Fprintf(stderr, "projects list: 无法输出 JSON: %v\n", err)
		return 1
	}
	return 0
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
	if err := json.NewEncoder(stdout).Encode(project); err != nil {
		fmt.Fprintf(stderr, "projects get: 无法输出 JSON: %v\n", err)
		return 1
	}
	return 0
}

func printProjectsError(command string, err error, stderr io.Writer) int {
	switch {
	case errors.Is(err, client.ErrUnauthorized):
		fmt.Fprintf(stderr, "%s: 认证失败：服务未接受 Token\n", command)
	case errors.Is(err, client.ErrForbidden):
		fmt.Fprintf(stderr, "%s: 权限不足：项目读取被拒绝\n", command)
	case errors.Is(err, client.ErrNotFound):
		fmt.Fprintf(stderr, "%s: 项目不存在\n", command)
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
	default:
		printTasksUsage(stderr)
		return 2
	}
}

func runTasksList(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("tasks list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	projectID := flags.Int64("project", 0, "项目 ID")
	page := flags.Int("page", 1, "页码（默认 1）")
	perPage := flags.Int("per-page", 50, "每页条目数（默认 50）")
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
	if flags.NArg() != 0 || (projectSpecified && *projectID < 1) || *page < 1 || *perPage < 1 || *perPage > 1000 || len(sortBy) != len(orderBy) || !validTaskSortOptions(sortBy, orderBy) {
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
	if err := json.NewEncoder(stdout).Encode(tasks); err != nil {
		fmt.Fprintf(stderr, "tasks list: 无法输出 JSON: %v\n", err)
		return 1
	}
	return 0
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
	if err := json.NewEncoder(stdout).Encode(task); err != nil {
		fmt.Fprintf(stderr, "tasks get: 无法输出 JSON: %v\n", err)
		return 1
	}
	return 0
}

func printTasksError(command string, err error, stderr io.Writer) int {
	switch {
	case errors.Is(err, client.ErrUnauthorized):
		fmt.Fprintf(stderr, "%s: 认证失败：服务未接受 Token\n", command)
	case errors.Is(err, client.ErrForbidden):
		fmt.Fprintf(stderr, "%s: 权限不足：任务读取被拒绝\n", command)
	case errors.Is(err, client.ErrNotFound):
		fmt.Fprintf(stderr, "%s: 任务或项目不存在\n", command)
	default:
		fmt.Fprintf(stderr, "%s: %v\n", command, err)
	}
	return 1
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
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "读取 Vikunja 任务；仅发送 GET 请求。")
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
