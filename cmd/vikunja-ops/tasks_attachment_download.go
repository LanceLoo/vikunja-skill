package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// attachmentDownloadPreview is the typed preview output of
// tasks attachments download. It contains only the normalized intent, never
// attachment content, the server URL, or the token.
type attachmentDownloadPreview struct {
	Mode                string `json:"mode"`
	Operation           string `json:"operation"`
	TaskID              int64  `json:"task_id"`
	AttachmentID        int64  `json:"attachment_id"`
	DestinationPath     string `json:"destination_path"`
	ExpectedSize        *int64 `json:"expected_size"`
	MaximumBytes        int64  `json:"maximum_bytes"`
	MaximumFilesWritten int    `json:"maximum_files_written"`
}

// attachmentDownloadResult is the typed apply output of
// tasks attachments download.
type attachmentDownloadResult struct {
	Mode                string `json:"mode"`
	Operation           string `json:"operation"`
	TaskID              int64  `json:"task_id"`
	AttachmentID        int64  `json:"attachment_id"`
	DestinationPath     string `json:"destination_path"`
	BytesWritten        int64  `json:"bytes_written"`
	SHA256              string `json:"sha256"`
	DownloadStatus      int    `json:"download_status"`
	LocalReadback       string `json:"local_readback"`
	MaximumFilesWritten int    `json:"maximum_files_written"`
}

// Narrow test seams for installation failure modes. They are not a general
// filesystem abstraction.
var (
	linkFile                     = os.Link
	removeFile                   = os.Remove
	checkDownloadDestinationSafe = checkDestinationAbsentAndParentSafe
)

func runTaskAttachmentDownload(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--help" {
		printTaskAttachmentDownloadUsage(stdout)
		return 0
	}
	flags := flag.NewFlagSet("tasks attachments download", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	taskID := flags.Int64("task-id", 0, "任务 ID（必填，正整数）")
	attachmentID := flags.Int64("attachment-id", 0, "附件 ID（必填，正整数）")
	output := flags.String("output", "", "本地输出路径（必填；目标必须不存在，父目录必须已存在）")
	apply := flags.Bool("apply", false, "执行下载；默认仅预览")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			printTaskAttachmentDownloadUsage(stdout)
			return 0
		}
		fmt.Fprintln(stderr, err)
		printTaskAttachmentDownloadUsage(stderr)
		return 2
	}
	taskIDSet, attachmentIDSet, outputSet := false, false, false
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "task-id":
			taskIDSet = true
		case "attachment-id":
			attachmentIDSet = true
		case "output":
			outputSet = true
		}
	})
	if flags.NArg() != 0 || !taskIDSet || !attachmentIDSet || !outputSet || *taskID < 1 || *attachmentID < 1 {
		printTaskAttachmentDownloadUsage(stderr)
		return 2
	}
	// All local validation, including output path syntax and filesystem
	// state, must pass before any config load or network request.
	destination, err := validateAttachmentOutputPath(*output)
	if err != nil {
		fmt.Fprintf(stderr, "tasks attachments download: 输出路径无效：%v\n", err)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "tasks attachments download: %v\n", err)
		return 1
	}
	api := client.New(nil)
	// Preflight: exactly one strict metadata GET in both modes; preview sends
	// no byte GET and creates nothing locally.
	meta, err := api.GetTaskAttachmentDownloadMeta(context.Background(), cfg.BaseURL, cfg.Token, *taskID, *attachmentID)
	if err != nil {
		return printAttachmentsError("tasks attachments download", err, stderr)
	}
	if !*apply {
		return writeJSON("tasks attachments download", attachmentDownloadPreview{
			Mode:                "preview",
			Operation:           "attachment_download",
			TaskID:              *taskID,
			AttachmentID:        *attachmentID,
			DestinationPath:     destination,
			ExpectedSize:        meta.Size,
			MaximumBytes:        client.MaxAttachmentDownloadBytes,
			MaximumFilesWritten: 1,
		}, stdout, stderr)
	}
	return applyAttachmentDownload(api, cfg, *taskID, meta, destination, stdout, stderr)
}

