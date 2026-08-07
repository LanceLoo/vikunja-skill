package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"vikunja-opencode-skill/internal/client"
	"vikunja-opencode-skill/internal/config"
)

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
	case "create":
		return runTasksCreate(args[1:], stdout, stderr)
	case "update":
		return runTasksUpdate(args[1:], stdout, stderr)
	case "complete":
		return runTasksComplete(args[1:], stdout, stderr)
	case "labels":
		return runTaskLabels(args[1:], stdout, stderr)
	case "comments":
		return runTaskComments(args[1:], stdout, stderr)
	case "attachments":
		return runTaskAttachments(args[1:], stdout, stderr)
	case "bulk-update":
		return runTasksBulkUpdate(args[1:], stdout, stderr)
	default:
		printTasksUsage(stderr)
		return 2
	}
}

// taskChanges contains only fields explicitly selected at the command line.
// It is used for preview output and is deliberately separate from the client
// DTO so an omitted flag can never accidentally become a zero-value update.
type taskChanges struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Priority    *int    `json:"priority,omitempty"`
	DueDate     *string `json:"due_date,omitempty"`
}

func bindTaskChangeFlags(flags *flag.FlagSet) (*string, *string, *int, *string) {
	title := flags.String("title", "", "任务标题")
	description := flags.String("description", "", "任务描述")
	priority := flags.Int("priority", 0, "优先级")
	dueDate := flags.String("due-date", "", "截止时间（RFC3339）")
	return title, description, priority, dueDate
}

func taskChangeFlags(flags *flag.FlagSet, title, description *string, priority *int, dueDate *string) (taskChanges, bool) {
	var changes taskChanges
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "title":
			changes.Title = title
		case "description":
			changes.Description = description
		case "priority":
			changes.Priority = priority
		case "due-date":
			changes.DueDate = dueDate
		}
	})
	return changes, changes.Title != nil || changes.Description != nil || changes.Priority != nil || changes.DueDate != nil
}

func validTaskChanges(changes taskChanges, requireTitle bool) bool {
	if (requireTitle && changes.Title == nil) || (changes.Title != nil && strings.TrimSpace(*changes.Title) == "") {
		return false
	}
	if changes.Priority != nil && *changes.Priority < 0 {
		return false
	}
	if changes.DueDate != nil {
		if _, err := time.Parse(time.RFC3339, *changes.DueDate); err != nil {
			return false
		}
	}
	return true
}

func parseTaskID(command string, args []string, usage func(io.Writer), stderr io.Writer) (int64, bool) {
	if len(args) == 0 {
		usage(stderr)
		return 0, false
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id < 1 {
		fmt.Fprintf(stderr, "%s: id 必须是正整数\n", command)
		usage(stderr)
		return 0, false
	}
	return id, true
}

func runTasksCreate(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--help" {
		printTasksCreateUsage(stdout)
		return 0
	}
	projectID, ok := parseTaskID("tasks create", args, printTasksCreateUsage, stderr)
	if !ok {
		return 2
	}
	flags := flag.NewFlagSet("tasks create", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	title, description, priority, dueDate := bindTaskChangeFlags(flags)
	apply := flags.Bool("apply", false, "执行写入；默认仅预览")
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			printTasksCreateUsage(stdout)
			return 0
		}
		fmt.Fprintln(stderr, err)
		printTasksCreateUsage(stderr)
		return 2
	}
	changes, _ := taskChangeFlags(flags, title, description, priority, dueDate)
	if flags.NArg() != 0 || !validTaskChanges(changes, true) {
		printTasksCreateUsage(stderr)
		return 2
	}
	if !*apply {
		return writeJSON("tasks create", map[string]any{"mode": "preview", "operation": "create", "project_id": projectID, "changes": changes}, stdout, stderr)
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "tasks create: %v\n", err)
		return 1
	}
	task, err := client.New(nil).CreateTask(context.Background(), cfg.BaseURL, cfg.Token, projectID, client.CreateTaskInput{Title: *changes.Title, Description: changes.Description, Priority: changes.Priority, DueDate: changes.DueDate})
	if err != nil {
		return printTaskWriteError("tasks create", err, stderr)
	}
	return writeJSON("tasks create", task, stdout, stderr)
}

