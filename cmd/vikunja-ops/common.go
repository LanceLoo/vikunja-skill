package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"vikunja-opencode-skill/internal/client"
)

func bindPaginationFlags(flags *flag.FlagSet) (page, perPage *int) {
	return flags.Int("page", 1, "页码（默认 1）"), flags.Int("per-page", 50, "每页条目数（默认 50）")
}

func validPagination(page, perPage int) bool { return page >= 1 && perPage >= 1 && perPage <= 1000 }

type prettyJSONWriter struct {
	io.Writer
}

func writeJSON(command string, value any, stdout, stderr io.Writer) int {
	encoder := json.NewEncoder(stdout)
	if _, ok := stdout.(prettyJSONWriter); ok {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(value); err != nil {
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

func (values *stringSlice) String() string { return "" }

func (values *stringSlice) Set(value string) error {
	if value == "" {
		return errors.New("值不能为空")
	}
	*values = append(*values, value)
	return nil
}
