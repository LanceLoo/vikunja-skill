package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

const attachmentDeleteToken = "delete-secret-token"

func deletePage(page, total, pages int, items string) string {
	return `{"items":` + items + `,"page":` + strconv.Itoa(page) + `,"per_page":1000,"total":` + strconv.Itoa(total) + `,"total_pages":` + strconv.Itoa(pages) + `}`
}
func deleteItem(id int64) string {
	return `{"id":` + strconv.FormatInt(id, 10) + `,"task_id":12,"file":{"name":"name.bin","size":4}}`
}

func TestDeleteTaskAttachmentRequestValidationAndStatuses(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v2/tasks/12/attachments/5" || r.Header.Get("Authorization") != "Bearer "+attachmentDeleteToken || r.Header.Get("Content-Type") != "" {
			t.Errorf("unexpected delete request")
		}
		body, _ := io.ReadAll(r.Body)
		if len(body) != 0 {
			t.Errorf("body = %q", body)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	for _, ids := range [][2]int64{{0, 5}, {12, 0}} {
		if err := New(server.Client()).DeleteTaskAttachment(context.Background(), server.URL, attachmentDeleteToken, ids[0], ids[1]); err == nil {
			t.Error("invalid delete succeeded")
		}
	}
	if requests.Load() != 0 {
		t.Error("validation made request")
	}
	if err := New(server.Client()).DeleteTaskAttachment(context.Background(), server.URL, attachmentDeleteToken, 12, 5); err != nil {
		t.Fatal(err)
	}
	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError} {
		statusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, attachmentDeleteToken+" body")
		}))
		err := New(statusServer.Client()).DeleteTaskAttachment(context.Background(), statusServer.URL, attachmentDeleteToken, 12, 5)
		statusServer.Close()
		if err == nil || strings.Contains(err.Error(), attachmentDeleteToken) {
			t.Errorf("status %d: err = %v", status, err)
		}
		if status == http.StatusNotFound && !errors.Is(err, ErrNotFound) {
			t.Errorf("404 = %v", err)
		}
	}
}

func TestDeleteTaskAttachmentRedirectDoesNotForwardToken(t *testing.T) {
	var reached atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("token forwarded")
		}
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer server.Close()
	err := New(server.Client()).DeleteTaskAttachment(context.Background(), server.URL, attachmentDeleteToken, 12, 5)
	if err == nil || reached.Load() != 0 {
		t.Errorf("err = %v, reached = %d", err, reached.Load())
	}
}

func TestTaskAttachmentDeleteScanPreflightAndReadback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+attachmentDeleteToken {
			t.Error("bad scan request")
		}
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, deletePage(1, 2, 1, "["+deleteItem(4)+","+deleteItem(5)+"]"))
		default:
			t.Error("unexpected page")
		}
	}))
	defer server.Close()
	meta, err := New(server.Client()).GetTaskAttachmentDeleteMeta(context.Background(), server.URL, attachmentDeleteToken, 12, 5)
	if err != nil || meta.ID != 5 || meta.Name != "name.bin" {
		t.Fatalf("meta = %#v, err = %v", meta, err)
	}
	readback, err := New(server.Client()).VerifyTaskAttachmentDeletedReadback(context.Background(), server.URL, attachmentDeleteToken, 12, 9)
	if err != nil || readback.Present {
		t.Fatalf("readback = %#v, err = %v", readback, err)
	}
	if _, err := New(server.Client()).GetTaskAttachmentDeleteMeta(context.Background(), server.URL, attachmentDeleteToken, 12, 9); !errors.Is(err, ErrNotFound) {
		t.Errorf("absent preflight = %v", err)
	}
}

func TestTaskAttachmentDeleteScanRejectsInvalidPages(t *testing.T) {
	cases := []string{
		`not json`, deletePage(1, 1, 1, "["+deleteItem(5)+"]") + ` {}`,
		deletePage(1, 2, 1, "["+deleteItem(5)+"]"),
		`{"items":[{"id":5,"task_id":99,"file":{"name":"x","size":0}}],"page":1,"per_page":1000,"total":1,"total_pages":1}`,
	}
	for _, body := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }))
		_, err := New(server.Client()).GetTaskAttachmentDeleteMeta(context.Background(), server.URL, attachmentDeleteToken, 12, 5)
		server.Close()
		if err == nil || strings.Contains(err.Error(), attachmentDeleteToken) {
			t.Errorf("invalid scan accepted: %v", err)
		}
	}
	for _, declared := range []bool{true, false} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if declared {
				w.Header().Set("Content-Length", strconv.FormatInt(attachmentDeleteResponseMaxBytes+1, 10))
			}
			_, _ = w.Write(bytes.Repeat([]byte("x"), int(attachmentDeleteResponseMaxBytes+1)))
		}))
		_, err := New(server.Client()).GetTaskAttachmentDeleteMeta(context.Background(), server.URL, attachmentDeleteToken, 12, 5)
		server.Close()
		if err == nil {
			t.Error("oversized scan accepted")
		}
	}
}

func TestTaskAttachmentDeleteScanLaterPageDuplicateAndLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 1 {
			_, _ = io.WriteString(w, deletePage(1, 1001, 2, "["+strings.Repeat(deleteItem(1)+",", 999)+deleteItem(5)+"]"))
			return
		}
		_, _ = io.WriteString(w, deletePage(2, 1001, 2, "["+deleteItem(5)+"]"))
	}))
	defer server.Close()
	if _, err := New(server.Client()).GetTaskAttachmentDeleteMeta(context.Background(), server.URL, attachmentDeleteToken, 12, 5); err == nil {
		t.Error("cross-page duplicate accepted")
	}
}

func TestTaskAttachmentDeleteScanFindsLaterPageAndRejectsPageLimit(t *testing.T) {
	var first strings.Builder
	for id := int64(1); id <= 1000; id++ {
		if id > 1 {
			first.WriteByte(',')
		}
		first.WriteString(deleteItem(id))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, deletePage(1, 1001, 2, "["+first.String()+"]"))
		case "2":
			_, _ = io.WriteString(w, deletePage(2, 1001, 2, "["+deleteItem(1001)+"]"))
		default:
			t.Errorf("unexpected page %q", r.URL.Query().Get("page"))
		}
	}))
	meta, err := New(server.Client()).GetTaskAttachmentDeleteMeta(context.Background(), server.URL, attachmentDeleteToken, 12, 1001)
	server.Close()
	if err != nil || meta.ID != 1001 {
		t.Fatalf("later-page meta = %#v, err = %v", meta, err)
	}
	var requests atomic.Int32
	limit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, deletePage(1, 100001, 101, "[]"))
	}))
	defer limit.Close()
	if _, err := New(limit.Client()).GetTaskAttachmentDeleteMeta(context.Background(), limit.URL, attachmentDeleteToken, 12, 1); err == nil {
		t.Error("over-page-limit scan accepted")
	}
	if requests.Load() != 1 {
		t.Errorf("requests = %d, want 1", requests.Load())
	}
}
