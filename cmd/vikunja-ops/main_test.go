package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"vikunja-opencode-skill/internal/client"
)

func TestSharedCLIHelpers(t *testing.T) {
	t.Run("pagination defaults and range", func(t *testing.T) {
		flags := flag.NewFlagSet("test", flag.ContinueOnError)
		page, perPage := bindPaginationFlags(flags)
		if *page != 1 || *perPage != 50 {
			t.Fatalf("defaults = page %d, per-page %d", *page, *perPage)
		}
		for _, pagination := range [][2]int{{1, 1}, {1, 50}, {2, 1000}} {
			if !validPagination(pagination[0], pagination[1]) {
				t.Errorf("validPagination(%d, %d) = false", pagination[0], pagination[1])
			}
		}
		for _, pagination := range [][2]int{{0, 50}, {1, 0}, {1, 1001}} {
			if validPagination(pagination[0], pagination[1]) {
				t.Errorf("validPagination(%d, %d) = true", pagination[0], pagination[1])
			}
		}
	})

	t.Run("JSON preserves bytes and appends newline", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		value := json.RawMessage(`{"unknown_field":{"preserved":true}}`)
		if code := writeJSON("resource get", value, &stdout, &stderr); code != 0 {
			t.Fatalf("writeJSON() = %d, stderr %q", code, stderr.String())
		}
		if got, want := stdout.String(), "{\"unknown_field\":{\"preserved\":true}}\n"; got != want {
			t.Errorf("stdout = %q, want %q", got, want)
		}
	})

	t.Run("resource errors retain specific messages without error details", func(t *testing.T) {
		var stderr bytes.Buffer
		secret := errors.New("request failed with test-secret-token")
		if code := printResourceError("tasks get", errors.Join(client.ErrNotFound, secret), "任务读取被拒绝", "任务或项目不存在", &stderr); code != 1 {
			t.Fatalf("printResourceError() = %d", code)
		}
		if got, want := stderr.String(), "tasks get: 任务或项目不存在\n"; got != want {
			t.Errorf("stderr = %q, want %q", got, want)
		}
	})
}

func TestProjectsList(t *testing.T) {
	token := "test-secret-token"
	var gotQueries []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/projects" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		gotQueries = append(gotQueries, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"total":0,"page":1,"per_page":50,"total_pages":0}`))
	}))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", token)

	for _, args := range [][]string{{"projects", "list"}, {"projects", "list", "--page", "2", "--per-page", "25"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 {
			t.Fatalf("run(%v) = %d, stderr %q", args, code, stderr.String())
		}
		if stderr.Len() != 0 || !strings.HasSuffix(stdout.String(), "\n") {
			t.Fatalf("output = stdout %q stderr %q", stdout.String(), stderr.String())
		}
		var value any
		if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
			t.Fatalf("stdout is not JSON: %v", err)
		}
	}
	if gotQueries[0].Get("page") != "1" || gotQueries[0].Get("per_page") != "50" {
		t.Errorf("default query = %v", gotQueries[0])
	}
	if gotQueries[1].Get("page") != "2" || gotQueries[1].Get("per_page") != "25" {
		t.Errorf("specified query = %v", gotQueries[1])
	}
}

func TestProjectsGetAndNotFound(t *testing.T) {
	token := "test-secret-token"
	status := http.StatusOK
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/projects/42" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(`{"id":42,"title":"Project","parent_project_id":null,"unknown_field":{"preserved":true}}`))
		}
	}))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", token)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"projects", "get", "42"}, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("code = %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	var project, expected map[string]any
	err := json.Unmarshal(stdout.Bytes(), &project)
	if expectedErr := json.Unmarshal([]byte(`{"id":42,"title":"Project","parent_project_id":null,"unknown_field":{"preserved":true}}`), &expected); err != nil || expectedErr != nil || !jsonEqual(project, expected) {
		t.Fatalf("project output = %q, error = %v", stdout.String(), err)
	}

	status = http.StatusNotFound
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"projects", "get", "42"}, &stdout, &stderr); code != 1 {
		t.Fatalf("404 code = %d", code)
	}
	if strings.Contains(stderr.String(), token) || stdout.Len() != 0 {
		t.Fatalf("unsafe 404 output: stdout %q stderr %q", stdout.String(), stderr.String())
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func jsonEqual(left, right any) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return bytes.Equal(leftJSON, rightJSON)
}

