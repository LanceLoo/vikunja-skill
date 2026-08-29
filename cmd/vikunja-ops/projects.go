package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"vikunja-opencode-skill/internal/client"
	"vikunja-opencode-skill/internal/config"
)

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
	case "create":
		return runProjectsCreate(args[1:], stdout, stderr)
	case "update":
		return runProjectsUpdate(args[1:], stdout, stderr)
	default:
		printProjectsUsage(stderr)
		return 2
	}
}

func runProjectsCreate(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--help" {
		printProjectsCreateUsage(stdout)
		return 0
	}
	if projectCreateDuplicateFlags(args) {
		printProjectsCreateUsage(stderr)
		return 2
	}
	flags := flag.NewFlagSet("projects create", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	title := flags.String("title", "", "项目标题")
	apply := flags.Bool("apply", false, "执行创建；默认仅预览")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			printProjectsCreateUsage(stdout)
			return 0
		}
		printProjectsCreateUsage(stderr)
		return 2
	}
	titleSet := false
	flags.Visit(func(f *flag.Flag) { titleSet = titleSet || f.Name == "title" })
	normalizedTitle := strings.TrimSpace(*title)
	if flags.NArg() != 0 || !titleSet || normalizedTitle == "" || utf8.RuneCountInString(normalizedTitle) > 250 {
		printProjectsCreateUsage(stderr)
		return 2
	}
	if !*apply {
		return writeJSON("projects create", map[string]any{
			"mode":             "preview",
			"operation":        "create",
			"title":            normalizedTitle,
			"maximum_affected": 1,
			"next_args":        []string{"projects", "create", "--title=" + normalizedTitle, "--apply"},
		}, stdout, stderr)
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "projects create: %v\n", err)
		return 1
	}
	api := client.New(nil)
	created, err := api.CreateProject(context.Background(), cfg.BaseURL, cfg.Token, normalizedTitle)
	if err != nil {
		return printProjectCreateError(err, stderr)
	}
	if created.ID < 1 {
		return printProjectCreateNoSafeID(stderr)
	}
	readback, readbackErr := api.GetProject(cfg.BaseURL, cfg.Token, created.ID)
	if readbackErr != nil {
		fmt.Fprintf(stderr, "projects create: 创建请求已返回项目 ID %d，但回读未完成（%s）\n", created.ID, describeProjectCreateReadbackError(readbackErr))
		return 1
	}
	if readback.ID != created.ID || readback.Title != normalizedTitle {
		fmt.Fprintf(stderr, "projects create: 创建请求已返回项目 ID %d，但当前回读项目与请求不一致\n", created.ID)
		return 1
	}
	return writeJSON("projects create", map[string]any{
		"mode":             "apply",
		"operation":        "create",
		"title":            normalizedTitle,
		"maximum_affected": 1,
		"project_id":       created.ID,
		"create_status":    "returned_project",
		"readback_status":  "id_and_title_matched",
		"readback":         readback,
	}, stdout, stderr)
}

func projectCreateDuplicateFlags(args []string) bool {
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name := strings.SplitN(arg, "=", 2)[0]
		name = strings.TrimPrefix(strings.TrimPrefix(name, "--"), "-")
		switch name {
		case "title":
			if seen[name] {
				return true
			}
			seen[name] = true
			if !strings.Contains(arg, "=") && i+1 < len(args) {
				i++
			}
		case "apply":
			if seen[name] {
				return true
			}
			seen[name] = true
		}
	}
	return false
}

func printProjectCreateError(err error, stderr io.Writer) int {
	return printProjectCreateNoSafeID(stderr)
}

func printProjectCreateNoSafeID(stderr io.Writer) int {
	fmt.Fprintln(stderr, "projects create: 未获得可安全回读的项目 ID；创建结果可能未知，请勿盲目重试")
	return 1
}

func describeProjectCreateReadbackError(err error) string {
	switch {
	case errors.Is(err, client.ErrUnauthorized):
		return "回读认证失败"
	case errors.Is(err, client.ErrForbidden):
		return "回读被拒绝"
	case errors.Is(err, client.ErrNotFound):
		return "回读返回 404"
	default:
		return "回读网络或服务端错误"
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
