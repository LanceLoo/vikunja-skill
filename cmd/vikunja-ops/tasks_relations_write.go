package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"vikunja-opencode-skill/internal/client"
	"vikunja-opencode-skill/internal/config"
)

// The field order of these payloads is part of the confirmation-token contract.
type taskRelationCreateConfirmationPayload struct {
	Version      int    `json:"version"`
	Operation    string `json:"operation"`
	TaskID       int64  `json:"taskID"`
	OtherTaskID  int64  `json:"otherTaskID"`
	RelationKind string `json:"relationKind"`
}

type taskRelationDeleteConfirmationPayload struct {
	Version      int    `json:"version"`
	Operation    string `json:"operation"`
	TaskID       int64  `json:"taskID"`
	RelationKind string `json:"relationKind"`
	OtherTaskID  int64  `json:"otherTaskID"`
}

func taskRelationConfirmationToken(operation string, taskID, otherTaskID int64, kind string) (string, error) {
	var value any
	switch operation {
	case "relation_create":
		value = taskRelationCreateConfirmationPayload{1, operation, taskID, otherTaskID, kind}
	case "relation_delete":
		value = taskRelationDeleteConfirmationPayload{1, operation, taskID, kind, otherTaskID}
	default:
		return "", errors.New("invalid relation operation")
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func runTaskRelationWrite(operation string, args []string, stdout, stderr io.Writer) int {
	usage := printTaskRelationsCreateUsage
	if operation == "relation_delete" {
		usage = printTaskRelationsDeleteUsage
	}
	if len(args) == 1 && args[0] == "--help" {
		usage(stdout)
		return 0
	}
	if relationWriteDuplicateFlags(args) {
		usage(stderr)
		return 2
	}
	flags := flag.NewFlagSet("tasks relations", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	taskID := flags.Int64("task-id", 0, "任务 ID")
	otherTaskID := flags.Int64("other-task-id", 0, "关联任务 ID")
	kind := flags.String("relation-kind", "", "关系类型")
	apply := flags.Bool("apply", false, "执行写入")
	confirm := flags.String("confirm", "", "确认 token")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			usage(stdout)
			return 0
		}
		usage(stderr)
		return 2
	}
	set := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if flags.NArg() != 0 || !set["task-id"] || !set["other-task-id"] || !set["relation-kind"] ||
		*taskID < 1 || *otherTaskID < 1 || *taskID == *otherTaskID || !client.ValidTaskRelationKind(*kind) {
		usage(stderr)
		return 2
	}
	confirmToken, err := taskRelationConfirmationToken(operation, *taskID, *otherTaskID, *kind)
	if err != nil {
		fmt.Fprintln(stderr, "tasks relations: 无法生成 confirmation token")
		return 1
	}
	if set["confirm"] && !*apply {
		fmt.Fprintln(stderr, "tasks relations: --confirm 只能与 --apply 一起使用")
		return 2
	}
	if *apply && (!validTaskDeleteConfirmationFormat(*confirm) || *confirm != confirmToken) {
		fmt.Fprintln(stderr, "tasks relations: --apply 需要与本次预览完全一致的 sha256 confirmation token")
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "tasks relations: %v\n", err)
		return 1
	}
	api := client.New(nil)
	snapshot, err := api.GetTaskRelationSnapshot(cfg.BaseURL, cfg.Token, *taskID)
	if err != nil {
		return printTaskRelationSnapshotError(err, stderr)
	}
	if operation == "relation_create" && snapshot.HasAnyDirectRelation(*otherTaskID) {
		fmt.Fprintf(stderr, "tasks relations: 预检失败：当前源任务已与目标任务存在关系（%s），未发送写入请求\n", strings.Join(snapshot.KindsFor(*otherTaskID), ", "))
		return 1
	}
	if operation == "relation_delete" && !snapshot.Has(*kind, *otherTaskID) {
		fmt.Fprintln(stderr, "tasks relations: 预检失败：当前源任务不存在请求的直接关系，未发送写入请求")
		return 1
	}
	if !*apply {
		return writeJSON("tasks relations", relationWriteOutput("preview", operation, *taskID, *otherTaskID, *kind, confirmToken, 0), stdout, stderr)
	}

	var status int
	var writeErr error
	if operation == "relation_create" {
		status, writeErr = api.CreateTaskRelation(cfg.BaseURL, cfg.Token, client.CreateTaskRelationInput{TaskID: *taskID, OtherTaskID: *otherTaskID, RelationKind: *kind})
	} else {
		status, writeErr = api.DeleteTaskRelation(cfg.BaseURL, cfg.Token, *taskID, *kind, *otherTaskID)
	}
	readback, readbackErr := api.GetTaskRelationSnapshot(cfg.BaseURL, cfg.Token, *taskID)
	if writeErr != nil {
		fmt.Fprintf(stderr, "tasks relations: %s；%s\n", describeTaskRelationWriteError(operation, writeErr), describeTaskRelationReadback(readback, readbackErr, *otherTaskID))
		return 1
	}
	if readbackErr != nil {
		fmt.Fprintf(stderr, "tasks relations: 写入获服务端确认（HTTP %d），但回读未完成（%s）\n", status, describeReadbackError(readbackErr))
		return 1
	}
	present := readback.Has(*kind, *otherTaskID)
	if (operation == "relation_create" && !present) || (operation == "relation_delete" && readback.HasAnyDirectRelation(*otherTaskID)) {
		fmt.Fprintf(stderr, "tasks relations: 写入获服务端确认（HTTP %d），但当前回读状态与预期不符\n", status)
		return 1
	}
	output := relationWriteOutput("apply", operation, *taskID, *otherTaskID, *kind, confirmToken, status)
	if operation == "relation_create" {
		output["readback"] = "present"
	} else {
		output["readback"] = "absent"
	}
	return writeJSON("tasks relations", output, stdout, stderr)
}