func TestProjectsGetRejectsInvalidIDBeforeRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", "test-secret-token")

	for _, id := range []string{"abc", "-1", "9223372036854775808"} {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"projects", "get", id}, &stdout, &stderr); code != 2 {
			t.Errorf("get %q: code = %d, want 2", id, code)
		}
		if stdout.Len() != 0 || stderr.Len() == 0 {
			t.Errorf("get %q: stdout %q stderr %q", id, stdout.String(), stderr.String())
		}
	}
	if requests != 0 {
		t.Errorf("invalid IDs made %d HTTP requests, want 0", requests)
	}
}

func TestProjectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{"projects", "list", "--unknown"}, {"projects", "list", "--page", "x"}, {"projects", "list", "--page", "0"}, {"projects", "list", "--per-page", "1001"}, {"projects", "list", "extra"}, {"projects", "get"}, {"projects", "get", "", "extra"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
		if stdout.Len() != 0 || stderr.Len() == 0 {
			t.Errorf("run(%v): stdout %q stderr %q", args, stdout.String(), stderr.String())
		}
	}
}

func TestProjectsHelpWritesOnlyStdout(t *testing.T) {
	for _, args := range [][]string{{"projects", "--help"}, {"projects", "list", "--help"}, {"projects", "get", "--help"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 {
			t.Errorf("run(%v) = %d, want 0", args, code)
		}
		if stdout.Len() == 0 || stderr.Len() != 0 {
			t.Errorf("run(%v): stdout %q stderr %q", args, stdout.String(), stderr.String())
		}
	}
}

func TestTasksList(t *testing.T) {
	token := "test-secret-token"
	var requests []struct {
		path  string
		query url.Values
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		requests = append(requests, struct {
			path  string
			query url.Values
		}{r.URL.Path, r.URL.Query()})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"total":0,"page":1,"per_page":50,"total_pages":0}`))
	}))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", token)

	cases := [][]string{
		{"tasks", "list"},
		{"tasks", "list", "--project", "7", "--page", "2", "--per-page", "25", "--query", "review", "--filter", "done = false", "--filter-timezone", "Asia/Shanghai", "--include-nulls", "--sort-by", "priority", "--order-by", "desc", "--sort-by", "title", "--order-by", "asc"},
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 {
			t.Fatalf("run(%v) = %d, stderr %q", args, code, stderr.String())
		}
		if stderr.Len() != 0 || !strings.HasSuffix(stdout.String(), "\n") {
			t.Fatalf("output = stdout %q stderr %q", stdout.String(), stderr.String())
		}
		var value any
		if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
			t.Fatalf("stdout is not JSON: %v", err)
		}
	}
	if len(requests) != len(cases) {
		t.Fatalf("requests = %d, want %d", len(requests), len(cases))
	}
	if requests[0].path != "/api/v2/tasks" || requests[0].query.Get("page") != "1" || requests[0].query.Get("per_page") != "50" {
		t.Errorf("default request = %s?%s", requests[0].path, requests[0].query.Encode())
	}
	got := requests[1]
	if got.path != "/api/v2/projects/7/tasks" {
		t.Errorf("project request path = %q", got.path)
	}
	if got.query.Get("page") != "2" || got.query.Get("per_page") != "25" || got.query.Get("q") != "review" || got.query.Get("filter") != "done = false" || got.query.Get("filter_timezone") != "Asia/Shanghai" || got.query.Get("filter_include_nulls") != "true" {
		t.Errorf("full query = %v", got.query)
	}
	if strings.Join(got.query["sort_by"], ",") != "priority,title" || strings.Join(got.query["order_by"], ",") != "desc,asc" {
		t.Errorf("sort query = %v", got.query)
	}
}

func TestTasksGetAndNotFound(t *testing.T) {
	token := "test-secret-token"
	status := http.StatusOK
	requests := 0
	const response = `{"id":42,"title":"Task","unknown_field":{"preserved":true}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/tasks/42" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(response))
		}
	}))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", token)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"tasks", "get", "42"}, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("code = %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	var task, expected any
	if err := json.Unmarshal(stdout.Bytes(), &task); err != nil {
		t.Fatalf("task output is not JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(response), &expected); err != nil || !jsonEqual(task, expected) {
		t.Fatalf("task output = %q, error = %v", stdout.String(), err)
	}

	status = http.StatusNotFound
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"tasks", "get", "42"}, &stdout, &stderr); code != 1 {
		t.Fatalf("404 code = %d", code)
	}
	if !strings.Contains(stderr.String(), "任务或项目不存在") || strings.Contains(stderr.String(), token) || stdout.Len() != 0 {
		t.Fatalf("unsafe 404 output: stdout %q stderr %q", stdout.String(), stderr.String())
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestTasksInvalidArgumentsDoNotRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", "test-secret-token")

	for _, args := range [][]string{
		{"tasks", "list", "--project", "0"},
		{"tasks", "list", "--project", "-1"},
		{"tasks", "list", "--sort-by", "title"},
		{"tasks", "list", "--order-by", "asc"},
		{"tasks", "list", "--sort-by", ""},
		{"tasks", "list", "--sort-by", "   ", "--order-by", "asc"},
		{"tasks", "list", "--sort-by", "title", "--order-by", "   "},
		{"tasks", "list", "--sort-by", "title", "--order-by", "sideways"},
		{"tasks", "list", "--page", "0"},
		{"tasks", "list", "--per-page", "1001"},
		{"tasks", "get", "0"},
		{"tasks", "get", "-1"},
		{"tasks", "get", "abc"},
		{"tasks", "get", "9223372036854775808"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
		if stdout.Len() != 0 || stderr.Len() == 0 {
			t.Errorf("run(%v): stdout %q stderr %q", args, stdout.String(), stderr.String())
		}
	}
	if requests != 0 {
		t.Errorf("invalid arguments made %d HTTP requests, want 0", requests)
	}
}

func TestTasksHelpWritesOnlyStdout(t *testing.T) {
	for _, args := range [][]string{{"tasks", "--help"}, {"tasks", "list", "--help"}, {"tasks", "get", "--help"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 {
			t.Errorf("run(%v) = %d, want 0", args, code)
		}
		if stdout.Len() == 0 || stderr.Len() != 0 {
			t.Errorf("run(%v): stdout %q stderr %q", args, stdout.String(), stderr.String())
		}
	}
}

func TestLabelsListAndTaskLabels(t *testing.T) {
	token := "test-secret-token"
	requests := 0
	var listFormats []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || (r.URL.Path != "/api/v2/labels" && r.URL.Path != "/api/v2/tasks/42/labels") {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+token || r.URL.Query().Get("page") != "2" || r.URL.Query().Get("per_page") != "25" || r.URL.Query().Get("q") != "find me" {
			t.Errorf("headers/query = %q %v", r.Header.Get("Authorization"), r.URL.Query())
		}
		if r.URL.Path == "/api/v2/tasks/42/labels" && r.URL.Query().Get("format") != "" {
			t.Errorf("task label format = %q", r.URL.Query().Get("format"))
		}
		if r.URL.Path == "/api/v2/labels" {
			listFormats = append(listFormats, r.URL.Query().Get("format"))
		}
		_, _ = w.Write([]byte(`{"items":[{"id":1,"unknown":null}],"total":1,"page":2,"per_page":25,"total_pages":1,"$schema":"https://example.test/labels.json","extra":null}`))
	}))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", token)
	for _, args := range [][]string{{"labels", "list", "--page", "2", "--per-page", "25", "--q", "find me", "--format", "html"}, {"labels", "list", "--page", "2", "--per-page", "25", "--q", "find me", "--format", "markdown"}, {"labels", "list", "--page", "2", "--per-page", "25", "--q", "find me"}, {"tasks", "labels", "42", "--page", "2", "--per-page", "25", "--q", "find me"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 || stdout.String() != "{\"items\":[{\"id\":1,\"unknown\":null}],\"total\":1,\"page\":2,\"per_page\":25,\"total_pages\":1,\"$schema\":\"https://example.test/labels.json\",\"extra\":null}\n" {
			t.Fatalf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
		}
	}
	if requests != 4 {
		t.Fatalf("requests = %d", requests)
	}
	if got, want := strings.Join(listFormats, ","), "html,markdown,"; got != want {
		t.Errorf("labels list formats = %q, want %q", got, want)
	}
}

func TestLabelsInvalidArgumentsDoNotRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", "test-secret-token")
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
			t.Setenv("VIKUNJA_URL", server.URL+"?base-secret")
			t.Setenv("VIKUNJA_TOKEN", token)
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

func TestTaskComments(t *testing.T) {
	token := "test-secret-token"
	var requests []url.Values
	response := `{"items":[{"id":1,"comment":null,"unknown":{"nested":true}}],"total":1,"page":2,"per_page":25,"total_pages":1,"$schema":"https://example.test/comments.json","extra":null}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/tasks/42/comments" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("request = %s %s, authorization = %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		requests = append(requests, r.URL.Query())
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", token)
	for _, args := range [][]string{{"tasks", "comments", "42"}, {"tasks", "comments", "42", "--page", "2", "--per-page", "25", "--q", "find me", "--order-by", "desc"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 || stdout.String() != response+"\n" {
			t.Fatalf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
		}
	}
	if len(requests) != 2 || requests[0].Get("page") != "1" || requests[0].Get("per_page") != "50" || requests[0].Get("q") != "" || requests[0].Get("order_by") != "" || requests[1].Get("page") != "2" || requests[1].Get("per_page") != "25" || requests[1].Get("q") != "find me" || requests[1].Get("order_by") != "desc" {
		t.Fatalf("queries = %#v", requests)
	}
}

func TestTaskCommentsInvalidArgumentsDoNotRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", "test-secret-token")
	for _, args := range [][]string{{"tasks", "comments", "0"}, {"tasks", "comments", "nope"}, {"tasks", "comments", "1", "--page", "0"}, {"tasks", "comments", "1", "--per-page", "1001"}, {"tasks", "comments", "1", "--order-by", "newest"}, {"tasks", "comments", "1", "--format", "markdown"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Errorf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
		}
	}
	if requests != 0 {
		t.Errorf("invalid task comment arguments made %d HTTP requests", requests)
	}
}

func TestTaskCommentsResourceErrorsAreSafe(t *testing.T) {
	token := "test-secret-token"
	for _, test := range []struct {
		status  int
		message string
	}{{http.StatusUnauthorized, "认证失败：服务未接受 Token"}, {http.StatusForbidden, "权限不足：评论读取被拒绝"}, {http.StatusNotFound, "任务或评论不存在"}} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status) }))
			defer server.Close()
			t.Setenv("VIKUNJA_URL", server.URL+"?base-secret")
			t.Setenv("VIKUNJA_TOKEN", token)
			var stdout, stderr bytes.Buffer
			if code := run([]string{"tasks", "comments", "42", "--q", "search-secret"}, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), test.message) || strings.Contains(stderr.String(), token) || strings.Contains(stderr.String(), "base-secret") || strings.Contains(stderr.String(), "search-secret") {
				t.Fatalf("code = %d, stdout %q stderr %q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestTaskAttachments(t *testing.T) {
	token := "test-secret-token"
	var requests []url.Values
	response := `{"items":[{"id":1,"file":null,"unknown":{"nested":true}}],"total":1,"page":1,"per_page":50,"total_pages":1,"$schema":"https://example.test/attachments.json","extra":null}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/tasks/42/attachments" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("request = %s %s, authorization = %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		requests = append(requests, r.URL.Query())
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", token)
	for _, args := range [][]string{{"tasks", "attachments", "42"}, {"tasks", "attachments", "42", "--page", "2", "--per-page", "25"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 || stdout.String() != response+"\n" {
			t.Fatalf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
		}
	}
	if len(requests) != 2 || requests[0].Get("page") != "1" || requests[0].Get("per_page") != "50" || requests[0].Get("q") != "" || requests[1].Get("page") != "2" || requests[1].Get("per_page") != "25" || requests[1].Get("q") != "" {
		t.Fatalf("queries = %#v", requests)
	}
}

func TestTaskAttachmentsInvalidArgumentsDoNotRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", "test-secret-token")
	for _, args := range [][]string{{"tasks", "attachments", "0"}, {"tasks", "attachments", "nope"}, {"tasks", "attachments", "1", "--page", "0"}, {"tasks", "attachments", "1", "--per-page", "1001"}, {"tasks", "attachments", "1", "--q", "ignored"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Errorf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
		}
	}
	if requests != 0 {
		t.Errorf("invalid task attachment arguments made %d HTTP requests", requests)
	}
}

func TestTaskAttachmentsResourceErrorsAreSafe(t *testing.T) {
	token := "test-secret-token"
	for _, test := range []struct {
		status  int
		message string
	}{{http.StatusUnauthorized, "认证失败：服务未接受 Token"}, {http.StatusForbidden, "权限不足：附件读取被拒绝"}, {http.StatusNotFound, "任务或附件不存在"}} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status) }))
			defer server.Close()
			t.Setenv("VIKUNJA_URL", server.URL+"?base-secret")
			t.Setenv("VIKUNJA_TOKEN", token)
			var stdout, stderr bytes.Buffer
			if code := run([]string{"tasks", "attachments", "42"}, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), test.message) || strings.Contains(stderr.String(), token) || strings.Contains(stderr.String(), "base-secret") {
				t.Fatalf("code = %d, stdout %q stderr %q", code, stdout.String(), stderr.String())
			}
		})
	}
}
