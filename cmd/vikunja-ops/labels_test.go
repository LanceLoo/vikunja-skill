package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLabelsInvalidArgumentsDoNotRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	configureTest(t, server.URL, "test-secret-token")
	for _, args := range [][]string{{"labels", "list", "--page", "0"}, {"labels", "list", "--format", "json"}, {"tasks", "labels", "0"}, {"tasks", "labels", "nope"}, {"tasks", "labels", "1", "--per-page", "1001"}, {"tasks", "labels", "1", "--format", "json"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Errorf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
		}
	}
	if requests != 0 {
		t.Errorf("invalid label arguments made %d HTTP requests", requests)
	}
}

func TestLabelsResourceErrorsAreSafe(t *testing.T) {
	token := "test-secret-token"
	for _, test := range []struct {
		status  int
		message string
	}{
		{http.StatusUnauthorized, "认证失败：服务未接受 Token"},
		{http.StatusForbidden, "权限不足：标签读取被拒绝"},
		{http.StatusNotFound, "标签或任务不存在"},
	} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+token {
					t.Errorf("authorization = %q", r.Header.Get("Authorization"))
				}
				w.WriteHeader(test.status)
			}))
			defer server.Close()
			configureTest(t, server.URL+"?base-secret", token)
			for _, args := range [][]string{{"labels", "list", "--q", "search-secret"}, {"tasks", "labels", "42", "--q", "search-secret"}} {
				var stdout, stderr bytes.Buffer
				if code := run(args, &stdout, &stderr); code != 1 {
					t.Errorf("run(%v) = %d, want 1", args, code)
				}
				if stdout.Len() != 0 || !strings.Contains(stderr.String(), test.message) || strings.Contains(stderr.String(), token) || strings.Contains(stderr.String(), "base-secret") || strings.Contains(stderr.String(), "search-secret") {
					t.Errorf("run(%v): stdout %q stderr %q", args, stdout.String(), stderr.String())
				}
			}
		})
	}
}