func runTasksUpdate(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--help" {
		printTasksUpdateUsage(stdout)
		return 0
	}
	id, ok := parseTaskID("tasks update", args, printTasksUpdateUsage, stderr)
	if !ok {
		return 2
	}
	flags := flag.NewFlagSet("tasks update", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	title, description, priority, dueDate := bindTaskChangeFlags(flags)
	apply := flags.Bool("apply", false, "执行写入；默认仅预览")
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			printTasksUpdateUsage(stdout)
			return 0
		}
		fmt.Fprintln(stderr, err)
		printTasksUpdateUsage(stderr)
		return 2
	}
	changes, specified := taskChangeFlags(flags, title, description, priority, dueDate)
	if flags.NArg() != 0 || !specified || !validTaskChanges(changes, false) {
		printTasksUpdateUsage(stderr)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "tasks update: %v\n", err)
		return 1
	}
	if !*apply {
		current, err := client.New(nil).GetTask(cfg.BaseURL, cfg.Token, id)
		if err != nil {
			return printTasksError("tasks update", err, stderr)
		}
		return writeJSON("tasks update", map[string]any{"mode": "preview", "operation": "update", "task_id": id, "current": current, "changes": changes}, stdout, stderr)
	}
	task, err := client.New(nil).UpdateTask(context.Background(), cfg.BaseURL, cfg.Token, id, client.UpdateTaskInput{Title: changes.Title, Description: changes.Description, Priority: changes.Priority, DueDate: changes.DueDate})
	if err != nil {
		return printTaskWriteError("tasks update", err, stderr)
	}
	return writeJSON("tasks update", task, stdout, stderr)
}

