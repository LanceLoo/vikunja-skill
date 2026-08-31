package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

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
	configureTest(t, server.URL, token)

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
	configureTest(t, server.URL, token)

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
	if got, want := stdout.String(), response+"\n"; got != want {
		t.Errorf("non-pretty output = %q, want unchanged compact JSON %q", got, want)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--pretty", "tasks", "get", "42"}, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("pretty get: code = %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	assertPrettyJSON(t, stdout.Bytes())

	status = http.StatusNotFound
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"tasks", "get", "42"}, &stdout, &stderr); code != 1 {
		t.Fatalf("404 code = %d", code)
	}
	if !strings.Contains(stderr.String(), "任务或项目不存在") || strings.Contains(stderr.String(), token) || stdout.Len() != 0 {
		t.Fatalf("unsafe 404 output: stdout %q stderr %q", stdout.String(), stderr.String())
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
}

func TestTaskRelations(t *testing.T) {
	token := "test-secret-token"
	const response = `{"id":42,"related_tasks":{"blocks":[{"id":7,"unknown":null}]},"other":{"ignored":true}}`
	const related = `{"blocks":[{"id":7,"unknown":null}]}`
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/tasks/42" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("request = %s %s authorization = %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	configureTest(t, server.URL, token)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"tasks", "relations", "42"}, &stdout, &stderr); code != 0 || stderr.Len() != 0 || stdout.String() != related+"\n" {
		t.Fatalf("code = %d, stdout %q stderr %q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--pretty", "tasks", "relations", "42"}, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("pretty code = %d, stdout %q stderr %q", code, stdout.String(), stderr.String())
	}
	assertPrettyJSON(t, stdout.Bytes())
	var got, want any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode pretty output: %v", err)
	}
	if err := json.Unmarshal([]byte(related), &want); err != nil || !jsonEqual(got, want) {
		t.Fatalf("pretty output = %q, error = %v", stdout.String(), err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestTaskRelationsInvalidArgumentsDoNotRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	for _, config := range []struct{ baseURL, token string }{
		{"", ""},
		{"://invalid-url", ""},
		{server.URL, ""},
	} {
		configureTest(t, config.baseURL, config.token)
		for _, args := range [][]string{
			{"tasks", "relations"},
			{"tasks", "relations", "0"},
			{"tasks", "relations", "nope"},
			{"tasks", "relations", "1", "extra"},
			{"tasks", "relations", "1", "--apply"},
			{"tasks", "relations", "1", "--confirm", "sha256:deadbeef"},
			{"tasks", "relations", "1", "create", "blocks"},
		} {
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || stderr.Len() == 0 || !strings.Contains(stderr.String(), "用法:") {
				t.Errorf("config=%q/%q run(%v) = %d, stdout %q stderr %q", config.baseURL, config.token, args, code, stdout.String(), stderr.String())
			}
		}
	}
	if requests != 0 {
		t.Fatalf("invalid relation arguments made %d HTTP requests", requests)
	}
}

func TestTaskRelationsResourceErrorsAreSafe(t *testing.T) {
	token := "test-secret-token"
	baseSecret, fragmentSecret := "base-secret", "fragment-secret"
	for _, test := range []struct {
		name, message string
		handler       http.HandlerFunc
	}{
		{"unauthorized", "认证失败：服务未接受 Token", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }},
		{"forbidden", "权限不足：任务读取被拒绝", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }},
		{"not-found", "任务或项目不存在", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) }},
		{"redirect", "302", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "/target?token="+token)
			w.WriteHeader(http.StatusFound)
		}},
		{"service", "502", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }},
		{"malformed-json", "cannot decode task relations response", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"related_tasks":`)) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(test.handler)
			defer server.Close()
			configureTest(t, server.URL+"?"+baseSecret+"#"+fragmentSecret, token)
			var stdout, stderr bytes.Buffer
			if code := run([]string{"tasks", "relations", "42"}, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), test.message) {
				t.Fatalf("code = %d, stdout %q stderr %q", code, stdout.String(), stderr.String())
			}
			for _, secret := range []string{token, baseSecret, fragmentSecret} {
				if strings.Contains(stderr.String(), secret) {
					t.Errorf("stderr leaked %q: %q", secret, stderr.String())
				}
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	baseURL := server.URL
	server.Close()
	configureTest(t, baseURL+"?"+baseSecret+"#"+fragmentSecret, token)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"tasks", "relations", "42"}, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "request to") || strings.Contains(stderr.String(), token) || strings.Contains(stderr.String(), baseSecret) || strings.Contains(stderr.String(), fragmentSecret) {
		t.Fatalf("transport: code = %d stdout %q stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestTasksPrettyInvalidArgumentsWriteOnlyStderr(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()
	configureTest(t, server.URL, "test-secret-token")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"--pretty", "tasks", "get", "not-an-id"}, &stdout, &stderr); code != 2 {
		t.Errorf("run() = %d, want 2", code)
	}
	if stdout.Len() != 0 || stderr.Len() == 0 {
		t.Errorf("pretty invalid-argument output: stdout %q stderr %q", stdout.String(), stderr.String())
	}
	if requests != 0 {
		t.Errorf("pretty invalid arguments made %d HTTP requests, want 0", requests)
	}
}

func TestTasksInvalidArgumentsDoNotRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()
	configureTest(t, server.URL, "test-secret-token")

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
	for _, args := range [][]string{{"tasks", "--help"}, {"tasks", "list", "--help"}, {"tasks", "get", "--help"}, {"tasks", "relations", "--help"}, {"tasks", "create", "--help"}, {"tasks", "update", "--help"}, {"tasks", "complete", "--help"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 {
			t.Errorf("run(%v) = %d, want 0", args, code)
		}
		if stdout.Len() == 0 || stderr.Len() != 0 {
			t.Errorf("run(%v): stdout %q stderr %q", args, stdout.String(), stderr.String())
		}
	}
}

func TestTaskMutationsPreviewDoesNotWrite(t *testing.T) {
	token := "test-secret-token"
	var requests []struct{ method, path string }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, struct{ method, path string }{r.Method, r.URL.Path})
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/tasks/42" {
			t.Errorf("preview request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"id":42,"title":"Current","done":false}`))
	}))
	defer server.Close()
	configureTest(t, server.URL, token)

	var stdout, stderr bytes.Buffer
	compactCreate := []string{"tasks", "create", "7", "--title", "New", "--description", "details", "--priority", "2", "--due-date", "2026-08-06T10:00:00Z"}
	if code := run(compactCreate, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("run(%v) = %d, stdout %q, stderr %q", compactCreate, code, stdout.String(), stderr.String())
	}
	if got, want := stdout.String(), "{\"changes\":{\"title\":\"New\",\"description\":\"details\",\"priority\":2,\"due_date\":\"2026-08-06T10:00:00Z\"},\"mode\":\"preview\",\"operation\":\"create\",\"project_id\":7}\n"; got != want {
		t.Errorf("compact create preview = %q, want %q", got, want)
	}

	cases := []struct {
		args []string
		want map[string]any
	}{
		{[]string{"--pretty", "tasks", "create", "7", "--title", "New", "--description", "details", "--priority", "2", "--due-date", "2026-08-06T10:00:00Z"}, map[string]any{"mode": "preview", "operation": "create", "project_id": float64(7), "changes": map[string]any{"title": "New", "description": "details", "priority": float64(2), "due_date": "2026-08-06T10:00:00Z"}}},
		{[]string{"--pretty", "tasks", "update", "42", "--title", "Updated"}, map[string]any{"mode": "preview", "operation": "update", "task_id": float64(42), "current": map[string]any{"id": float64(42), "title": "Current", "done": false}, "changes": map[string]any{"title": "Updated"}}},
		{[]string{"--pretty", "tasks", "complete", "42"}, map[string]any{"mode": "preview", "operation": "complete", "task_id": float64(42), "current": map[string]any{"id": float64(42), "title": "Current", "done": false}, "changes": map[string]any{"done": true}}},
	}
	for _, test := range cases {
		var stdout, stderr bytes.Buffer
		if code := run(test.args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
			t.Fatalf("run(%v) = %d, stdout %q, stderr %q", test.args, code, stdout.String(), stderr.String())
		}
		var preview map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &preview); err != nil {
			t.Fatalf("preview is not JSON: %v", err)
		}
		if !jsonEqual(preview, test.want) {
			t.Errorf("preview for %v = %#v, want %#v", test.args, preview, test.want)
		}
		assertPrettyJSON(t, stdout.Bytes())
	}
	if len(requests) != 2 { // create needs no request; update and complete read current state.
		t.Fatalf("requests = %#v, want exactly two GET requests", requests)
	}
}

