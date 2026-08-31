package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func attachmentDeleteArgs(args ...string) []string {
	return append([]string{"tasks", "attachments", "delete"}, args...)
}
func attachmentPage(id int64) string {
	if id == 0 {
		return `{"items":[],"page":1,"per_page":1000,"total":0,"total_pages":0}`
	}
	return `{"items":[{"id":` + stringInt(id) + `,"task_id":8,"file":{"name":"private-name.txt","size":1}}],"page":1,"per_page":1000,"total":1,"total_pages":1}`
}
func stringInt(v int64) string { b, _ := json.Marshal(v); return string(b) }

func TestAttachmentDeleteHelpAndLocalGates(t *testing.T) {
	var out, err bytes.Buffer
	if code := run(attachmentDeleteArgs("--help"), &out, &err); code != 0 || !strings.Contains(out.String(), "--attachment-id") {
		t.Fatalf("help %d %q", code, out.String())
	}
	requests := 0
	s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer s.Close()
	configureTest(t, s.URL, "secret")
	for _, a := range [][]string{{}, {"--task-id", "8", "--attachment-id", "0"}, {"--task-id", "8", "--attachment-id", "2", "--confirm", "sha256:" + strings.Repeat("a", 64)}, {"--task-id", "8", "--attachment-id", "2", "--apply", "--confirm", "bad"}, {"--task-id", "8", "--attachment-id", "2", "--all"}} {
		out.Reset()
		err.Reset()
		if code := run(attachmentDeleteArgs(a...), &out, &err); code != 2 {
			t.Fatalf("%v code=%d", a, code)
		}
	}
	if requests != 0 {
		t.Fatalf("requests=%d", requests)
	}
}

func TestAttachmentDeletePreviewAndHappyApply(t *testing.T) {
	var requests []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch len(requests) {
		case 1:
			_, _ = io.WriteString(w, attachmentPage(4))
		case 2:
			if r.Method != http.MethodGet {
				t.Fatal("apply preflight")
			}
			_, _ = io.WriteString(w, attachmentPage(4))
		case 3:
			if r.Method != http.MethodDelete || r.ContentLength != 0 {
				t.Fatalf("delete %s length %d", r.Method, r.ContentLength)
			}
			w.WriteHeader(http.StatusNoContent)
		case 4:
			_, _ = io.WriteString(w, attachmentPage(0))
		default:
			t.Fatal("extra request")
		}
	}))
	defer s.Close()
	configureTest(t, s.URL, "secret")
	var pOut, pErr bytes.Buffer
	if code := run(attachmentDeleteArgs("--task-id", "8", "--attachment-id", "4"), &pOut, &pErr); code != 0 {
		t.Fatalf("preview %d %s", code, pErr.String())
	}
	if strings.Contains(pOut.String(), "private-name") || strings.Contains(pOut.String(), "secret") {
		t.Fatal("preview leak")
	}
	var p map[string]any
	if err := json.Unmarshal(pOut.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	var aOut, aErr bytes.Buffer
	if code := run(attachmentDeleteArgs("--task-id", "8", "--attachment-id", "4", "--apply", "--confirm", p["confirmation_token"].(string)), &aOut, &aErr); code != 0 {
		t.Fatalf("apply %d %s", code, aErr.String())
	}
	want := "GET /api/v2/tasks/8/attachments,GET /api/v2/tasks/8/attachments,DELETE /api/v2/tasks/8/attachments/4,GET /api/v2/tasks/8/attachments"
	if got := strings.Join(requests, ","); got != want {
		t.Fatalf("got %s", got)
	}
	if !strings.Contains(aOut.String(), `"readback":"absent"`) || strings.Contains(aOut.String(), "已删除") {
		t.Fatalf("output %s", aOut.String())
	}
}

