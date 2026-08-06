package main

import (
	"flag"
	"fmt"
	"io"
	"strconv"

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