// applyAttachmentDownload performs exactly one byte GET after the strict
// metadata preflight, streaming at most one file to a random temporary name
// in the target parent and finalizing without overwrite. It never retries;
// on any failure before installation the temporary file is removed and the
// transfer outcome is reported as incomplete.
func applyAttachmentDownload(api *client.Client, cfg config.Config, taskID int64, meta client.AttachmentMeta, destination string, stdout, stderr io.Writer) int {
	fail := func(format string, args ...any) int {
		fmt.Fprintf(stderr, "tasks attachments download: "+format+"\n", args...)
		return 1
	}
	if meta.Size != nil && *meta.Size > client.MaxAttachmentDownloadBytes {
		return fail("附件元数据大小超过 100 MiB 上限，未下载")
	}
	// Centralized revalidation immediately before the byte request: final
	// absence plus the full parent ancestor chain.
	if err := checkDownloadDestinationSafe(destination); err != nil {
		return fail("输出路径校验失败：%v；未下载", err)
	}
	stream, err := api.GetTaskAttachmentBytes(context.Background(), cfg.BaseURL, cfg.Token, taskID, meta.ID)
	if err != nil {
		return printAttachmentsError("tasks attachments download", err, stderr)
	}
	defer stream.Close()
	if stream.ContentLength > client.MaxAttachmentDownloadBytes {
		return fail("响应 Content-Length 超过 100 MiB 上限，未下载")
	}
	if meta.Size != nil && stream.ContentLength >= 0 && stream.ContentLength != *meta.Size {
		return fail("响应 Content-Length 与元数据大小不一致，未下载")
	}
	parent := filepath.Dir(destination)
	temp, err := os.CreateTemp(parent, ".vikunja-ops-download-*.tmp")
	if err != nil {
		return fail("无法创建临时文件，未下载")
	}
	tempName := temp.Name()
	cleanupTemp := func() string {
		_ = temp.Close()
		if err := removeFile(tempName); err != nil {
			return "；临时文件清理失败，临时文件可能仍在原目录"
		}
		return "；已清理临时文件"
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(stream, client.MaxAttachmentDownloadBytes+1))
	if copyErr != nil {
		return fail("传输未完成：结果未知%s，请勿盲目重试", cleanupTemp())
	}
	if written > client.MaxAttachmentDownloadBytes {
		return fail("接收字节超过 100 MiB 上限%s", cleanupTemp())
	}
	if stream.ContentLength >= 0 && written != stream.ContentLength {
		return fail("接收字节与 Content-Length 不一致%s", cleanupTemp())
	}
	// Independent of Content-Length (which may be unknown), the received
	// bytes must exactly equal a typed-present metadata size.
	if meta.Size != nil && written != *meta.Size {
		return fail("接收字节与元数据大小不一致%s", cleanupTemp())
	}
	if err := stream.Close(); err != nil {
		return fail("传输未完成：结果未知%s，请勿盲目重试", cleanupTemp())
	}
	if err := temp.Sync(); err != nil {
		return fail("无法写入本地文件%s", cleanupTemp())
	}
	if err := temp.Close(); err != nil {
		return fail("无法写入本地文件%s", cleanupTemp())
	}
	// Centralized revalidation immediately before final installation.
	if err := checkDownloadDestinationSafe(destination); err != nil {
		return fail("输出路径校验失败：%v%s", err, cleanupTemp())
	}
	installed, err := installWithoutOverwrite(tempName, destination)
	if err != nil && !installed {
		return fail("无法在不覆盖的情况下完成写入：%v%s", err, cleanupTemp())
	}
	result, readbackErr := attachmentDownloadReadback(destination, written, hash, stream, taskID, meta)
	if err != nil {
		// The destination was linked but temporary removal failed: a file WAS
		// installed. Never claim nothing was written.
		if readbackErr != nil {
			fmt.Fprintln(stderr, "tasks attachments download: 目标文件已写入，但临时文件清理失败，且本地回读校验失败：结果未知，请人工核对目标文件与临时文件")
			return 1
		}
		fmt.Fprintln(stderr, "tasks attachments download: 目标文件已写入并通过本地回读，但临时文件清理失败，请人工清理临时文件")
		return writeJSON("tasks attachments download", result, stdout, stderr)
	}
	if readbackErr != nil {
		return fail("本地回读校验失败：结果未知，请人工核对目标文件")
	}
	return writeJSON("tasks attachments download", result, stdout, stderr)
}

