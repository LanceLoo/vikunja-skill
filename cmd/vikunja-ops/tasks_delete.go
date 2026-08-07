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
	"strings"

	"vikunja-opencode-skill/internal/client"
	"vikunja-opencode-skill/internal/config"
)

// taskDeleteConfirmationPayload is the fixed confirmation-token payload for a
// single-task delete. The field order is part of the token contract and must
// never change; it contains only the normalized intent, never the raw command
// line, control flags like --apply/--confirm/--pretty, the URL, the token, or
// any task content such as the title.
type taskDeleteConfirmationPayload struct {
	Version   int    `json:"version"`
	Operation string `json:"operation"`
	TaskID    int64  `json:"task_id"`
	ProjectID int64  `json:"project_id"`
}

// taskDeleteConfirmationToken derives the deterministic confirmation token
// for one normalized single-task delete plan.
func taskDeleteConfirmationToken(taskID, projectID int64) (string, error) {
	payload, err := json.Marshal(taskDeleteConfirmationPayload{
		Version:   1,
		Operation: "tasks.delete",
		TaskID:    taskID,
		ProjectID: projectID,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// validTaskDeleteConfirmationFormat reports whether confirm has exactly the
// sha256:<64 lowercase hex> shape.
func validTaskDeleteConfirmationFormat(confirm string) bool {
	const prefix = "sha256:"
	if len(confirm) != len(prefix)+64 || !strings.HasPrefix(confirm, prefix) {
		return false
	}
	for _, c := range confirm[len(prefix):] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func runTasksDelete(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--help" {
		printTasksDeleteUsage(stdout)
		return 0
	}
	flags := flag.NewFlagSet("tasks delete", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	id := flags.Int64("id", 0, "任务 ID（必填，正整数）")
	projectID := flags.Int64("project-id", 0, "项目 ID（必填，正整数）")
	apply := flags.Bool("apply", false, "执行删除；默认仅预览")
	confirm := flags.String("confirm", "", "预览输出的 confirmation token；--apply 时必填")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			printTasksDeleteUsage(stdout)
			return 0
		}
		fmt.Fprintln(stderr, err)
		printTasksDeleteUsage(stderr)
		return 2
	}
	idSet, projectIDSet := false, false
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "id":
			idSet = true
		case "project-id":
			projectIDSet = true
		}
	})
	if flags.NArg() != 0 || !idSet || !projectIDSet || *id < 1 || *projectID < 1 {
		printTasksDeleteUsage(stderr)
		return 2
	}
	token, err := taskDeleteConfirmationToken(*id, *projectID)
	if err != nil {
		fmt.Fprintf(stderr, "tasks delete: 无法生成 confirmation token: %v\n", err)
		return 1
	}
	// All local validation and the token check must pass before any config
	// load or network request.
	if *confirm != "" && !*apply {
		fmt.Fprintln(stderr, "tasks delete: --confirm 只能与 --apply 一起使用")
		return 2
	}
	if *apply {
		if !validTaskDeleteConfirmationFormat(*confirm) {
			fmt.Fprintln(stderr, "tasks delete: --apply 需要 --confirm 提供 sha256:<64 位小写十六进制> 格式的 confirmation token")
			return 2
		}
		if *confirm != token {
			fmt.Fprintln(stderr, "tasks delete: --apply 需要 --confirm 提供与本次预览完全一致的 confirmation token")
			return 2
		}
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "tasks delete: %v\n", err)
		return 1
	}
	api := client.New(nil)
	// Preflight: exactly one GET before any write, verifying that the target
	// is currently readable and that both the returned task ID and project ID
	// match the request. This is not a TOCTOU guard; the delete endpoint
	// supports no conditional writes, so preview and apply are not atomic.
	task, code := preflightTaskDelete(api, cfg.BaseURL, cfg.Token, *id, *projectID, stderr)
	if code != 0 {
		return code
	}
	if !*apply {
		return writeJSON("tasks delete", map[string]any{
			"mode":               "preview",
			"operation":          "delete",
			"task_id":            *id,
			"project_id":         *projectID,
			"title":              task.Title,
			"confirmation_token": token,
			"maximum_affected":   1,
			"next":               fmt.Sprintf("vikunja-ops tasks delete --id %d --project-id %d --apply --confirm %s", *id, *projectID, token),
		}, stdout, stderr)
	}
	if err := api.DeleteTask(context.Background(), cfg.BaseURL, cfg.Token, *id); err != nil {
		return reportTaskDeleteFailure(api, cfg.BaseURL, cfg.Token, *id, err, stderr)
	}
	return reportTaskDeleteReadback(api, cfg.BaseURL, cfg.Token, *id, *projectID, token, stdout, stderr)
}

