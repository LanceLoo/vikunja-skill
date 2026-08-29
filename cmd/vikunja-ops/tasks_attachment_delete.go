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

	"vikunja-opencode-skill/internal/client"
	"vikunja-opencode-skill/internal/config"
)

// attachmentDeleteConfirmationPayload has deliberately stable field order and
// contains normalized intent only, never remote metadata or credentials.
type attachmentDeleteConfirmationPayload struct {
	Version      int    `json:"version"`
	Operation    string `json:"operation"`
	TaskID       int64  `json:"task_id"`
	AttachmentID int64  `json:"attachment_id"`
}

func attachmentDeleteConfirmationToken(taskID, attachmentID int64) (string, error) {
	b, err := json.Marshal(attachmentDeleteConfirmationPayload{1, "attachment_delete", taskID, attachmentID})
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:]), nil
}

func runTaskAttachmentDelete(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--help" {
		printTaskAttachmentDeleteUsage(stdout)
		return 0
	}
	f := flag.NewFlagSet("tasks attachments delete", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	taskID := f.Int64("task-id", 0, "任务 ID")
	attachmentID := f.Int64("attachment-id", 0, "附件 ID")
	apply := f.Bool("apply", false, "执行删除")
	confirm := f.String("confirm", "", "confirmation token")
	if err := f.Parse(args); err != nil {
		if err == flag.ErrHelp {
			printTaskAttachmentDeleteUsage(stdout)
			return 0
		}
		printTaskAttachmentDeleteUsage(stderr)
		return 2
	}
	taskSet, attachmentSet := false, false
	f.Visit(func(x *flag.Flag) {
		if x.Name == "task-id" {
			taskSet = true
		}
		if x.Name == "attachment-id" {
			attachmentSet = true
		}
	})
	if f.NArg() != 0 || !taskSet || !attachmentSet || *taskID < 1 || *attachmentID < 1 {
		printTaskAttachmentDeleteUsage(stderr)
		return 2
	}
	if *confirm != "" && !*apply {
		fmt.Fprintln(stderr, "tasks attachments delete: --confirm 只能与 --apply 一起使用")
		return 2
	}
	token, err := attachmentDeleteConfirmationToken(*taskID, *attachmentID)
	if err != nil {
		fmt.Fprintln(stderr, "tasks attachments delete: 无法生成 confirmation token")
		return 1
	}
	if *apply {
		if !validTaskDeleteConfirmationFormat(*confirm) {
			fmt.Fprintln(stderr, "tasks attachments delete: --apply 需要 sha256:<64 位小写十六进制> confirmation token")
			return 2
		}
		if *confirm != token {
			fmt.Fprintln(stderr, "tasks attachments delete: confirmation token 与本次预览不一致")
			return 2
		}
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "tasks attachments delete: %v\n", err)
		return 1
	}
	api := client.New(nil)
	if _, err = api.GetTaskAttachmentDeleteMeta(context.Background(), cfg.BaseURL, cfg.Token, *taskID, *attachmentID); err != nil {
		return printAttachmentDeletePreflightError(err, stderr)
	}
	if !*apply {
		return writeJSON("tasks attachments delete", map[string]any{"mode": "preview", "operation": "attachment_delete", "task_id": *taskID, "attachment_id": *attachmentID, "confirmation_token": token, "maximum_affected": 1, "next": fmt.Sprintf("vikunja-ops tasks attachments delete --task-id %d --attachment-id %d --apply --confirm %s", *taskID, *attachmentID, token)}, stdout, stderr)
	}
	if err := api.DeleteTaskAttachment(context.Background(), cfg.BaseURL, cfg.Token, *taskID, *attachmentID); err != nil {
		return reportAttachmentDeleteFailure(api, cfg, *taskID, *attachmentID, err, stderr)
	}
	return reportAttachmentDeleteReadback(api, cfg, *taskID, *attachmentID, token, stdout, stderr)
}