// attachmentDownloadReadback verifies the installed destination is a regular
// file of the expected size and builds the typed success output.
func attachmentDownloadReadback(destination string, written int64, hash interface{ Sum([]byte) []byte }, stream *client.AttachmentByteStream, taskID int64, meta client.AttachmentMeta) (attachmentDownloadResult, error) {
	info, err := os.Lstat(destination)
	if err != nil || !info.Mode().IsRegular() || info.Size() != written {
		return attachmentDownloadResult{}, errors.New("readback failed")
	}
	return attachmentDownloadResult{
		Mode:                "apply",
		Operation:           "attachment_download",
		TaskID:              taskID,
		AttachmentID:        meta.ID,
		DestinationPath:     destination,
		BytesWritten:        written,
		SHA256:              "sha256:" + hex.EncodeToString(hash.Sum(nil)),
		DownloadStatus:      stream.StatusCode,
		LocalReadback:       "verified",
		MaximumFilesWritten: 1,
	}, nil
}

// installWithoutOverwrite finalizes temp as destination without overwriting:
// linkFile fails when the destination already exists, and removing the
// temporary name completes the move within the same directory. The temporary
// name is random and never derived from server data. installed reports
// whether the destination file was created, so a temporary-removal failure
// after a successful install is never misreported as "nothing written".
func installWithoutOverwrite(temp, destination string) (installed bool, err error) {
	if err := linkFile(temp, destination); err != nil {
		if os.IsExist(err) {
			return false, errors.New("输出目标已存在，不会覆盖")
		}
		return false, errors.New("目标无法以不覆盖方式创建")
	}
	if err := removeFile(temp); err != nil {
		return true, errors.New("临时文件清理失败")
	}
	return true, nil
}

// validateAttachmentOutputPath performs conservative local validation of the
// user-supplied output path. The destination must not already exist (file,
// directory, or symlink), the parent must already exist as an ordinary
// directory without symlinks or reparse points in its chain, and no
// directories are created. The server filename or Content-Disposition never
// influences the target.
func validateAttachmentOutputPath(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("输出路径不能为空")
	}
	if strings.IndexByte(raw, 0) >= 0 {
		return "", errors.New("输出路径不能包含 NUL 字符")
	}
	if strings.HasSuffix(raw, "/") || strings.HasSuffix(raw, `\`) {
		return "", errors.New("输出路径不能以路径分隔符结尾")
	}
	if unsafeWindowsPath(raw) {
		return "", errors.New("输出路径包含不安全的 Windows 特殊形式")
	}
	cleaned := filepath.Clean(raw)
	if cleaned == "." {
		return "", errors.New("输出路径不能为当前目录")
	}
	volume := filepath.VolumeName(cleaned)
	if rest := cleaned[len(volume):]; rest == "" || rest == "/" || rest == `\` {
		return "", errors.New("输出路径不能为卷根目录")
	}
	if err := checkDestinationAbsentAndParentSafe(cleaned); err != nil {
		return "", err
	}
	return cleaned, nil
}

// checkDestinationAbsentAndParentSafe is the single centralized destination
// revalidation: final absence plus a safe parent. It runs at initial
// validation, immediately before the byte request, and immediately before
// final installation.
func checkDestinationAbsentAndParentSafe(destination string) error {
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("输出目标已存在，不会覆盖")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return errors.New("无法确认输出目标不存在")
	}
	parent := filepath.Dir(destination)
	info, err := os.Lstat(parent)
	if err != nil {
		return errors.New("输出父目录不存在，不会自动创建")
	}
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return errors.New("输出父目录必须是普通目录，不能是符号链接或重解析点")
	}
	if parentChainUnsafe(parent) {
		return errors.New("输出路径的祖先目录不能包含符号链接或重解析点")
	}
	return nil
}

// parentChainUnsafe reports whether any ancestor directory of parent is a
// link (symlink or, on Windows, any reparse point such as a junction).
// Errors are treated as unsafe.
func parentChainUnsafe(parent string) bool {
	current, err := filepath.Abs(parent)
	if err != nil {
		return true
	}
	for {
		if pathComponentIsLink(current) {
			return true
		}
		next := filepath.Dir(current)
		if next == current {
			return false
		}
		current = next
	}
}
