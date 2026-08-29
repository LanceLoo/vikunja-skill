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

func runProjectsUpdate(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--help" {
		printProjectsUpdateUsage(stdout)
		return 0
	}
	if len(args) == 0 || projectUpdateDuplicateFlags(args[1:]) {
		printProjectsUpdateUsage(stderr)
		return 2
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id < 1 {
		printProjectsUpdateUsage(stderr)
		return 2
	}
	f := flag.NewFlagSet("projects update", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	title := f.String("title", "", "项目标题")
	apply := f.Bool("apply", false, "执行更新")
	if err := f.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			printProjectsUpdateUsage(stdout)
			return 0
		}
		printProjectsUpdateUsage(stderr)
		return 2
	}
	titleSet := false
	f.Visit(func(v *flag.Flag) { titleSet = titleSet || v.Name == "title" })
	normalized := strings.TrimSpace(*title)
	if f.NArg() != 0 || !titleSet || normalized == "" || utf8.RuneCountInString(normalized) > 250 {
		printProjectsUpdateUsage(stderr)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "projects update: %v\n", err)
		return 1
	}
	api := client.New(nil)
	preflight, err := api.GetProjectTitleUpdateSnapshot(cfg.BaseURL, cfg.Token, id)
	if err != nil {
		return printProjectUpdatePreflightError(err, stderr)
	}
	if preflight.ID != id || !preflight.IsRoot {
		fmt.Fprintln(stderr, "projects update: 预检未确认目标为请求的根项目，未发送更新请求")
		return 1
	}
	base := projectUpdateOutput(id, preflight.Title, normalized)
	if !*apply {
		base["mode"] = "preview"
		base["plan_status"] = map[bool]string{true: "already_matches", false: "would_update"}[preflight.Title == normalized]
		base["maximum_affected"] = 1
		base["next_args"] = []string{"projects", "update", strconv.FormatInt(id, 10), "--title=" + normalized, "--apply"}
		return writeJSON("projects update", base, stdout, stderr)
	}
	if preflight.Title == normalized {
		base["mode"] = "apply"
		base["update_status"] = "not_sent_already_matched"
		base["maximum_affected"] = 0
		return writeJSON("projects update", base, stdout, stderr)
	}
	updated, patchErr := api.UpdateProjectTitle(context.Background(), cfg.BaseURL, cfg.Token, id, normalized)
	readback, readbackErr := api.GetProjectTitleUpdateSnapshot(cfg.BaseURL, cfg.Token, id)
	if patchErr != nil || updated.ID != id || updated.Title != normalized || !updated.IsRoot {
		fmt.Fprintln(stderr, "projects update: 更新结果可能未知；当前状态可能匹配但不能证明由本次请求造成，请勿盲目重试")
		return 1
	}
	if readbackErr != nil || readback.ID != id || readback.Title != normalized || !readback.IsRoot {
		fmt.Fprintln(stderr, "projects update: 更新请求已获确认，但当前状态未验证或不匹配，请勿自动重试")
		return 1
	}
	base["mode"] = "apply"
	base["maximum_affected"] = 1
	base["update_status"] = "patch_acknowledged"
	base["readback_status"] = "current_state_matched"
	return writeJSON("projects update", base, stdout, stderr)
}

func projectUpdateOutput(id int64, current, desired string) map[string]any {
	return map[string]any{"operation": "project_title_update", "project_id": id, "target_scope": "root_project", "current_title": current, "desired_title": desired, "default_project_status": "unknown_not_exposed", "concurrency_note": "last_write_wins_no_precondition"}
}

func projectUpdateDuplicateFlags(args []string) bool {
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name := strings.TrimPrefix(strings.TrimPrefix(strings.SplitN(arg, "=", 2)[0], "--"), "-")
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

func printProjectUpdatePreflightError(err error, stderr io.Writer) int {
	switch {
	case errors.Is(err, client.ErrUnauthorized):
		fmt.Fprintln(stderr, "projects update: 预检认证失败")
	case errors.Is(err, client.ErrForbidden):
		fmt.Fprintln(stderr, "projects update: 预检被拒绝")
	case errors.Is(err, client.ErrNotFound):
		fmt.Fprintln(stderr, "projects update: 预检返回 404")
	default:
		fmt.Fprintln(stderr, "projects update: 预检或快照验证未完成")
	}
	return 1
}