func printAttachmentDeletePreflightError(err error, stderr io.Writer) int {
	switch {
	case errors.Is(err, client.ErrNotFound):
		fmt.Fprintln(stderr, "tasks attachments delete: 任务或附件不存在")
	case errors.Is(err, client.ErrUnauthorized):
		fmt.Fprintln(stderr, "tasks attachments delete: 认证失败：服务未接受 Token")
	case errors.Is(err, client.ErrForbidden):
		fmt.Fprintln(stderr, "tasks attachments delete: 权限不足：附件删除预检被拒绝")
	default:
		fmt.Fprintln(stderr, "tasks attachments delete: 附件删除预检未完成")
	}
	return 1
}
func describeAttachmentDeleteError(err error) string {
	var s *client.StatusError
	switch {
	case errors.Is(err, client.ErrUnauthorized):
		return "认证失败：服务未接受 Token"
	case errors.Is(err, client.ErrForbidden):
		return "权限不足：附件删除被拒绝（HTTP 403）"
	case errors.Is(err, client.ErrNotFound):
		return "删除请求返回 404：任务或附件不存在"
	case errors.As(err, &s) && s.StatusCode == http.StatusTooManyRequests:
		return "请求受限：服务端返回 HTTP 429，请稍后重试"
	case errors.As(err, &s) && s.StatusCode >= 500:
		return fmt.Sprintf("服务端错误（HTTP %d）：删除结果可能未知，请勿盲目重试", s.StatusCode)
	case errors.As(err, &s) && s.StatusCode >= 400 && s.StatusCode < 500:
		return fmt.Sprintf("删除请求被服务端明确拒绝（HTTP %d）", s.StatusCode)
	default:
		return "删除请求结果可能未知，请勿盲目重试"
	}
}
func reconcileAttachmentDelete(api *client.Client, cfg config.Config, taskID, attachmentID int64) string {
	r, err := api.VerifyTaskAttachmentDeletedReadback(context.Background(), cfg.BaseURL, cfg.Token, taskID, attachmentID)
	switch {
	case err == nil && r.Present:
		return "回读显示附件仍存在"
	case err == nil:
		return "回读显示目标当前不存在（不能据此推断由本命令删除）"
	case errors.Is(err, client.ErrNotFound):
		return "回读返回 404（仅此事实，不能据此推断附件已被删除）"
	default:
		return "回读也未完成"
	}
}
func reportAttachmentDeleteFailure(api *client.Client, cfg config.Config, taskID, attachmentID int64, deleteErr error, stderr io.Writer) int {
	fmt.Fprintf(stderr, "tasks attachments delete: %s；%s\n", describeAttachmentDeleteError(deleteErr), reconcileAttachmentDelete(api, cfg, taskID, attachmentID))
	return 1
}
func reportAttachmentDeleteReadback(api *client.Client, cfg config.Config, taskID, attachmentID int64, token string, stdout, stderr io.Writer) int {
	r, err := api.VerifyTaskAttachmentDeletedReadback(context.Background(), cfg.BaseURL, cfg.Token, taskID, attachmentID)
	if err != nil {
		fmt.Fprintln(stderr, "tasks attachments delete: 服务端已确认 HTTP 204，但删除/回读未验证")
		return 1
	}
	if r.Present {
		fmt.Fprintln(stderr, "tasks attachments delete: 服务端已确认 HTTP 204，但回读显示附件仍存在，删除未验证")
		return 1
	}
	return writeJSON("tasks attachments delete", map[string]any{"mode": "apply", "operation": "attachment_delete", "task_id": taskID, "attachment_id": attachmentID, "maximum_affected": 1, "confirmation_token": token, "delete_status": 204, "readback": "absent", "note": "服务端已确认收到删除请求（HTTP 204）；完整当前回读未发现目标附件，不能据此推断由本命令删除。"}, stdout, stderr)
}