func relationWriteOutput(mode, operation string, taskID, otherTaskID int64, kind, token string, status int) map[string]any {
	output := map[string]any{"mode": mode, "operation": operation, "task_id": taskID, "other_task_id": otherTaskID, "relation_kind": kind, "confirmation_token": token, "maximum_affected": 1,
		"next": fmt.Sprintf("vikunja-ops tasks relations %s --task-id %d --other-task-id %d --relation-kind %s --apply --confirm %s", strings.TrimPrefix(operation, "relation_"), taskID, otherTaskID, kind, token)}
	if mode == "apply" {
		delete(output, "next")
		if operation == "relation_create" {
			output["create_status"] = status
		} else {
			output["delete_status"] = status
		}
	}
	return output
}

func relationWriteDuplicateFlags(args []string) bool {
	seen := map[string]bool{}
	for _, arg := range args {
		name := strings.SplitN(arg, "=", 2)[0]
		// flag accepts both -name and --name. Normalize the spelling before
		// counting so mixed spellings cannot silently select the final value.
		name = strings.TrimPrefix(name, "--")
		name = strings.TrimPrefix(name, "-")
		switch name {
		case "task-id", "other-task-id", "relation-kind", "apply", "confirm":
			if seen[name] {
				return true
			}
			seen[name] = true
		}
	}
	return false
}

func describeTaskRelationWriteError(operation string, err error) string {
	var statusErr *client.StatusError
	switch {
	case errors.Is(err, client.ErrUnauthorized):
		return "认证失败：服务未接受 Token"
	case errors.Is(err, client.ErrForbidden):
		return "权限不足：关系写入被拒绝（HTTP 403）"
	case errors.Is(err, client.ErrNotFound):
		return "关系写入请求返回 404"
	}
	if errors.As(err, &statusErr) {
		return fmt.Sprintf("关系%s请求被服务端拒绝（HTTP %d）", strings.TrimPrefix(operation, "relation_"), statusErr.StatusCode)
	}
	return "关系写入请求结果可能未知，请勿盲目重试"
}

func printTaskRelationSnapshotError(err error, stderr io.Writer) int {
	var statusErr *client.StatusError
	switch {
	case errors.Is(err, client.ErrUnauthorized):
		fmt.Fprintln(stderr, "tasks relations: 认证失败：服务未接受 Token")
	case errors.Is(err, client.ErrForbidden):
		fmt.Fprintln(stderr, "tasks relations: 权限不足：关系预检被拒绝（HTTP 403）")
	case errors.Is(err, client.ErrNotFound):
		fmt.Fprintln(stderr, "tasks relations: 关系预检返回 404")
	case errors.As(err, &statusErr):
		fmt.Fprintf(stderr, "tasks relations: 关系预检返回 HTTP %d\n", statusErr.StatusCode)
	default:
		fmt.Fprintln(stderr, "tasks relations: 关系预检或快照解析未完成")
	}
	return 1
}

func describeTaskRelationReadback(snapshot client.TaskRelationSnapshot, err error, otherTaskID int64) string {
	if err != nil {
		return "回读未完成"
	}
	if snapshot.HasAnyDirectRelation(otherTaskID) {
		return "回读显示当前仍存在直接关系"
	}
	return "回读显示当前不存在直接关系"
}