func TestTaskMutationsApplyWrites(t *testing.T) {
	var requests []struct {
		method, path, contentType, authorization string
		body                                     map[string]any
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		requests = append(requests, struct {
			method, path, contentType, authorization string
			body                                     map[string]any
		}{r.Method, r.URL.Path, r.Header.Get("Content-Type"), r.Header.Get("Authorization"), body})
		_, _ = w.Write([]byte(`{"id":42,"title":"Written","metadata":{"source":"test"}}`))
	}))
	defer server.Close()
	token := "test-secret-token"
	configureTest(t, server.URL, token)

	for _, args := range [][]string{
		{"--pretty", "tasks", "create", "7", "--title", "New", "--due-date", "2026-08-06T10:00:00Z", "--apply"},
		{"--pretty", "tasks", "update", "42", "--description", "Changed", "--apply"},
		{"--pretty", "tasks", "complete", "42", "--apply"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
			t.Fatalf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
		}
		var task map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &task); err != nil || task["id"] != float64(42) {
			t.Fatalf("apply output = %q, error = %v", stdout.String(), err)
		}
		assertPrettyJSON(t, stdout.Bytes())
	}
	if len(requests) != 3 {
		t.Fatalf("write requests = %d, want 3", len(requests))
	}
	for i, want := range []struct {
		method, path, contentType string
		body                      map[string]any
	}{
		{http.MethodPost, "/api/v2/projects/7/tasks", "application/json", map[string]any{"title": "New", "due_date": "2026-08-06T10:00:00Z"}},
		{http.MethodPatch, "/api/v2/tasks/42", "application/merge-patch+json", map[string]any{"description": "Changed"}},
		{http.MethodPatch, "/api/v2/tasks/42", "application/merge-patch+json", map[string]any{"done": true}},
	} {
		got := requests[i]
		if got.method != want.method || got.path != want.path || got.contentType != want.contentType || got.authorization != "Bearer "+token || !jsonEqual(got.body, want.body) {
			t.Errorf("request %d = %#v, want method=%s path=%s content-type=%s authorization=%q body=%#v", i, got, want.method, want.path, want.contentType, "Bearer "+token, want.body)
		}
	}
}