// preflightTaskDelete performs exactly one GET and enforces that the returned
// task ID and project ID match the requested scope. Any mismatch, error, or
// unexpected response means no DELETE may follow.
func preflightTaskDelete(api *client.Client, baseURL, token string, taskID, projectID int64, stderr io.Writer) (client.Task, int) {
	task, err := api.GetTask(baseURL, token, taskID)
	if err != nil {
		return client.Task{}, printTasksError("tasks delete", err, stderr)
	}
	if task.ID != taskID || task.ProjectID != projectID {
		fmt.Fprintln(stderr, "tasks delete: 预检失败：服务返回的任务 ID 或项目 ID 与请求不一致，已中止，未发送任何删除请求")
		return client.Task{}, 1
	}
	return task, 0
}

// reportTaskDeleteFailure handles every non-204 DELETE outcome. It performs
// exactly one GET reconciliation so the report can state the currently
// observed state, but it never claims a causal outcome from a 404 — a
// reconciliation 404 is only the fact that the task is not readable now.
func reportTaskDeleteFailure(api *client.Client, baseURL, token string, taskID int64, deleteErr error, stderr io.Writer) int {
	fmt.Fprintf(stderr, "tasks delete: %s；%s\n", describeTaskDeleteError(deleteErr), reconcileTaskDelete(api, baseURL, token, taskID))
	return 1
}

// reconcileTaskDelete performs exactly one GET and returns a safe,
// causality-free description of the observed state.
func reconcileTaskDelete(api *client.Client, baseURL, token string, taskID int64) string {
	task, err := api.GetTask(baseURL, token, taskID)
	switch {
	case err == nil && task.ID == taskID:
		return "回读显示任务仍存在"
	case err == nil:
		return "回读返回的任务 ID 与请求不一致"
	case errors.Is(err, client.ErrNotFound):
		return "回读返回 404（仅此事实，不能据此推断任务已被删除）"
	default:
		return "回读也未完成"
	}
}

// reportTaskDeleteReadback performs exactly one GET readback after a
// server-acknowledged 204 delete. A 404 readback means the task is currently
// not readable — it is reported as that fact alone, never as a causal claim
// that this command deleted the task; a 200 readback means the delete outcome
// is not as expected; any other readback failure means the delete was
// server-acknowledged but the readback is unverified.
func reportTaskDeleteReadback(api *client.Client, baseURL, token string, taskID, projectID int64, confirmToken string, stdout, stderr io.Writer) int {
	task, err := api.GetTask(baseURL, token, taskID)
	switch {
	case err == nil && task.ID == taskID:
		fmt.Fprintln(stderr, "tasks delete: 删除已获服务端确认（HTTP 204），但回读显示任务仍存在，删除未验证")
		return 1
	case err == nil:
		fmt.Fprintln(stderr, "tasks delete: 删除已获服务端确认（HTTP 204），但回读返回的任务 ID 不一致，删除未验证")
		return 1
	case errors.Is(err, client.ErrNotFound):
		return writeJSON("tasks delete", map[string]any{
			"mode":               "apply",
			"operation":          "delete",
			"task_id":            taskID,
			"project_id":         projectID,
			"maximum_affected":   1,
			"confirmation_token": confirmToken,
			"delete_status":      204,
			"readback":           "not_found",
			"note":               "服务端已确认收到删除请求（HTTP 204）；当前回读返回 404（仅此事实，不能据此推断由本命令删除了该任务）",
		}, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "tasks delete: 删除已获服务端确认（HTTP 204），但回读验证未完成（%s）\n", describeReadbackError(err))
		return 1
	}
}

// describeTaskDeleteError maps a failed DELETE to a safe classification. It
// never prints the token, the URL, query, fragment, or any response body.
func describeTaskDeleteError(err error) string {
	var statusErr *client.StatusError
	switch {
	case errors.Is(err, client.ErrUnauthorized):
		return "认证失败：服务未接受 Token"
	case errors.Is(err, client.ErrForbidden):
		return "权限不足：任务删除被拒绝（HTTP 403）"
	case errors.Is(err, client.ErrNotFound):
		return "删除请求返回 404：任务不存在"
	case errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusTooManyRequests:
		return "请求受限：服务端返回 HTTP 429，请稍后重试"
	case errors.As(err, &statusErr) && statusErr.StatusCode >= 500:
		return fmt.Sprintf("服务端错误（HTTP %d）：删除结果可能未知，请勿盲目重试", statusErr.StatusCode)
	case errors.As(err, &statusErr) && statusErr.StatusCode >= 400 && statusErr.StatusCode < 500:
		return fmt.Sprintf("删除请求被服务端明确拒绝（HTTP %d）", statusErr.StatusCode)
	default:
		return "删除请求结果可能未知，请勿盲目重试"
	}
}

// describeReadbackError maps a failed post-delete readback to a short safe
// reason.
func describeReadbackError(err error) string {
	var statusErr *client.StatusError
	switch {
	case errors.Is(err, client.ErrUnauthorized):
		return "回读认证失败"
	case errors.Is(err, client.ErrForbidden):
		return "回读被拒绝（HTTP 403）"
	case errors.As(err, &statusErr):
		return fmt.Sprintf("回读返回 HTTP %d", statusErr.StatusCode)
	default:
		return "回读网络错误"
	}
}