func runTasksComplete(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--help" {
		printTasksCompleteUsage(stdout)
		return 0
	}
	id, ok := parseTaskID("tasks complete", args, printTasksCompleteUsage, stderr)
	if !ok {
		return 2
	}
	flags := flag.NewFlagSet("tasks complete", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	apply := flags.Bool("apply", false, "执行写入；默认仅预览")
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			printTasksCompleteUsage(stdout)
			return 0
		}
		fmt.Fprintln(stderr, err)
		printTasksCompleteUsage(stderr)
		return 2
	}
	if flags.NArg() != 0 {
		printTasksCompleteUsage(stderr)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "tasks complete: %v\n", err)
		return 1
	}
	if !*apply {
		current, err := client.New(nil).GetTask(cfg.BaseURL, cfg.Token, id)
		if err != nil {
			return printTasksError("tasks complete", err, stderr)
		}
		return writeJSON("tasks complete", map[string]any{"mode": "preview", "operation": "complete", "task_id": id, "current": current, "changes": map[string]bool{"done": true}}, stdout, stderr)
	}
	task, err := client.New(nil).CompleteTask(context.Background(), cfg.BaseURL, cfg.Token, id)
	if err != nil {
		return printTaskWriteError("tasks complete", err, stderr)
	}
	return writeJSON("tasks complete", task, stdout, stderr)
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
	tasks, err := client.New(nil).ListTasks(cfg.BaseURL, cfg.Token, client.TaskListOptions{Page: *page, PerPage: *perPage, ProjectID: *projectID, Query: *query, Filter: *filter, FilterTimezone: *filterTimezone, FilterIncludeNulls: *includeNulls, SortBy: sortBy, OrderBy: orderBy})
	if err != nil {
		return printTasksError("tasks list", err, stderr)
	}
	return writeJSON("tasks list", tasks, stdout, stderr)
}
func validTaskSortOptions(sortBy, orderBy []string) bool {
	for i := range sortBy {
		if strings.TrimSpace(sortBy[i]) == "" || strings.TrimSpace(orderBy[i]) == "" || (orderBy[i] != "asc" && orderBy[i] != "desc") {
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

func printTaskWriteError(command string, err error, stderr io.Writer) int {
	return printResourceError(command, err, "任务写入被拒绝", "任务或项目不存在", stderr)
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
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id < 1 {
		fmt.Fprintln(stderr, "tasks labels: task-id 必须是正整数")
		printTaskLabelsUsage(stderr)
		return 2
	}
	f := flag.NewFlagSet("tasks labels", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	p, pp := bindPaginationFlags(f)
	q := f.String("q", "", "搜索文本")
	if err := f.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			printTaskLabelsUsage(stdout)
			return 0
		}
		fmt.Fprintln(stderr, err)
		printTaskLabelsUsage(stderr)
		return 2
	}
	if f.NArg() != 0 || !validPagination(*p, *pp) {
		printTaskLabelsUsage(stderr)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "tasks labels: %v\n", err)
		return 1
	}
	v, err := client.New(nil).ListTaskLabels(cfg.BaseURL, cfg.Token, id, *p, *pp, *q)
	if err != nil {
		return printLabelsError("tasks labels", err, stderr)
	}
	return writeJSON("tasks labels", v, stdout, stderr)
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
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id < 1 {
		fmt.Fprintln(stderr, "tasks comments: task-id 必须是正整数")
		printTaskCommentsUsage(stderr)
		return 2
	}
	f := flag.NewFlagSet("tasks comments", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	p, pp := bindPaginationFlags(f)
	q := f.String("q", "", "搜索文本")
	o := f.String("order-by", "", "排序方向（asc 或 desc）")
	if err := f.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			printTaskCommentsUsage(stdout)
			return 0
		}
		fmt.Fprintln(stderr, err)
		printTaskCommentsUsage(stderr)
		return 2
	}
	if f.NArg() != 0 || !validPagination(*p, *pp) || !validOrderBy(*o) {
		printTaskCommentsUsage(stderr)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "tasks comments: %v\n", err)
		return 1
	}
	v, err := client.New(nil).ListTaskComments(cfg.BaseURL, cfg.Token, id, *p, *pp, *q, *o)
	if err != nil {
		return printCommentsError("tasks comments", err, stderr)
	}
	return writeJSON("tasks comments", v, stdout, stderr)
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
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id < 1 {
		fmt.Fprintln(stderr, "tasks attachments: task-id 必须是正整数")
		printTaskAttachmentsUsage(stderr)
		return 2
	}
	f := flag.NewFlagSet("tasks attachments", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	p, pp := bindPaginationFlags(f)
	if err := f.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			printTaskAttachmentsUsage(stdout)
			return 0
		}
		fmt.Fprintln(stderr, err)
		printTaskAttachmentsUsage(stderr)
		return 2
	}
	if f.NArg() != 0 || !validPagination(*p, *pp) {
		printTaskAttachmentsUsage(stderr)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "tasks attachments: %v\n", err)
		return 1
	}
	v, err := client.New(nil).ListTaskAttachments(cfg.BaseURL, cfg.Token, id, *p, *pp)
	if err != nil {
		return printAttachmentsError("tasks attachments", err, stderr)
	}
	return writeJSON("tasks attachments", v, stdout, stderr)
}
func printCommentsError(command string, err error, stderr io.Writer) int {
	return printResourceError(command, err, "评论读取被拒绝", "任务或评论不存在", stderr)
}
func printAttachmentsError(command string, err error, stderr io.Writer) int {
	return printResourceError(command, err, "附件读取被拒绝", "任务或附件不存在", stderr)
}
func validOrderBy(orderBy string) bool { return orderBy == "" || orderBy == "asc" || orderBy == "desc" }