func TestTaskMutationApplyResourceErrorsAreSafe(t *testing.T) {
	token := "test-secret-token"
	baseSecret := "base-secret"
	fragmentSecret := "fragment-secret"
	for _, command := range [][]string{
		{"tasks", "create", "7", "--title", "New", "--apply"},
		{"tasks", "update", "42", "--title", "New", "--apply"},
		{"tasks", "complete", "42", "--apply"},
	} {
		for _, test := range []struct {
			name, message string
			handler       http.HandlerFunc
		}{
			{"unauthorized", "认证失败：服务未接受 Token", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }},
			{"forbidden", "权限不足：任务写入被拒绝", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }},
			{"not-found", "任务或项目不存在", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) }},
			{"redirect", "request to", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "://invalid-redirect")
				w.WriteHeader(http.StatusFound)
			}},
		} {
			t.Run(strings.Join(command[1:3], "-")+"-"+test.name, func(t *testing.T) {
				server := httptest.NewServer(test.handler)
				defer server.Close()
				configureTest(t, server.URL+"?"+baseSecret+"#"+fragmentSecret, token)
				var stdout, stderr bytes.Buffer
				if code := run(command, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), test.message) {
					t.Fatalf("run(%v) = %d, stdout %q, stderr %q", command, code, stdout.String(), stderr.String())
				}
				for _, secret := range []string{token, "Authorization", baseSecret, fragmentSecret} {
					if strings.Contains(stderr.String(), secret) {
						t.Errorf("stderr leaked %q: %q", secret, stderr.String())
					}
				}
			})
		}
		t.Run(strings.Join(command[1:3], "-")+"-transport", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			baseURL := server.URL
			server.Close()
			configureTest(t, baseURL+"?"+baseSecret+"#"+fragmentSecret, token)
			var stdout, stderr bytes.Buffer
			if code := run(command, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "request to") {
				t.Fatalf("run(%v) = %d, stdout %q, stderr %q", command, code, stdout.String(), stderr.String())
			}
			for _, secret := range []string{token, "Authorization", baseSecret, fragmentSecret} {
				if strings.Contains(stderr.String(), secret) {
					t.Errorf("stderr leaked %q: %q", secret, stderr.String())
				}
			}
		})
	}
}

func TestTaskMutationInvalidArgumentsDoNotRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	configureTest(t, server.URL, "test-secret-token")
	for _, args := range [][]string{
		{"tasks", "create", "0", "--title", "New"},
		{"tasks", "create", "7"},
		{"tasks", "create", "7", "--title", " "},
		{"tasks", "create", "7", "--due-date", "invalid", "--title", "New"},
		{"tasks", "update", "0", "--title", "New"},
		{"tasks", "update", "42"},
		{"tasks", "update", "42", "--priority", "-1"},
		{"tasks", "update", "42", "--due-date", "invalid"},
		{"tasks", "complete", "0"},
		{"tasks", "complete", "42", "extra"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Errorf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
		}
	}
	if requests != 0 {
		t.Errorf("invalid arguments made %d HTTP requests", requests)
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
	configureTest(t, server.URL, token)
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
	configureTest(t, server.URL, "test-secret-token")
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
			configureTest(t, server.URL+"?base-secret", token)
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
	configureTest(t, server.URL, token)
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
	configureTest(t, server.URL, "test-secret-token")
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
			configureTest(t, server.URL+"?base-secret", token)
			var stdout, stderr bytes.Buffer
			if code := run([]string{"tasks", "attachments", "42"}, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), test.message) || strings.Contains(stderr.String(), token) || strings.Contains(stderr.String(), "base-secret") {
				t.Fatalf("code = %d, stdout %q stderr %q", code, stdout.String(), stderr.String())
			}
		})
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
	configureTest(t, server.URL, token)
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
