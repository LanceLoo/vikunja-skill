package main

import (
	"flag"
	"fmt"
	"io"

	"vikunja-opencode-skill/internal/client"
	"vikunja-opencode-skill/internal/config"
)

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

func printLabelsError(command string, err error, stderr io.Writer) int {
	return printResourceError(command, err, "标签读取被拒绝", "标签或任务不存在", stderr)
}

func validLabelFormat(format string) bool {
	return format == "" || format == "html" || format == "markdown"
}
