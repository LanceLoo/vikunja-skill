package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vikunja-opencode-skill/internal/client"
)

func uploadArgs(args ...string) []string {
	return append([]string{"tasks", "attachments", "upload"}, args...)
}

func TestAttachmentUploadValidationDoesNotRequest(t *testing.T) {
	requests := 0
	s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer s.Close()
	t.Setenv("VIKUNJA_URL", s.URL)
	t.Setenv("VIKUNJA_TOKEN", "upload-secret")
	file := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(file, []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--task-id", "0", "--file", file},
		{"--task-id", "1", "--file", file, "--file", file},
		{"--task-id", "1", "--file", file, "--confirm", "sha256:" + strings.Repeat("a", 64)},
		{"--task-id", "1", "--file", filepath.Join(t.TempDir(), "missing")},
		{"--task-id", "1", "--file", t.TempDir()},
	} {
		var out, err bytes.Buffer
		if code := run(uploadArgs(args...), &out, &err); code != 2 {
			t.Fatalf("%v: code=%d stderr=%s", args, code, err.String())
		}
	}
	if requests != 0 {
		t.Fatalf("requests=%d", requests)
	}
}

func TestAttachmentUploadSourceSafetyAndSizeBounds(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.txt")
	if err := os.WriteFile(good, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	tooLarge := filepath.Join(dir, "too-large.bin")
	f, err := os.Create(tooLarge)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(client.MaxAttachmentUploadBytes + 1); err != nil {
		f.Close()
		t.Skipf("sparse truncate unavailable: %v", err)
	}
	_ = f.Close()
	if _, _, err := inspectAttachmentUploadSource(tooLarge); err == nil {
		t.Fatal("oversize source accepted")
	}
	if m, _, err := inspectAttachmentUploadSource(good); err != nil || m.size != 1 {
		t.Fatalf("valid source rejected: %#v %v", m, err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(good, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, _, err := inspectAttachmentUploadSource(link); err == nil {
		t.Fatal("symlink source accepted")
	}
}

func TestAttachmentUploadPreviewAndApply(t *testing.T) {
	file := filepath.Join(t.TempDir(), "proof.txt")
	payload := []byte("safe payload")
	if err := os.WriteFile(file, payload, 0600); err != nil {
		t.Fatal(err)
	}
	var requests []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch len(requests) {
		case 1, 2:
			if r.Method != http.MethodGet {
				t.Fatalf("preflight %s", r.Method)
			}
			_, _ = io.WriteString(w, `{"id":12,"project_id":1}`)
			if len(requests) == 2 {
				if err := os.WriteFile(file, []byte("changed after private capture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
		case 3:
			if r.Method != http.MethodPost {
				t.Fatalf("post %s", r.Method)
			}
			mr, e := r.MultipartReader()
			if e != nil {
				t.Fatal(e)
			}
			p, e := mr.NextPart()
			if e != nil {
				t.Fatal(e)
			}
			if p.FormName() != "files" || p.FileName() != "proof.txt" {
				t.Fatalf("part %q %q", p.FormName(), p.FileName())
			}
			got, e := io.ReadAll(p)
			if e != nil || !bytes.Equal(got, payload) {
				t.Fatalf("payload=%q err=%v", got, e)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"errors":[],"success":[{"id":9,"task_id":12,"file":{"name":"proof.txt","size":12}}]}`)
		case 4:
			if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("per_page") != "1000" {
				t.Fatal("pagination")
			}
			_, _ = io.WriteString(w, `{"items":[{"id":9,"task_id":12,"file":{"name":"proof.txt","size":12}}],"page":1,"per_page":1000,"total":1,"total_pages":1}`)
		default:
			t.Fatalf("extra request")
		}
	}))
	defer s.Close()
	t.Setenv("VIKUNJA_URL", s.URL)
	t.Setenv("VIKUNJA_TOKEN", "upload-secret")
	var previewOut, previewErr bytes.Buffer
	if code := run(uploadArgs("--task-id", "12", "--file", file), &previewOut, &previewErr); code != 0 {
		t.Fatalf("preview %d %s", code, previewErr.String())
	}
	var preview map[string]any
	if json.Unmarshal(previewOut.Bytes(), &preview) != nil {
		t.Fatal("invalid preview JSON")
	}
	token, _ := preview["confirmation_token"].(string)
	if preview["filename"] != "proof.txt" || strings.Contains(previewOut.String(), file) || strings.Contains(previewOut.String(), "upload-secret") {
		t.Fatalf("unsafe preview: %s", previewOut.String())
	}
	var applyOut, applyErr bytes.Buffer
	if code := run(uploadArgs("--task-id", "12", "--file", file, "--apply", "--confirm", token), &applyOut, &applyErr); code != 0 {
		t.Fatalf("apply %d %s", code, applyErr.String())
	}
	if !strings.Contains(applyOut.String(), `"remote_readback":"verified"`) || strings.Contains(applyOut.String(), file) {
		t.Fatalf("unsafe apply: %s", applyOut.String())
	}
	if len(requests) != 4 {
		t.Fatalf("requests=%v", requests)
	}
}

func TestAttachmentUploadEmptyAndPostFailureNoReadback(t *testing.T) {
	file := filepath.Join(t.TempDir(), "empty.txt")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	requests := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			_, _ = io.WriteString(w, `{"id":7,"project_id":1}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer s.Close()
	t.Setenv("VIKUNJA_URL", s.URL)
	t.Setenv("VIKUNJA_TOKEN", "upload-secret")
	var pOut, pErr bytes.Buffer
	if code := run(uploadArgs("--task-id", "7", "--file", file), &pOut, &pErr); code != 0 {
		t.Fatalf("preview: %d %s", code, pErr.String())
	}
	var p map[string]any
	if err := json.Unmarshal(pOut.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	var aOut, aErr bytes.Buffer
	if code := run(uploadArgs("--task-id", "7", "--file", file, "--apply", "--confirm", p["confirmation_token"].(string)), &aOut, &aErr); code != 1 {
		t.Fatalf("apply=%d %s", code, aErr.String())
	}
	if requests != 2 || aOut.Len() != 0 || strings.Contains(aErr.String(), "upload-secret") || strings.Contains(aErr.String(), s.URL) {
		t.Fatalf("requests=%d output=%q err=%q", requests, aOut.String(), aErr.String())
	}
}

func TestAttachmentUploadChangeBeforeSnapshotDoesNotLoadConfigOrRequest(t *testing.T) {
	file := filepath.Join(t.TempDir(), "change.txt")
	if err := os.WriteFile(file, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	m, clean, err := inspectAttachmentUploadSource(file)
	if err != nil {
		t.Fatal(err)
	}
	token, err := attachmentUploadToken(3, m)
	if err != nil {
		t.Fatal(err)
	}
	original := captureAttachmentUpload
	defer func() { captureAttachmentUpload = original }()
	captureAttachmentUpload = func(path string, manifest attachmentUploadManifest) ([]byte, error) {
		_ = os.WriteFile(path, []byte("after"), 0600)
		return original(path, manifest)
	}
	t.Setenv("VIKUNJA_URL", "")
	t.Setenv("VIKUNJA_TOKEN", "")
	var out, stderr bytes.Buffer
	if code := run(uploadArgs("--task-id", "3", "--file", clean, "--apply", "--confirm", token), &out, &stderr); code != 1 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if out.Len() != 0 || strings.Contains(stderr.String(), clean) {
		t.Fatalf("output leak: %q %q", out.String(), stderr.String())
	}
}

func TestAttachmentUploadNullErrorsResponseIsReadBackAndVerified(t *testing.T) {
	file := filepath.Join(t.TempDir(), "null-errors.txt")
	payload := []byte("test")
	if err := os.WriteFile(file, payload, 0600); err != nil {
		t.Fatal(err)
	}
	m, _, err := inspectAttachmentUploadSource(file)
	if err != nil {
		t.Fatal(err)
	}
	token, err := attachmentUploadToken(22, m)
	if err != nil {
		t.Fatal(err)
	}
	var requests []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch len(requests) {
		case 1:
			if r.Method != http.MethodGet || r.URL.Path != "/api/v2/tasks/22" {
				t.Fatalf("preflight: %s %s", r.Method, r.URL.Path)
			}
			_, _ = io.WriteString(w, `{"id":22,"project_id":1}`)
		case 2:
			if r.Method != http.MethodPost {
				t.Fatalf("post: %s", r.Method)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"errors":null,"success":[{"id":31,"task_id":22,"file":{"name":"null-errors.txt","size":4}}]}`)
		case 3:
			if r.Method != http.MethodGet || r.URL.Path != "/api/v2/tasks/22/attachments" {
				t.Fatalf("readback: %s %s", r.Method, r.URL.Path)
			}
			_, _ = io.WriteString(w, `{"items":[{"id":31,"task_id":22,"file":{"name":"null-errors.txt","size":4}}],"page":1,"per_page":1000,"total":1,"total_pages":1}`)
		default:
			t.Fatalf("unexpected request %d", len(requests))
		}
	}))
	defer s.Close()
	t.Setenv("VIKUNJA_URL", s.URL)
	t.Setenv("VIKUNJA_TOKEN", "upload-secret")
	var out, stderr bytes.Buffer
	if code := run(uploadArgs("--task-id", "22", "--file", file, "--apply", "--confirm", token), &out, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if got, want := strings.Join(requests, ","), "GET /api/v2/tasks/22,POST /api/v2/tasks/22/attachments,GET /api/v2/tasks/22/attachments"; got != want {
		t.Fatalf("requests=%s want=%s", got, want)
	}
	if !strings.Contains(out.String(), `"remote_readback":"verified"`) || stderr.Len() != 0 {
		t.Fatalf("out=%s stderr=%s", out.String(), stderr.String())
	}
}

func TestAttachmentUploadPost201InvalidResponseSkipsReadback(t *testing.T) {
	file := filepath.Join(t.TempDir(), "bad-response.txt")
	if err := os.WriteFile(file, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	m, _, err := inspectAttachmentUploadSource(file)
	if err != nil {
		t.Fatal(err)
	}
	token, err := attachmentUploadToken(23, m)
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch requests {
		case 1:
			_, _ = io.WriteString(w, `{"id":23,"project_id":1}`)
		case 2:
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"errors":[],"success":[]}`)
		default:
			t.Fatalf("readback must not occur")
		}
	}))
	defer s.Close()
	t.Setenv("VIKUNJA_URL", s.URL)
	t.Setenv("VIKUNJA_TOKEN", "upload-secret")
	var out, stderr bytes.Buffer
	if code := run(uploadArgs("--task-id", "23", "--file", file, "--apply", "--confirm", token), &out, &stderr); code != 1 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if requests != 2 || out.Len() != 0 || !strings.Contains(stderr.String(), "服务端已确认 HTTP 201，但响应未验证；附件可能已创建，请勿盲目重试") {
		t.Fatalf("requests=%d out=%q stderr=%q", requests, out.String(), stderr.String())
	}
}
