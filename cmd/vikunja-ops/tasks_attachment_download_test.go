package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const attachmentDownloadToken = "test-secret-token"

type attachmentDownloadRequest struct {
	method, path, query string
}

type attachmentDownloadServerOptions struct {
	listStatus           int
	listBody             string
	dataStatus           int
	data                 []byte
	dataContentLength    *int64 // explicit Content-Length header override
	abortDataMidTransfer bool
	chunkedWriter        func(w io.Writer) // chunked streaming body (unknown length)
}

const attachmentDownloadListBody = `{"$schema":"https://x","items":[{"id":5,"size":4,"name":"` + attachmentDownloadToken + `-server-name.bin","secret":"` + attachmentDownloadToken + `"}],"page":1,"per_page":1000,"total":1,"total_pages":1}`

func newAttachmentDownloadServer(t *testing.T, opts attachmentDownloadServerOptions) (*httptest.Server, *[]attachmentDownloadRequest) {
	t.Helper()
	if opts.listStatus == 0 {
		opts.listStatus = http.StatusOK
	}
	if opts.listBody == "" {
		opts.listBody = attachmentDownloadListBody
	}
	if opts.dataStatus == 0 {
		opts.dataStatus = http.StatusOK
	}
	if opts.data == nil && opts.chunkedWriter == nil {
		opts.data = []byte("data")
	}
	requests := &[]attachmentDownloadRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+attachmentDownloadToken {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		*requests = append(*requests, attachmentDownloadRequest{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery})
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/tasks/12/attachments":
			w.WriteHeader(opts.listStatus)
			if opts.listStatus == http.StatusOK {
				_, _ = w.Write([]byte(opts.listBody))
			} else {
				_, _ = w.Write([]byte(`{"message":"server detail with ` + attachmentDownloadToken + `"}`))
			}
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/tasks/12/attachments/5":
			if opts.dataStatus != http.StatusOK {
				w.WriteHeader(opts.dataStatus)
				_, _ = w.Write([]byte(`{"message":"server detail with ` + attachmentDownloadToken + `"}`))
				return
			}
			if opts.dataContentLength != nil {
				w.Header().Set("Content-Length", fmt.Sprint(*opts.dataContentLength))
			}
			w.WriteHeader(http.StatusOK)
			if opts.abortDataMidTransfer {
				if len(opts.data) > 1 {
					_, _ = w.Write(opts.data[:1])
				}
				if hijacker, ok := w.(http.Hijacker); ok {
					if conn, _, err := hijacker.Hijack(); err == nil {
						_ = conn.Close()
					}
				}
				return
			}
			if opts.chunkedWriter != nil {
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				opts.chunkedWriter(w)
				return
			}
			_, _ = w.Write(opts.data)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	return server, requests
}

func attachmentDownloadArgs(extra ...string) []string {
	return append([]string{"tasks", "attachments", "download"}, extra...)
}

func tempOutputPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

func countLeftoverTemps(t *testing.T, destination string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(destination))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	count := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".vikunja-ops-download-") {
			count++
		}
	}
	return count
}

