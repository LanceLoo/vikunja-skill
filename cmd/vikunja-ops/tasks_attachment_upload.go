package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"vikunja-opencode-skill/internal/client"
	"vikunja-opencode-skill/internal/config"
)

// The payload deliberately excludes the source path and credentials. Field
// order is a versioned part of the confirmation-token contract.
type attachmentUploadConfirmationPayload struct {
	Version   int    `json:"version"`
	Operation string `json:"operation"`
	TaskID    int64  `json:"task_id"`
	Filename  string `json:"filename"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
}
type attachmentUploadManifest struct {
	filename string
	size     int64
	sha256   string
	info     fs.FileInfo
}

// Narrow test seam for the interval between the initial streaming manifest and
// the private memory capture.
var captureAttachmentUpload = captureAttachmentUploadSnapshot

func attachmentUploadToken(taskID int64, m attachmentUploadManifest) (string, error) {
	b, err := json.Marshal(attachmentUploadConfirmationPayload{1, "attachment_upload", taskID, m.filename, m.size, m.sha256})
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:]), nil
}

func runTaskAttachmentUpload(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--help" {
		printTaskAttachmentUploadUsage(stdout)
		return 0
	}
	f := flag.NewFlagSet("tasks attachments upload", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	taskID := f.Int64("task-id", 0, "任务 ID")
	source := f.String("file", "", "本地单个文件")
	apply := f.Bool("apply", false, "执行上传")
	confirm := f.String("confirm", "", "confirmation token")
	if err := f.Parse(args); err != nil {
		if err == flag.ErrHelp {
			printTaskAttachmentUploadUsage(stdout)
			return 0
		}
		printTaskAttachmentUploadUsage(stderr)
		return 2
	}
	taskSet, fileSet := false, false
	f.Visit(func(x *flag.Flag) {
		if x.Name == "task-id" {
			taskSet = true
		}
		if x.Name == "file" {
			fileSet = true
		}
	})
	if f.NArg() != 0 || !taskSet || !fileSet || countAttachmentUploadFileFlags(args) != 1 || *taskID < 1 {
		printTaskAttachmentUploadUsage(stderr)
		return 2
	}
	if *confirm != "" && !*apply {
		fmt.Fprintln(stderr, "tasks attachments upload: --confirm 只能与 --apply 一起使用")
		return 2
	}
	// Reject a malformed gate before touching the filesystem. This makes the
	// syntactic validation contract independent of local source state.
	if *apply && !validTaskDeleteConfirmationFormat(*confirm) {
		fmt.Fprintln(stderr, "tasks attachments upload: --apply 需要 sha256:<64 位小写十六进制> confirmation token")
		return 2
	}
	m, clean, err := inspectAttachmentUploadSource(*source)
	if err != nil {
		fmt.Fprintln(stderr, "tasks attachments upload: 本地源文件无效")
		return 2
	}
	token, err := attachmentUploadToken(*taskID, m)
	if err != nil {
		fmt.Fprintln(stderr, "tasks attachments upload: 无法生成 confirmation token")
		return 1
	}
	if *apply {
		if *confirm != token {
			fmt.Fprintln(stderr, "tasks attachments upload: confirmation token 与当前源文件不一致，未发送请求")
			return 2
		}
	}
	var snapshot []byte
	if *apply {
		snapshot, err = captureAttachmentUpload(clean, m)
		if err != nil {
			fmt.Fprintln(stderr, "tasks attachments upload: 源文件在上传前发生变化，未发送请求")
			return 1
		}
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "tasks attachments upload: %v\n", err)
		return 1
	}
	api := client.New(nil)
	task, err := api.GetTask(cfg.BaseURL, cfg.Token, *taskID)
	if err != nil {
		return printAttachmentsError("tasks attachments upload", err, stderr)
	}
	if task.ID != *taskID {
		fmt.Fprintln(stderr, "tasks attachments upload: 预检返回的任务 ID 不一致")
		return 1
	}
	if !*apply {
		return writeJSON("tasks attachments upload", map[string]any{"mode": "preview", "operation": "attachment_upload", "task_id": *taskID, "filename": m.filename, "expected_size": m.size, "sha256": "sha256:" + m.sha256, "maximum_bytes": client.MaxAttachmentUploadBytes, "maximum_files_written": 1, "confirmation_token": token}, stdout, stderr)
	}
	return applyAttachmentUpload(api, cfg, *taskID, snapshot, m, token, stdout, stderr)
}

// flag.FlagSet intentionally permits repeated scalar flags. This command's
// safety contract is one source only, so inspect the original argument list
// before accepting it. --file=value and --file value are both one occurrence.
func countAttachmentUploadFileFlags(args []string) int {
	n := 0
	for _, arg := range args {
		if arg == "--file" || strings.HasPrefix(arg, "--file=") {
			n++
		}
	}
	return n
}

func inspectAttachmentUploadSource(raw string) (attachmentUploadManifest, string, error) {
	if raw == "" || strings.IndexByte(raw, 0) >= 0 || strings.HasSuffix(raw, "/") || strings.HasSuffix(raw, `\`) || unsafeWindowsPath(raw) {
		return attachmentUploadManifest{}, "", errors.New("unsafe path")
	}
	clean := filepath.Clean(raw)
	vol := filepath.VolumeName(clean)
	rest := clean[len(vol):]
	if clean == "." || rest == "" || rest == "/" || rest == `\` {
		return attachmentUploadManifest{}, "", errors.New("directory path")
	}
	name := filepath.Base(clean)
	if name == "." || name == ".." || componentUnsafeOnWindows(name) {
		return attachmentUploadManifest{}, "", errors.New("unsafe filename")
	}
	info, err := os.Lstat(clean)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&fs.ModeSymlink != 0 || pathComponentIsLink(clean) || info.Size() > client.MaxAttachmentUploadBytes {
		return attachmentUploadManifest{}, "", errors.New("not a safe regular file")
	}
	parent := filepath.Dir(clean)
	pi, err := os.Lstat(parent)
	if err != nil || !pi.IsDir() || pathComponentIsLink(parent) || parentChainUnsafe(parent) {
		return attachmentUploadManifest{}, "", errors.New("unsafe parent")
	}
	h, size, err := hashAttachmentSource(clean, info)
	if err != nil {
		return attachmentUploadManifest{}, "", err
	}
	return attachmentUploadManifest{name, size, h, info}, clean, nil
}

func hashAttachmentSource(path string, expected fs.FileInfo) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(expected, actual) {
		return "", 0, errors.New("source changed")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, client.MaxAttachmentUploadBytes+1))
	if err != nil || n > client.MaxAttachmentUploadBytes || n != expected.Size() {
		return "", 0, errors.New("source changed or invalid")
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// captureAttachmentUploadSnapshot reads the approved source once into private
// memory. The descriptor identity and manifest checks reject ordinary changes
// observed while capturing; the returned bytes are subsequently the only bytes
// supplied to the HTTP client.
func captureAttachmentUploadSnapshot(path string, expected attachmentUploadManifest) ([]byte, error) {
	src, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil || !os.SameFile(expected.info, info) {
		return nil, errors.New("source replaced")
	}
	snapshot, err := io.ReadAll(io.LimitReader(src, client.MaxAttachmentUploadBytes+1))
	if err != nil || int64(len(snapshot)) != expected.size || int64(len(snapshot)) > client.MaxAttachmentUploadBytes {
		return nil, errors.New("source changed")
	}
	sum := sha256.Sum256(snapshot)
	if hex.EncodeToString(sum[:]) != expected.sha256 {
		return nil, errors.New("source changed")
	}
	return snapshot, nil
}

func applyAttachmentUpload(api *client.Client, cfg config.Config, taskID int64, snapshot []byte, m attachmentUploadManifest, token string, stdout, stderr io.Writer) int {
	result, err := api.UploadTaskAttachment(context.Background(), cfg.BaseURL, cfg.Token, client.TaskAttachmentUploadInput{TaskID: taskID, Filename: m.filename, Size: m.size, Reader: bytes.NewReader(snapshot)})
	if err != nil {
		if errors.Is(err, client.ErrTaskAttachmentUploadResponseUnverified) {
			fmt.Fprintln(stderr, "tasks attachments upload: 服务端已确认 HTTP 201，但响应未验证；附件可能已创建，请勿盲目重试")
			return 1
		}
		fmt.Fprintln(stderr, "tasks attachments upload: 上传结果可能未知，请勿盲目重试")
		return 1
	}
	if err := api.VerifyTaskAttachmentUploadReadback(context.Background(), cfg.BaseURL, cfg.Token, result); err != nil {
		fmt.Fprintln(stderr, "tasks attachments upload: 服务端已确认 HTTP 201，但远程回读未验证")
		return 1
	}
	return writeJSON("tasks attachments upload", map[string]any{"mode": "apply", "operation": "attachment_upload", "task_id": taskID, "filename": m.filename, "expected_size": m.size, "sha256": "sha256:" + m.sha256, "maximum_bytes": client.MaxAttachmentUploadBytes, "maximum_files_written": 1, "confirmation_token": token, "attachment_id": result.ID, "upload_status": 201, "remote_readback": "verified"}, stdout, stderr)
}