func TestAttachmentDeletePreflightNotFoundDoesNotDelete(t *testing.T) {
	n := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { n++; w.WriteHeader(http.StatusNotFound) }))
	defer s.Close()
	configureTest(t, s.URL, "secret")
	var out, err bytes.Buffer
	if code := run(attachmentDeleteArgs("--task-id", "8", "--attachment-id", "4"), &out, &err); code != 1 || n != 1 || !strings.Contains(err.String(), "任务或附件不存在") {
		t.Fatalf("code=%d n=%d err=%s", code, n, err.String())
	}
}

func TestAttachmentDeleteFailureReconcilesWithoutSuccessClaim(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			n := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n++
				switch n {
				case 1:
					_, _ = io.WriteString(w, attachmentPage(4))
				case 2:
					w.WriteHeader(status)
				case 3:
					_, _ = io.WriteString(w, attachmentPage(0))
				default:
					t.Fatal("retry")
				}
			}))
			defer s.Close()
			configureTest(t, s.URL, "secret")
			token, _ := attachmentDeleteConfirmationToken(8, 4)
			var out, err bytes.Buffer
			if code := run(attachmentDeleteArgs("--task-id", "8", "--attachment-id", "4", "--apply", "--confirm", token), &out, &err); code != 1 || n != 3 || out.Len() != 0 || !strings.Contains(err.String(), "不能据此推断") {
				t.Fatalf("code=%d n=%d out=%q err=%q", code, n, out.String(), err.String())
			}
		})
	}
}

func TestAttachmentDeletePost204PresentAndReadbackError(t *testing.T) {
	for _, present := range []bool{true, false} {
		t.Run("case", func(t *testing.T) {
			n := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n++
				if n == 1 {
					_, _ = io.WriteString(w, attachmentPage(4))
					return
				}
				if n == 2 {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if present {
					_, _ = io.WriteString(w, attachmentPage(4))
				} else {
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			defer s.Close()
			configureTest(t, s.URL, "secret")
			token, _ := attachmentDeleteConfirmationToken(8, 4)
			var out, err bytes.Buffer
			if code := run(attachmentDeleteArgs("--task-id", "8", "--attachment-id", "4", "--apply", "--confirm", token), &out, &err); code != 1 || n != 3 || out.Len() != 0 || !strings.Contains(err.String(), "HTTP 204") {
				t.Fatalf("code=%d n=%d out=%q err=%q", code, n, out.String(), err.String())
			}
		})
	}
}

func TestAttachmentDeleteTokenIsStableAndIDBound(t *testing.T) {
	a, err := attachmentDeleteConfirmationToken(8, 4)
	if err != nil || !validTaskDeleteConfirmationFormat(a) {
		t.Fatalf("token=%q err=%v", a, err)
	}
	b, _ := attachmentDeleteConfirmationToken(8, 5)
	c, _ := attachmentDeleteConfirmationToken(9, 4)
	if a == b || a == c {
		t.Fatalf("token not ID-bound: %q %q %q", a, b, c)
	}
}

func TestAttachmentDeleteTransportFailureReconcilesWithoutRetry(t *testing.T) {
	n := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {
		case 1:
			_, _ = io.WriteString(w, attachmentPage(4))
		case 2:
			// Closing the connection makes the DELETE a transport uncertainty;
			// the next request must be the single reconciliation scan.
			if h, ok := w.(http.Hijacker); ok {
				c, _, e := h.Hijack()
				if e == nil {
					_ = c.Close()
				}
			}
		case 3:
			_, _ = io.WriteString(w, attachmentPage(0))
		default:
			t.Fatal("unexpected retry")
		}
	}))
	defer s.Close()
	configureTest(t, s.URL, "secret")
	token, _ := attachmentDeleteConfirmationToken(8, 4)
	var out, stderr bytes.Buffer
	if code := run(attachmentDeleteArgs("--task-id", "8", "--attachment-id", "4", "--apply", "--confirm", token), &out, &stderr); code != 1 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if n != 3 || out.Len() != 0 || !strings.Contains(stderr.String(), "删除请求结果可能未知") || !strings.Contains(stderr.String(), "不能据此推断") {
		t.Fatalf("n=%d out=%q stderr=%q", n, out.String(), stderr.String())
	}
}