func runAttachmentDownload(t *testing.T, server *httptest.Server, extra ...string) (int, string, string) {
	t.Helper()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", attachmentDownloadToken)
	var stdout, stderr bytes.Buffer
	code := run(attachmentDownloadArgs(extra...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestTaskAttachmentDownloadHelp(t *testing.T) {
	for _, args := range [][]string{
		{"tasks", "attachments", "download", "--help"},
		{"tasks", "--help"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 {
			t.Errorf("run(%v) = %d, want 0", args, code)
		}
		if !strings.Contains(stdout.String(), "tasks attachments download") || !strings.Contains(stdout.String(), "--output") || stderr.Len() != 0 {
			t.Errorf("run(%v): stdout %q stderr %q", args, stdout.String(), stderr.String())
		}
	}
}

func TestTaskAttachmentDownloadInvalidArgumentsDoNotRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", attachmentDownloadToken)

	parent := t.TempDir()
	existing := filepath.Join(parent, "existing.bin")
	if err := os.WriteFile(existing, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	volumeRoot := filepath.VolumeName(parent) + string(filepath.Separator)

	cases := [][]string{
		attachmentDownloadArgs(),
		attachmentDownloadArgs("--task-id", "12"),
		attachmentDownloadArgs("--task-id", "12", "--attachment-id", "5"),
		attachmentDownloadArgs("--task-id", "0", "--attachment-id", "5", "--output", filepath.Join(parent, "o")),
		attachmentDownloadArgs("--task-id", "-1", "--attachment-id", "5", "--output", filepath.Join(parent, "o")),
		attachmentDownloadArgs("--task-id", "abc", "--attachment-id", "5", "--output", filepath.Join(parent, "o")),
		attachmentDownloadArgs("--task-id", "12", "--attachment-id", "0", "--output", filepath.Join(parent, "o")),
		attachmentDownloadArgs("--task-id", "12", "--attachment-id", "-5", "--output", filepath.Join(parent, "o")),
		attachmentDownloadArgs("12", "--task-id", "12", "--attachment-id", "5", "--output", filepath.Join(parent, "o")), // positional unsupported
		attachmentDownloadArgs("--task-id", "12", "--attachment-id", "5", "--output", filepath.Join(parent, "o"), "extra"),
		attachmentDownloadArgs("--task-id", "12", "--attachment-id", "5", "--output", filepath.Join(parent, "o"), "--confirm", "sha256:x"), // confirm unsupported
		attachmentDownloadArgs("--task-id", "12", "--attachment-id", "5", "--output", filepath.Join(parent, "o"), "--all"),
		attachmentDownloadArgs("--task-id", "12", "--attachment-id", "5", "--output", filepath.Join(parent, "o"), "--ids", "5"),
		// Output path rejections: all before config load and any request.
		attachmentDownloadArgs("--task-id", "12", "--attachment-id", "5", "--output", ""),
		attachmentDownloadArgs("--task-id", "12", "--attachment-id", "5", "--output", "."),
		attachmentDownloadArgs("--task-id", "12", "--attachment-id", "5", "--output", volumeRoot),
		attachmentDownloadArgs("--task-id", "12", "--attachment-id", "5", "--output", filepath.Join(parent, "sub")+string(filepath.Separator)), // trailing separator
		attachmentDownloadArgs("--task-id", "12", "--attachment-id", "5", "--output", existing),                                                // existing target
		attachmentDownloadArgs("--task-id", "12", "--attachment-id", "5", "--output", parent),                                                  // existing directory target
		attachmentDownloadArgs("--task-id", "12", "--attachment-id", "5", "--output", filepath.Join(parent, "missing", "o")),                   // missing parent
		attachmentDownloadArgs("--task-id", "12", "--attachment-id", "5", "--output", filepath.Join(existing, "o")),                            // parent is a file
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Errorf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
		}
	}
	if requests != 0 {
		t.Errorf("invalid arguments made %d HTTP requests, want 0", requests)
	}
}

func TestTaskAttachmentDownloadPreview(t *testing.T) {
	server, requests := newAttachmentDownloadServer(t, attachmentDownloadServerOptions{})
	defer server.Close()

	destination := tempOutputPath(t, "out.bin")
	code, stdout, stderr := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", destination)
	if code != 0 || stderr != "" {
		t.Fatalf("run = %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	var preview map[string]any
	if err := json.Unmarshal([]byte(stdout), &preview); err != nil {
		t.Fatalf("preview is not JSON: %v", err)
	}
	want := map[string]any{
		"mode":                  "preview",
		"operation":             "attachment_download",
		"task_id":               float64(12),
		"attachment_id":         float64(5),
		"expected_size":         float64(4),
		"maximum_bytes":         float64(100 << 20),
		"maximum_files_written": float64(1),
	}
	for key, value := range want {
		if !jsonEqual(preview[key], value) {
			t.Errorf("preview[%q] = %#v, want %#v", key, preview[key], value)
		}
	}
	if preview["destination_path"] != destination {
		t.Errorf("destination_path = %#v, want %q", preview["destination_path"], destination)
	}
	if _, present := preview["destination"]; present {
		t.Errorf("output must use destination_path, not destination: %#v", preview)
	}
	// Exactly one metadata GET; no byte GET; nothing created locally.
	if len(*requests) != 1 {
		t.Fatalf("requests = %#v, want exactly 1 GET", *requests)
	}
	got := (*requests)[0]
	if got.method != http.MethodGet || got.path != "/api/v2/tasks/12/attachments" || got.query != "page=1&per_page=1000" {
		t.Errorf("request = %s %s?%s, want GET /api/v2/tasks/12/attachments?page=1&per_page=1000", got.method, got.path, got.query)
	}
	if _, err := os.Lstat(destination); err == nil {
		t.Error("preview created the destination")
	}
	if countLeftoverTemps(t, destination) != 0 {
		t.Error("preview left temporary files")
	}
	for _, secret := range []string{attachmentDownloadToken, "Authorization", "server-name.bin", "secret", server.URL} {
		if strings.Contains(stdout, secret) {
			t.Errorf("preview leaked %q: %q", secret, stdout)
		}
	}
}

func TestTaskAttachmentDownloadPreviewAbsentAttachment(t *testing.T) {
	server, requests := newAttachmentDownloadServer(t, attachmentDownloadServerOptions{
		listBody: `{"items":[{"id":9,"size":1}],"page":1,"per_page":1000,"total":1,"total_pages":1}`,
	})
	defer server.Close()

	code, stdout, stderr := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", tempOutputPath(t, "out.bin"))
	if code != 1 || stdout != "" || !strings.Contains(stderr, "任务或附件不存在") {
		t.Fatalf("run = %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if len(*requests) != 1 {
		t.Errorf("requests = %#v, want exactly 1 metadata GET", *requests)
	}
}

func TestTaskAttachmentDownloadPreviewMalformedMetadata(t *testing.T) {
	for _, body := range []string{
		`{`,
		`{"items":[{"id":5},{"id":5}],"page":1,"per_page":1000,"total":2,"total_pages":1}`,         // duplicate ids
		`{"items":[{"id":5}],"page":1,"per_page":1000,"total":2,"total_pages":1}`,                  // incomplete envelope
		`{"items":[{"id":5,"size":"4"}],"page":1,"per_page":1000,"total":1,"total_pages":1}`,       // untyped size
		`{"items":[{"id":5,"size":-1}],"page":1,"per_page":1000,"total":1,"total_pages":1}`,        // negative size
		`{"items":[{"id":5,"size":104857601}],"page":1,"per_page":1000,"total":1,"total_pages":1}`, // over cap
		`{"items":[{"id":5,"size":4}],"page":2,"per_page":1000,"total":1,"total_pages":1}`,         // page mismatch
		`{"items":[{"id":5,"size":4}],"page":1,"per_page":50,"total":1,"total_pages":1}`,           // per_page mismatch
		`{"items":[{"id":5,"size":4}],"page":1,"per_page":1000,"total":1,"total_pages":2}`,         // total_pages mismatch
		`{"items":[{"id":5,"size":4}],"per_page":1000,"total":1,"total_pages":1}`,                  // missing page
	} {
		server, requests := newAttachmentDownloadServer(t, attachmentDownloadServerOptions{listBody: body})
		code, stdout, _ := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", tempOutputPath(t, "out.bin"))
		if code != 1 || stdout != "" {
			t.Errorf("body %s: run = %d, stdout %q", body, code, stdout)
		}
		for _, request := range *requests {
			if request.path != "/api/v2/tasks/12/attachments" {
				t.Errorf("body %s: unexpected byte request %s", body, request.path)
			}
		}
		server.Close()
	}
}

func TestTaskAttachmentDownloadMetadataOnly200Accepted(t *testing.T) {
	for _, status := range []int{http.StatusCreated, http.StatusAccepted, http.StatusNoContent} {
		server, requests := newAttachmentDownloadServer(t, attachmentDownloadServerOptions{listStatus: status})
		code, stdout, stderr := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", tempOutputPath(t, "out.bin"), "--apply")
		if code != 1 || stdout != "" || !strings.Contains(stderr, fmt.Sprintf("HTTP status %d", status)) {
			t.Errorf("status %d: run = %d, stdout %q, stderr %q", status, code, stdout, stderr)
		}
		if len(*requests) != 1 || (*requests)[0].path != "/api/v2/tasks/12/attachments" {
			t.Errorf("status %d: requests = %#v, want exactly one metadata GET", status, *requests)
		}
		server.Close()
	}
}

func TestTaskAttachmentDownloadApplySuccess(t *testing.T) {
	server, requests := newAttachmentDownloadServer(t, attachmentDownloadServerOptions{})
	defer server.Close()

	destination := tempOutputPath(t, "out.bin")
	code, stdout, stderr := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", destination, "--apply")
	if code != 0 || stderr != "" {
		t.Fatalf("run = %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	// Exactly strict metadata GET then byte GET.
	if len(*requests) != 2 {
		t.Fatalf("requests = %#v, want metadata GET + byte GET", *requests)
	}
	if got := (*requests)[0]; got.method != http.MethodGet || got.path != "/api/v2/tasks/12/attachments" || got.query != "page=1&per_page=1000" {
		t.Errorf("metadata request = %s %s?%s", got.method, got.path, got.query)
	}
	if got := (*requests)[1]; got.method != http.MethodGet || got.path != "/api/v2/tasks/12/attachments/5" {
		t.Errorf("byte request = %s %s", got.method, got.path)
	}

	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "data" {
		t.Fatalf("downloaded content = %q, err = %v", content, err)
	}
	sum := sha256.Sum256([]byte("data"))
	var output map[string]any
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("apply output is not JSON: %v", err)
	}
	want := map[string]any{
		"mode":                  "apply",
		"operation":             "attachment_download",
		"task_id":               float64(12),
		"attachment_id":         float64(5),
		"bytes_written":         float64(4),
		"sha256":                "sha256:" + hex.EncodeToString(sum[:]),
		"download_status":       float64(200),
		"local_readback":        "verified",
		"maximum_files_written": float64(1),
	}
	for key, value := range want {
		if !jsonEqual(output[key], value) {
			t.Errorf("output[%q] = %#v, want %#v", key, output[key], value)
		}
	}
	if output["destination_path"] != destination {
		t.Errorf("destination_path = %#v, want %q", output["destination_path"], destination)
	}
	if _, present := output["destination"]; present {
		t.Errorf("output must use destination_path, not destination: %#v", output)
	}
	if countLeftoverTemps(t, destination) != 0 {
		t.Error("success left temporary files")
	}
	for _, secret := range []string{attachmentDownloadToken, "Authorization", "server-name.bin", server.URL} {
		if strings.Contains(stdout, secret) {
			t.Errorf("output leaked %q: %q", secret, stdout)
		}
	}
}

func TestTaskAttachmentDownloadApplyContentLengthOverLimit(t *testing.T) {
	over := int64(100<<20) + 1
	server, requests := newAttachmentDownloadServer(t, attachmentDownloadServerOptions{dataContentLength: &over})
	defer server.Close()

	destination := tempOutputPath(t, "out.bin")
	code, stdout, stderr := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", destination, "--apply")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "100 MiB") {
		t.Fatalf("run = %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if _, err := os.Lstat(destination); err == nil {
		t.Error("destination created")
	}
	if countLeftoverTemps(t, destination) != 0 {
		t.Error("leftover temporary files")
	}
	if len(*requests) != 2 {
		t.Errorf("requests = %#v, want metadata GET + byte GET, no retry", *requests)
	}
	for _, secret := range []string{attachmentDownloadToken, "Authorization", server.URL} {
		if strings.Contains(stderr, secret) {
			t.Errorf("stderr leaked %q: %q", secret, stderr)
		}
	}
}

func TestTaskAttachmentDownloadApplyMetadataSizeMismatch(t *testing.T) {
	server, requests := newAttachmentDownloadServer(t, attachmentDownloadServerOptions{
		listBody: `{"items":[{"id":5,"size":99}],"page":1,"per_page":1000,"total":1,"total_pages":1}`,
	})
	defer server.Close()

	destination := tempOutputPath(t, "out.bin")
	code, stdout, stderr := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", destination, "--apply")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "不一致") {
		t.Fatalf("run = %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if _, err := os.Lstat(destination); err == nil {
		t.Error("destination created")
	}
	if countLeftoverTemps(t, destination) != 0 {
		t.Error("leftover temporary files")
	}
	if len(*requests) != 2 {
		t.Errorf("requests = %#v, want metadata GET + byte GET", *requests)
	}
}

// Unknown Content-Length (chunked): received bytes must still exactly equal
// the typed-present metadata size.
func TestTaskAttachmentDownloadApplyUnknownLengthMatchesMetadata(t *testing.T) {
	server, requests := newAttachmentDownloadServer(t, attachmentDownloadServerOptions{
		chunkedWriter: func(w io.Writer) { _, _ = w.Write([]byte("data")) },
	})
	defer server.Close()

	destination := tempOutputPath(t, "out.bin")
	code, stdout, stderr := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", destination, "--apply")
	if code != 0 || stderr != "" {
		t.Fatalf("run = %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "data" {
		t.Fatalf("downloaded content = %q, err = %v", content, err)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if !jsonEqual(output["bytes_written"], float64(4)) {
		t.Errorf("bytes_written = %#v", output["bytes_written"])
	}
	if len(*requests) != 2 {
		t.Errorf("requests = %#v, want metadata GET + byte GET", *requests)
	}
}

func TestTaskAttachmentDownloadApplyUnknownLengthMismatchesMetadata(t *testing.T) {
	server, requests := newAttachmentDownloadServer(t, attachmentDownloadServerOptions{
		chunkedWriter: func(w io.Writer) { _, _ = w.Write([]byte("only3b")) },
	})
	defer server.Close()

	destination := tempOutputPath(t, "out.bin")
	code, stdout, stderr := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", destination, "--apply")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "不一致") {
		t.Fatalf("run = %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if _, err := os.Lstat(destination); err == nil {
		t.Error("destination created")
	}
	if countLeftoverTemps(t, destination) != 0 {
		t.Error("leftover temporary files")
	}
	if len(*requests) != 2 {
		t.Errorf("requests = %#v, want metadata GET + byte GET", *requests)
	}
}

func TestTaskAttachmentDownloadPreInstallFailuresReportCleanupFailure(t *testing.T) {
	t.Run("streamed metadata mismatch", func(t *testing.T) {
		server, _ := newAttachmentDownloadServer(t, attachmentDownloadServerOptions{
			chunkedWriter: func(w io.Writer) { _, _ = w.Write([]byte("only3b")) },
		})
		defer server.Close()
		originalRemove := removeFile
		removeFile = func(string) error { return errors.New("simulated cleanup failure") }
		defer func() { removeFile = originalRemove }()

		destination := tempOutputPath(t, "out.bin")
		code, stdout, stderr := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", destination, "--apply")
		if code != 1 || stdout != "" || !strings.Contains(stderr, "临时文件清理失败") || strings.Contains(stderr, "已清理临时文件") {
			t.Fatalf("run = %d, stdout %q, stderr %q", code, stdout, stderr)
		}
		if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("destination exists or could not be checked: %v", err)
		}
		for _, secret := range []string{attachmentDownloadToken, "Authorization", server.URL, "only3b"} {
			if strings.Contains(stderr, secret) {
				t.Errorf("stderr leaked %q: %q", secret, stderr)
			}
		}
		// The forced failure intentionally leaves the random temp; remove it
		// after restoring the seam so TempDir cleanup is not relied upon.
		removeFile = originalRemove
		for _, entry := range mustReadDir(t, filepath.Dir(destination)) {
			if strings.HasPrefix(entry.Name(), ".vikunja-ops-download-") {
				_ = os.Remove(filepath.Join(filepath.Dir(destination), entry.Name()))
			}
		}
	})

	t.Run("pre-install revalidation", func(t *testing.T) {
		server, _ := newAttachmentDownloadServer(t, attachmentDownloadServerOptions{})
		defer server.Close()
		originalCheck, originalRemove := checkDownloadDestinationSafe, removeFile
		checks := 0
		checkDownloadDestinationSafe = func(destination string) error {
			checks++
			if checks == 2 {
				return errors.New("simulated pre-install validation failure")
			}
			return originalCheck(destination)
		}
		removeFile = func(string) error { return errors.New("simulated cleanup failure") }
		defer func() { checkDownloadDestinationSafe, removeFile = originalCheck, originalRemove }()

		destination := tempOutputPath(t, "out.bin")
		code, stdout, stderr := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", destination, "--apply")
		if code != 1 || stdout != "" || !strings.Contains(stderr, "临时文件清理失败") || strings.Contains(stderr, "已清理临时文件") {
			t.Fatalf("run = %d, stdout %q, stderr %q", code, stdout, stderr)
		}
		if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("destination exists or could not be checked: %v", err)
		}
		if checks != 2 {
			t.Errorf("safety checks = %d, want pre-transfer + pre-install", checks)
		}
	})
}

func mustReadDir(t *testing.T, path string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// Exactly 100 MiB at the hard cap must pass.
func TestTaskAttachmentDownloadApplyExactlyAtLimit(t *testing.T) {
	const size = int64(100 << 20)
	server, _ := newAttachmentDownloadServer(t, attachmentDownloadServerOptions{
		listBody:          `{"items":[{"id":5,"size":104857600}],"page":1,"per_page":1000,"total":1,"total_pages":1}`,
		dataContentLength: func() *int64 { v := size; return &v }(),
		chunkedWriter:     nil,
		data:              make([]byte, size),
	})
	defer server.Close()

	destination := tempOutputPath(t, "out.bin")
	code, stdout, stderr := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", destination, "--apply")
	if code != 0 || stderr != "" {
		t.Fatalf("run = %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	info, err := os.Lstat(destination)
	if err != nil || info.Size() != size {
		t.Fatalf("destination size = %v, err = %v, want %d", info, err, size)
	}
	sum := sha256.Sum256(make([]byte, size))
	var output map[string]any
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if !jsonEqual(output["sha256"], "sha256:"+hex.EncodeToString(sum[:])) {
		t.Errorf("sha256 = %#v", output["sha256"])
	}
	if !jsonEqual(output["bytes_written"], float64(size)) {
		t.Errorf("bytes_written = %#v", output["bytes_written"])
	}
	if countLeftoverTemps(t, destination) != 0 {
		t.Error("leftover temporary files")
	}
}

// More than 100 MiB actually streamed (unknown length) must fail with
// cleanup even when metadata carries no size.
func TestTaskAttachmentDownloadApplyStreamedOverLimit(t *testing.T) {
	chunk := make([]byte, 1<<20)
	server, _ := newAttachmentDownloadServer(t, attachmentDownloadServerOptions{
		listBody: `{"items":[{"id":5}],"page":1,"per_page":1000,"total":1,"total_pages":1}`,
		chunkedWriter: func(w io.Writer) {
			for i := 0; i <= 100; i++ { // 101 MiB > 100 MiB cap
				_, _ = w.Write(chunk)
			}
		},
	})
	defer server.Close()

	destination := tempOutputPath(t, "out.bin")
	code, stdout, stderr := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", destination, "--apply")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "100 MiB") {
		t.Fatalf("run = %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if _, err := os.Lstat(destination); err == nil {
		t.Error("destination created")
	}
	if countLeftoverTemps(t, destination) != 0 {
		t.Error("over-limit transfer left temporary files")
	}
}

func TestTaskAttachmentDownloadApplyInterruptedTransfer(t *testing.T) {
	four := int64(4)
	server, _ := newAttachmentDownloadServer(t, attachmentDownloadServerOptions{
		dataContentLength:    &four,
		abortDataMidTransfer: true,
	})
	defer server.Close()

	destination := tempOutputPath(t, "out.bin")
	code, stdout, stderr := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", destination, "--apply")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "传输未完成") {
		t.Fatalf("run = %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if _, err := os.Lstat(destination); err == nil {
		t.Error("destination created after interrupted transfer")
	}
	if countLeftoverTemps(t, destination) != 0 {
		t.Error("interrupted transfer left temporary files")
	}
	for _, secret := range []string{attachmentDownloadToken, "Authorization", server.URL} {
		if strings.Contains(stderr, secret) {
			t.Errorf("stderr leaked %q: %q", secret, stderr)
		}
	}
}

func TestTaskAttachmentDownloadApplyStatusMatrixNoRetry(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusForbidden, "权限不足"},
		{http.StatusNotFound, "任务或附件不存在"},
		{http.StatusNoContent, "HTTP status 204"},
		{http.StatusInternalServerError, "HTTP status 500"},
	}
	for _, tc := range cases {
		server, requests := newAttachmentDownloadServer(t, attachmentDownloadServerOptions{dataStatus: tc.status})
		destination := tempOutputPath(t, "out.bin")
		code, stdout, stderr := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", destination, "--apply")
		if code != 1 || stdout != "" || !strings.Contains(stderr, tc.want) {
			t.Errorf("status %d: run = %d, stdout %q, stderr %q, want %q", tc.status, code, stdout, stderr, tc.want)
		}
		if len(*requests) != 2 {
			t.Errorf("status %d: requests = %#v, want exactly metadata GET + one byte GET, no retry", tc.status, *requests)
		}
		if _, err := os.Lstat(destination); err == nil {
			t.Errorf("status %d: destination created", tc.status)
		}
		if countLeftoverTemps(t, destination) != 0 {
			t.Errorf("status %d: leftover temporary files", tc.status)
		}
		for _, secret := range []string{attachmentDownloadToken, "Authorization", "server detail", server.URL} {
			if strings.Contains(stderr, secret) {
				t.Errorf("status %d: stderr leaked %q: %q", tc.status, secret, stderr)
			}
		}
		server.Close()
	}
}

func TestTaskAttachmentDownloadListFailureSendsNoByteRequest(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError} {
		server, requests := newAttachmentDownloadServer(t, attachmentDownloadServerOptions{listStatus: status})
		code, stdout, stderr := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", tempOutputPath(t, "out.bin"), "--apply")
		if code != 1 || stdout != "" {
			t.Errorf("list %d: run = %d, stdout %q, stderr %q", status, code, stdout, stderr)
		}
		if len(*requests) != 1 || (*requests)[0].path != "/api/v2/tasks/12/attachments" {
			t.Errorf("list %d: requests = %#v, want exactly one metadata GET", status, *requests)
		}
		for _, secret := range []string{attachmentDownloadToken, "Authorization", "server detail"} {
			if strings.Contains(stderr, secret) {
				t.Errorf("list %d: stderr leaked %q: %q", status, secret, stderr)
			}
		}
		server.Close()
	}
}

func TestTaskAttachmentDownloadApplyRedirectRefused(t *testing.T) {
	targetRequests := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests++
		if r.Header.Get("Authorization") != "" {
			t.Errorf("redirect target received Authorization header")
		}
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+attachmentDownloadToken {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/v2/tasks/12/attachments":
			_, _ = w.Write([]byte(attachmentDownloadListBody))
		case "/api/v2/tasks/12/attachments/5":
			http.Redirect(w, r, target.URL+"/stolen?token="+attachmentDownloadToken, http.StatusFound)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	destination := tempOutputPath(t, "out.bin")
	code, stdout, stderr := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", destination, "--apply")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "HTTP status 302") {
		t.Fatalf("run = %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if targetRequests != 0 {
		t.Errorf("redirect target received %d requests, want 0 (no token forwarding)", targetRequests)
	}
	if _, err := os.Lstat(destination); err == nil {
		t.Error("destination created")
	}
	for _, secret := range []string{attachmentDownloadToken, "Authorization", target.URL} {
		if strings.Contains(stderr, secret) {
			t.Errorf("stderr leaked %q: %q", secret, stderr)
		}
	}
}

func TestTaskAttachmentDownloadApplyTargetRejectedPreNetwork(t *testing.T) {
	server, requests := newAttachmentDownloadServer(t, attachmentDownloadServerOptions{})
	defer server.Close()

	parent := t.TempDir()
	destination := filepath.Join(parent, "out.bin")
	if err := os.WriteFile(destination, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", destination, "--apply")
	if code != 2 || stdout != "" {
		t.Fatalf("run = %d, stdout %q", code, stdout)
	}
	content, _ := os.ReadFile(destination)
	if string(content) != "keep" {
		t.Errorf("existing destination was modified: %q", content)
	}
	if len(*requests) != 0 {
		t.Errorf("requests = %#v, want 0 (rejected pre-network)", *requests)
	}
}

// installWithoutOverwrite must never replace a destination that appears
// after initial validation: linkFile fails and the destination is unchanged.
func TestInstallWithoutOverwriteNeverReplacesExistingDestination(t *testing.T) {
	parent := t.TempDir()
	temp := filepath.Join(parent, ".vikunja-ops-download-test.tmp")
	if err := os.WriteFile(temp, []byte("new-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(parent, "out.bin")
	if err := os.WriteFile(destination, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	installed, err := installWithoutOverwrite(temp, destination)
	if installed || err == nil {
		t.Fatalf("installed = %v, err = %v, want not installed with error", installed, err)
	}
	content, _ := os.ReadFile(destination)
	if string(content) != "keep" {
		t.Errorf("destination was replaced: %q", content)
	}
	_ = os.Remove(temp)
}

// A successful install followed by a failed temporary removal must report
// the file as installed, never as "nothing written".
func TestInstallWithoutOverwriteDistinguishesRemoveFailure(t *testing.T) {
	parent := t.TempDir()
	temp := filepath.Join(parent, ".vikunja-ops-download-test.tmp")
	if err := os.WriteFile(temp, []byte("new-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(parent, "out.bin")
	original := removeFile
	removeFile = func(string) error { return errors.New("simulated remove failure") }
	defer func() { removeFile = original }()

	installed, err := installWithoutOverwrite(temp, destination)
	if !installed || err == nil {
		t.Fatalf("installed = %v, err = %v, want installed with removal error", installed, err)
	}
	content, _ := os.ReadFile(destination)
	if string(content) != "new-bytes" {
		t.Errorf("destination content = %q, want installed bytes", content)
	}
	_ = os.Remove(temp)
}

// End-to-end: when temporary removal fails after a successful install, the
// CLI reports the file as written (typed success output) plus a cleanup
// warning, and never claims nothing was installed.
func TestTaskAttachmentDownloadApplyInstallRemoveFailure(t *testing.T) {
	server, _ := newAttachmentDownloadServer(t, attachmentDownloadServerOptions{})
	defer server.Close()

	original := removeFile
	calls := 0
	removeFile = func(name string) error {
		calls++
		if calls == 1 {
			return errors.New("simulated remove failure")
		}
		return original(name)
	}
	defer func() { removeFile = original }()

	destination := tempOutputPath(t, "out.bin")
	code, stdout, stderr := runAttachmentDownload(t, server, "--task-id", "12", "--attachment-id", "5", "--output", destination, "--apply")
	if code != 0 {
		t.Fatalf("run = %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "临时文件清理失败") || !strings.Contains(stderr, "目标文件已写入") {
		t.Errorf("stderr %q must state the file WAS written and cleanup failed", stderr)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if output["destination_path"] != destination || !jsonEqual(output["local_readback"], "verified") {
		t.Errorf("output = %#v", output)
	}
	content, _ := os.ReadFile(destination)
	if string(content) != "data" {
		t.Errorf("destination content = %q", content)
	}
}

// The centralized revalidation rejects a destination that exists when it
// runs, covering the pre-transfer and pre-install repetitions.
func TestCheckDestinationAbsentAndParentSafeCentralized(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "out.bin")
	if err := checkDestinationAbsentAndParentSafe(destination); err != nil {
		t.Fatalf("absent destination rejected: %v", err)
	}
	if err := os.WriteFile(destination, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkDestinationAbsentAndParentSafe(destination); err == nil {
		t.Error("existing destination accepted")
	}
	missing := filepath.Join(parent, "missing", "out.bin")
	if err := checkDestinationAbsentAndParentSafe(missing); err == nil {
		t.Error("missing parent accepted")
	}
}
