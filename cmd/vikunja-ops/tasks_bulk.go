package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"vikunja-opencode-skill/internal/client"
	"vikunja-opencode-skill/internal/config"
)

const maxBulkUpdateTaskIDs = 100

// bulkUpdateConfirmationPayload is the fixed confirmation-token payload. The
// field order is part of the token contract and must never change; it
// contains only the normalized plan, never the raw command line, flags like
// --apply/--confirm/--pretty, the URL, the token, or any task content.
type bulkUpdateConfirmationPayload struct {
	Version   int         `json:"version"`
	Operation string      `json:"operation"`
	TargetIDs []int64     `json:"target_ids"`
	Changes   taskChanges `json:"changes"`
}

// bulkUpdateConfirmationToken derives the deterministic confirmation token
// for one normalized bulk-update plan.
func bulkUpdateConfirmationToken(ids []int64, changes taskChanges) (string, error) {
	payload, err := json.Marshal(bulkUpdateConfirmationPayload{
		Version:   1,
		Operation: "tasks.bulk-update",
		TargetIDs: ids,
		Changes:   changes,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// parseBulkTaskIDs validates the raw --ids value and returns the deduplicated
// IDs in ascending numeric order. It performs no I/O.
func parseBulkTaskIDs(raw string) ([]int64, bool) {
	if raw == "" {
		return nil, false
	}
	segments := strings.Split(raw, ",")
	if len(segments) > maxBulkUpdateTaskIDs {
		return nil, false
	}
	seen := make(map[int64]struct{}, len(segments))
	ids := make([]int64, 0, len(segments))
	for _, segment := range segments {
		if segment == "" || strings.TrimSpace(segment) != segment {
			return nil, false
		}
		id, err := strconv.ParseInt(segment, 10, 64)
		if err != nil || id < 1 {
			return nil, false
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, false
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, true
}

// bulkUpdateFields lists the explicitly changed fields in the fixed canonical
// order required by the bulk endpoint.
func bulkUpdateFields(changes taskChanges) []string {
	fields := make([]string, 0, 4)
	if changes.Title != nil {
		fields = append(fields, "title")
	}
	if changes.Description != nil {
		fields = append(fields, "description")
	}
	if changes.Priority != nil {
		fields = append(fields, "priority")
	}
	if changes.DueDate != nil {
		fields = append(fields, "due_date")
	}
	return fields
}

func runTasksBulkUpdate(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--help" {
		printTasksBulkUpdateUsage(stdout)
		return 0
	}
	flags := flag.NewFlagSet("tasks bulk-update", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	idsFlag := flags.String("ids", "", "逗号分隔的任务 ID（必填，最多 100 个）")
	title, description, priority, dueDate := bindTaskChangeFlags(flags)
	apply := flags.Bool("apply", false, "执行批量写入；默认仅预览")
	confirm := flags.String("confirm", "", "预览输出的 confirmation token；--apply 时必填")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			printTasksBulkUpdateUsage(stdout)
			return 0
		}
		fmt.Fprintln(stderr, err)
		printTasksBulkUpdateUsage(stderr)
		return 2
	}
	changes, specified := taskChangeFlags(flags, title, description, priority, dueDate)
	ids, validIDs := parseBulkTaskIDs(*idsFlag)
	if flags.NArg() != 0 || !validIDs || !specified || !validTaskChanges(changes, false) {
		printTasksBulkUpdateUsage(stderr)
		return 2
	}
	token, err := bulkUpdateConfirmationToken(ids, changes)
	if err != nil {
		fmt.Fprintf(stderr, "tasks bulk-update: 无法生成 confirmation token: %v\n", err)
		return 1
	}
	// All local validation and the token check must pass before any config
	// load or network request.
	if *confirm != "" && !*apply {
		fmt.Fprintln(stderr, "tasks bulk-update: --confirm 只能与 --apply 一起使用")
		return 2
	}
	if *apply && (*confirm == "" || *confirm != token) {
		fmt.Fprintln(stderr, "tasks bulk-update: --apply 需要 --confirm 提供与本次预览完全一致的 confirmation token")
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "tasks bulk-update: %v\n", err)
		return 1
	}
	api := client.New(nil)
	// Preflight re-read: every target is fetched in ascending order before any
	// write. This only verifies that each target is currently readable and
	// that the service returns exactly the requested ID; it is not a TOCTOU
	// guard and not a conditional write. The confirmation token binds only
	// the approved normalized intent (IDs + changes), never a server state
	// snapshot; the update endpoint supports no conditional writes, so
	// preview and apply are not atomic.
	current := make([]client.Task, 0, len(ids))
	for _, id := range ids {
		task, err := api.GetTask(cfg.BaseURL, cfg.Token, id)
		if err != nil {
			return printTasksError("tasks bulk-update", err, stderr)
		}
		if task.ID != id {
			fmt.Fprintf(stderr, "tasks bulk-update: 预检失败：服务返回的任务 ID 与请求的 ID 不一致，已中止，未发送任何写请求\n")
			return 1
		}
		current = append(current, task)
	}
	if !*apply {
		return writeJSON("tasks bulk-update", map[string]any{
			"mode":               "preview",
			"operation":          "bulk_update",
			"target_ids":         ids,
			"requested_count":    len(ids),
			"ready_count":        len(current),
			"tasks":              current,
			"changes":            changes,
			"confirmation_token": token,
			"failure_strategy":   "server_atomic_batch",
		}, stdout, stderr)
	}
	result, err := api.BulkUpdateTasks(context.Background(), cfg.BaseURL, cfg.Token, client.BulkUpdateTasksInput{
		TaskIDs: ids,
		Fields:  bulkUpdateFields(changes),
		Values: client.UpdateTaskInput{
			Title: changes.Title, Description: changes.Description,
			Priority: changes.Priority, DueDate: changes.DueDate,
		},
	})
	if err != nil {
		return printBulkUpdateError("tasks bulk-update", err, stderr)
	}
	return writeJSON("tasks bulk-update", map[string]any{
		"mode":               "apply",
		"operation":          "bulk_update",
		"target_ids":         ids,
		"requested_count":    len(ids),
		"ready_count":        len(current),
		"succeeded_count":    len(ids), // a single bulk HTTP 200 means the whole batch was accepted atomically
		"returned_count":     len(result.Tasks),
		"tasks":              result.Tasks,
		"changes":            changes,
		"confirmation_token": token,
		"failure_strategy":   "server_atomic_batch",
	}, stdout, stderr)
}

// printBulkUpdateError maps bulk-update failures to safe stderr messages.
// 401/403/404 keep the existing safe resource mapping; explicit 4xx statuses
// (400/409/422 and any other 4xx) mean the server clearly rejected the
// request, while 429 means the request was rate-limited. 5xx statuses,
// transport failures, and success-response decode failures leave the batch
// outcome unknown, so the message warns against blind retries. It never
// prints the token, the URL, query, fragment, or any response body.
func printBulkUpdateError(command string, err error, stderr io.Writer) int {
	var statusErr *client.StatusError
	switch {
	case errors.Is(err, client.ErrUnauthorized):
		fmt.Fprintf(stderr, "%s: 认证失败：服务未接受 Token\n", command)
	case errors.Is(err, client.ErrForbidden):
		fmt.Fprintf(stderr, "%s: 权限不足：任务批量更新被拒绝\n", command)
	case errors.Is(err, client.ErrNotFound):
		fmt.Fprintf(stderr, "%s: 任务或项目不存在\n", command)
	case errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusTooManyRequests:
		fmt.Fprintf(stderr, "%s: 请求受限：服务端返回 HTTP 429，请稍后重试\n", command)
	case errors.As(err, &statusErr) && statusErr.StatusCode >= 500:
		fmt.Fprintf(stderr, "%s: 服务端错误（HTTP %d）：批量请求结果可能未知，请勿盲目重试\n", command, statusErr.StatusCode)
	case errors.As(err, &statusErr) && statusErr.StatusCode >= 400 && statusErr.StatusCode < 500:
		fmt.Fprintf(stderr, "%s: 请求被服务端明确拒绝（HTTP %d）\n", command, statusErr.StatusCode)
	default:
		fmt.Fprintf(stderr, "%s: 批量请求结果可能未知，请勿盲目重试：%v\n", command, err)
	}
	return 1
}
