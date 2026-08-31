package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const taskDeleteToken = "test-secret-token"

type taskDeleteRequest struct {
	method, path string
	rawBody      string
}

type taskDeleteServerOptions struct {
	getStatus       int    // GET status while the task is present (default 200)
	getBody         string // GET body while the task is present (default task 12 in project 7)
	deleteStatus    int    // DELETE response status (default 204)
	afterGetStatus  int    // GET status after a 204 DELETE (default 404)
	failedGetStatus int    // GET status after a non-204 DELETE (default: same as getStatus)
}

// newTaskDeleteServer records requests and serves task 12 GETs plus exactly
// one DELETE. GET behavior after a DELETE depends on the DELETE outcome.
func newTaskDeleteServer(t *testing.T, opts taskDeleteServerOptions) (*httptest.Server, *[]taskDeleteRequest) {
	t.Helper()
	if opts.getStatus == 0 {
		opts.getStatus = http.StatusOK
	}
	if opts.getBody == "" {
		// The default body deliberately embeds secrets in fields that must
		// never surface in CLI output.
		opts.getBody = `{"id":12,"title":"Task 12","project_id":7,"description":"` + taskDeleteToken + ` base-secret"}`
	}
	if opts.deleteStatus == 0 {
		opts.deleteStatus = http.StatusNoContent
	}
	if opts.afterGetStatus == 0 {
		opts.afterGetStatus = http.StatusNotFound
	}
	failedGetStatus := opts.failedGetStatus
	if failedGetStatus == 0 {
		failedGetStatus = opts.getStatus
	}
	requests := &[]taskDeleteRequest{}
	deleted, deleteFailed := false, false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+taskDeleteToken {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		record := taskDeleteRequest{method: r.Method, path: r.URL.Path}
		if r.Method != http.MethodGet {
			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r.Body)
			record.rawBody = buf.String()
		}
		*requests = append(*requests, record)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/tasks/12":
			status := opts.getStatus
			switch {
			case deleted:
				status = opts.afterGetStatus
			case deleteFailed:
				status = failedGetStatus
			}
			w.WriteHeader(status)
			if status == http.StatusOK {
				_, _ = w.Write([]byte(opts.getBody))
			}
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v2/tasks/12":
			if opts.deleteStatus == http.StatusNoContent {
				deleted = true
			} else {
				deleteFailed = true
			}
			w.WriteHeader(opts.deleteStatus)
			if opts.deleteStatus != http.StatusNoContent {
				_, _ = w.Write([]byte(`{"message":"server detail with ` + taskDeleteToken + `"}`))
			}
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	return server, requests
}

func taskDeleteTokenFor(t *testing.T, taskID, projectID int64) string {
	t.Helper()
	token, err := taskDeleteConfirmationToken(taskID, projectID)
	if err != nil {
		t.Fatalf("taskDeleteConfirmationToken: %v", err)
	}
	return token
}

func applyTaskDeleteArgs(taskID, projectID int64, token string) []string {
	return []string{"tasks", "delete", "--id", fmt.Sprint(taskID), "--project-id", fmt.Sprint(projectID), "--apply", "--confirm", token}
}

func TestTasksDeleteHelp(t *testing.T) {
	for _, args := range [][]string{{"tasks", "delete", "--help"}, {"tasks", "--help"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 {
			t.Errorf("run(%v) = %d, want 0", args, code)
		}
		if !strings.Contains(stdout.String(), "tasks delete") || !strings.Contains(stdout.String(), "--confirm") || stderr.Len() != 0 {
			t.Errorf("run(%v): stdout %q stderr %q", args, stdout.String(), stderr.String())
		}
	}
}

func TestTasksDeleteInvalidArgumentsDoNotRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	configureTest(t, server.URL, taskDeleteToken)

	validToken := taskDeleteTokenFor(t, 12, 7)
	otherToken := taskDeleteTokenFor(t, 12, 8)
	uppercase := strings.ToUpper(validToken)

	cases := [][]string{
		{"tasks", "delete"},
		{"tasks", "delete", "--id", "12"},
		{"tasks", "delete", "--project-id", "7"},
		{"tasks", "delete", "--id", "0", "--project-id", "7"},
		{"tasks", "delete", "--id", "-1", "--project-id", "7"},
		{"tasks", "delete", "--id", "abc", "--project-id", "7"},
		{"tasks", "delete", "--id", "9223372036854775808", "--project-id", "7"},
		{"tasks", "delete", "--id", "12", "--project-id", "0"},
		{"tasks", "delete", "--id", "12", "--project-id", "-7"},
		{"tasks", "delete", "12", "--project-id", "7"},                          // positional ID unsupported
		{"tasks", "delete", "--id", "12", "--project-id", "7", "extra"},         // extra positional
		{"tasks", "delete", "--id", "12", "--project-id", "7", "--all"},         // unknown flags rejected
		{"tasks", "delete", "--id", "12", "--project-id", "7", "--ids", "12"},   // multi-ID flags unsupported
		{"tasks", "delete", "--id", "12", "--project-id", "7", "--filter", "x"}, // filter unsupported
		// apply/confirm gating: none of these may load config or send requests.
		{"tasks", "delete", "--id", "12", "--project-id", "7", "--confirm", validToken},
		{"tasks", "delete", "--id", "12", "--project-id", "7", "--apply"},
		{"tasks", "delete", "--id", "12", "--project-id", "7", "--apply", "--confirm", ""},
		{"tasks", "delete", "--id", "12", "--project-id", "7", "--apply", "--confirm", "sha256:wrong"},
		{"tasks", "delete", "--id", "12", "--project-id", "7", "--apply", "--confirm", "sha256:" + strings.Repeat("a", 63)},
		{"tasks", "delete", "--id", "12", "--project-id", "7", "--apply", "--confirm", uppercase},
		{"tasks", "delete", "--id", "12", "--project-id", "7", "--apply", "--confirm", otherToken},
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

func TestTasksDeletePreview(t *testing.T) {
	server, requests := newTaskDeleteServer(t, taskDeleteServerOptions{})
	defer server.Close()
	configureTest(t, server.URL, taskDeleteToken)

	wantToken := taskDeleteTokenFor(t, 12, 7)
	var stdout, stderr bytes.Buffer
	args := []string{"tasks", "delete", "--id", "12", "--project-id", "7"}
	if code := run(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
	}
	var preview map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &preview); err != nil {
		t.Fatalf("preview is not JSON: %v", err)
	}
	want := map[string]any{
		"mode":               "preview",
		"operation":          "delete",
		"task_id":            float64(12),
		"project_id":         float64(7),
		"title":              "Task 12",
		"confirmation_token": wantToken,
		"maximum_affected":   float64(1),
	}
	for key, value := range want {
		if !jsonEqual(preview[key], value) {
			t.Errorf("preview[%q] = %#v, want %#v", key, preview[key], value)
		}
	}
	next, ok := preview["next"].(string)
	if !ok || !strings.Contains(next, "--apply") || !strings.Contains(next, wantToken) {
		t.Errorf("preview next-step instruction = %#v", preview["next"])
	}
	if _, exposed := preview["description"]; exposed {
		t.Errorf("preview exposed description: %#v", preview)
	}
	for _, secret := range []string{taskDeleteToken, "Authorization", "base-secret", server.URL, "description"} {
		if strings.Contains(stdout.String(), secret) {
			t.Errorf("preview leaked %q: %q", secret, stdout.String())
		}
	}
	if len(*requests) != 1 {
		t.Fatalf("requests = %#v, want exactly 1 GET", *requests)
	}
	got := (*requests)[0]
	if got.method != http.MethodGet || got.path != "/api/v2/tasks/12" {
		t.Errorf("request = %s %s, want GET /api/v2/tasks/12", got.method, got.path)
	}
}

func TestTasksDeletePreviewPreflightGETFailureDeletesNothing(t *testing.T) {
	server, requests := newTaskDeleteServer(t, taskDeleteServerOptions{getStatus: http.StatusNotFound})
	defer server.Close()
	configureTest(t, server.URL, taskDeleteToken)

	var stdout, stderr bytes.Buffer
	args := []string{"tasks", "delete", "--id", "12", "--project-id", "7"}
	if code := run(args, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "任务或项目不存在") {
		t.Fatalf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
	}
	if len(*requests) != 1 || (*requests)[0].method != http.MethodGet {
		t.Errorf("requests = %#v, want exactly 1 GET and no DELETE", *requests)
	}
}

func TestTasksDeletePreflightScopeMismatchPreventsDelete(t *testing.T) {
	for _, body := range []string{
		`{"id":999,"title":"wrong task","project_id":7,"description":"` + taskDeleteToken + `"}`,
		`{"id":12,"title":"Task 12","project_id":8}`,
	} {
		server, requests := newTaskDeleteServer(t, taskDeleteServerOptions{getBody: body})
		configureTest(t, server.URL+"?base-secret#fragment-secret", taskDeleteToken)

		token := taskDeleteTokenFor(t, 12, 7)
		var stdout, stderr bytes.Buffer
		args := applyTaskDeleteArgs(12, 7, token)
		if code := run(args, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "预检失败") {
			t.Errorf("run(%v) body %s = %d, stdout %q, stderr %q", args, body, code, stdout.String(), stderr.String())
		}
		for _, request := range *requests {
			if request.method != http.MethodGet {
				t.Errorf("unexpected write request %s %s after scope mismatch", request.method, request.path)
			}
		}
		for _, secret := range []string{taskDeleteToken, "Authorization", "base-secret", "fragment-secret", "wrong task"} {
			if strings.Contains(stderr.String(), secret) {
				t.Errorf("stderr leaked %q: %q", secret, stderr.String())
			}
		}
		server.Close()
	}
}

func TestTasksDeleteApplySuccess(t *testing.T) {
	server, requests := newTaskDeleteServer(t, taskDeleteServerOptions{})
	defer server.Close()
	configureTest(t, server.URL, taskDeleteToken)

	token := taskDeleteTokenFor(t, 12, 7)
	var stdout, stderr bytes.Buffer
	args := applyTaskDeleteArgs(12, 7, token)
	if code := run(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
	}

	// Exactly preflight GET, one bodyless DELETE, one readback GET.
	if len(*requests) != 3 {
		t.Fatalf("requests = %#v, want GET + DELETE + GET", *requests)
	}
	if got := (*requests)[0]; got.method != http.MethodGet || got.path != "/api/v2/tasks/12" {
		t.Errorf("preflight = %s %s", got.method, got.path)
	}
	deleteReq := (*requests)[1]
	if deleteReq.method != http.MethodDelete || deleteReq.path != "/api/v2/tasks/12" || deleteReq.rawBody != "" {
		t.Errorf("delete = %s %s body %q, want bodyless DELETE /api/v2/tasks/12", deleteReq.method, deleteReq.path, deleteReq.rawBody)
	}
	if got := (*requests)[2]; got.method != http.MethodGet || got.path != "/api/v2/tasks/12" {
		t.Errorf("readback = %s %s", got.method, got.path)
	}

	var output map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("apply output is not JSON: %v", err)
	}
	// Non-causal contract: the result states only that the server acknowledged
	// the delete with HTTP 204 and that the current readback is not_found. It
	// must never claim that this command deleted the task.
	want := map[string]any{
		"mode":               "apply",
		"operation":          "delete",
		"task_id":            float64(12),
		"project_id":         float64(7),
		"maximum_affected":   float64(1),
		"confirmation_token": token,
		"delete_status":      float64(204),
		"readback":           "not_found",
	}
	for key, value := range want {
		if !jsonEqual(output[key], value) {
			t.Errorf("output[%q] = %#v, want %#v", key, output[key], value)
		}
	}
	note, ok := output["note"].(string)
	if !ok || !strings.Contains(note, "HTTP 204") || !strings.Contains(note, "404") || !strings.Contains(note, "不能据此推断") {
		t.Errorf("output note = %#v, want a non-causal 204 + readback-404 statement", output["note"])
	}
	if _, present := output["deletion_verified"]; present {
		t.Errorf("output must not contain deletion_verified: %#v", output)
	}
	for _, causal := range []string{"verified", "已验证", "已删除"} {
		if strings.Contains(stdout.String(), causal) {
			t.Errorf("output made a causal/verified claim %q: %q", causal, stdout.String())
		}
	}
	for _, secret := range []string{taskDeleteToken, "Authorization", "description"} {
		if strings.Contains(stdout.String(), secret) {
			t.Errorf("output leaked %q: %q", secret, stdout.String())
		}
	}
}

func TestTasksDeleteApplyReadbackStillPresent(t *testing.T) {
	server, requests := newTaskDeleteServer(t, taskDeleteServerOptions{afterGetStatus: http.StatusOK})
	defer server.Close()
	configureTest(t, server.URL, taskDeleteToken)

	var stdout, stderr bytes.Buffer
	args := applyTaskDeleteArgs(12, 7, taskDeleteTokenFor(t, 12, 7))
	if code := run(args, &stdout, &stderr); code != 1 || stdout.Len() != 0 ||
		!strings.Contains(stderr.String(), "HTTP 204") || !strings.Contains(stderr.String(), "删除未验证") {
		t.Fatalf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
	}
	if len(*requests) != 3 {
		t.Errorf("requests = %#v, want exactly GET + DELETE + GET", *requests)
	}
}

func TestTasksDeleteApplyReadbackUnverified(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusInternalServerError} {
		server, requests := newTaskDeleteServer(t, taskDeleteServerOptions{afterGetStatus: status})
		configureTest(t, server.URL+"?base-secret#fragment-secret", taskDeleteToken)

		var stdout, stderr bytes.Buffer
		args := applyTaskDeleteArgs(12, 7, taskDeleteTokenFor(t, 12, 7))
		if code := run(args, &stdout, &stderr); code != 1 || stdout.Len() != 0 ||
			!strings.Contains(stderr.String(), "删除已获服务端确认（HTTP 204），但回读验证未完成") {
			t.Errorf("readback %d: run = %d, stdout %q, stderr %q", status, code, stdout.String(), stderr.String())
		}
		if len(*requests) != 3 {
			t.Errorf("readback %d: requests = %#v, want exactly GET + DELETE + GET", status, *requests)
		}
		for _, secret := range []string{taskDeleteToken, "Authorization", "base-secret", "fragment-secret"} {
			if strings.Contains(stderr.String(), secret) {
				t.Errorf("readback %d: stderr leaked %q: %q", status, secret, stderr.String())
			}
		}
		server.Close()
	}
}

func TestTasksDeleteApplyReadbackTransportError(t *testing.T) {
	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			gets++
			if gets > 1 {
				if hijacker, ok := w.(http.Hijacker); ok {
					if conn, _, err := hijacker.Hijack(); err == nil {
						_ = conn.Close()
					}
				}
				return
			}
			_, _ = w.Write([]byte(`{"id":12,"title":"Task 12","project_id":7}`))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	configureTest(t, server.URL+"?base-secret#fragment-secret", taskDeleteToken)

	var stdout, stderr bytes.Buffer
	args := applyTaskDeleteArgs(12, 7, taskDeleteTokenFor(t, 12, 7))
	if code := run(args, &stdout, &stderr); code != 1 || stdout.Len() != 0 ||
		!strings.Contains(stderr.String(), "回读验证未完成（回读网络错误）") {
		t.Fatalf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
	}
	for _, secret := range []string{taskDeleteToken, "Authorization", "base-secret", "fragment-secret"} {
		if strings.Contains(stderr.String(), secret) {
			t.Errorf("stderr leaked %q: %q", secret, stderr.String())
		}
	}
}

func TestTasksDeleteStatusMatrixSingleReadbackNoRetry(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusForbidden, "权限不足：任务删除被拒绝（HTTP 403）"},
		{http.StatusNotFound, "删除请求返回 404：任务不存在"},
		{http.StatusConflict, "删除请求被服务端明确拒绝（HTTP 409）"},
		{http.StatusTooManyRequests, "请求受限"},
		{http.StatusInternalServerError, "服务端错误（HTTP 500）：删除结果可能未知，请勿盲目重试"},
	}
	for _, tc := range cases {
		server, requests := newTaskDeleteServer(t, taskDeleteServerOptions{deleteStatus: tc.status})
		configureTest(t, server.URL+"?base-secret#fragment-secret", taskDeleteToken)

		var stdout, stderr bytes.Buffer
		args := applyTaskDeleteArgs(12, 7, taskDeleteTokenFor(t, 12, 7))
		if code := run(args, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), tc.want) {
			t.Errorf("status %d: run = %d, stdout %q, stderr %q, want %q", tc.status, code, stdout.String(), stderr.String(), tc.want)
		}
		if !strings.Contains(stderr.String(), "回读显示任务仍存在") {
			t.Errorf("status %d: stderr %q missing reconciliation state", tc.status, stderr.String())
		}
		// Exactly preflight GET + one DELETE + one reconciliation GET; no retry.
		if len(*requests) != 3 {
			t.Errorf("status %d: requests = %#v, want GET + DELETE + GET", tc.status, *requests)
		}
		deletes := 0
		for _, request := range *requests {
			if request.method == http.MethodDelete {
				deletes++
			}
		}
		if deletes != 1 {
			t.Errorf("status %d: DELETE count = %d, want exactly 1", tc.status, deletes)
		}
		for _, secret := range []string{taskDeleteToken, "Authorization", "base-secret", "fragment-secret", "server detail"} {
			if strings.Contains(stderr.String(), secret) {
				t.Errorf("status %d: stderr leaked %q: %q", tc.status, secret, stderr.String())
			}
		}
		server.Close()
	}
}

func TestTasksDeleteReconciliation404ClaimsNoCausality(t *testing.T) {
	for _, deleteStatus := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError} {
		server, _ := newTaskDeleteServer(t, taskDeleteServerOptions{deleteStatus: deleteStatus, failedGetStatus: http.StatusNotFound})
		configureTest(t, server.URL, taskDeleteToken)

		var stdout, stderr bytes.Buffer
		args := applyTaskDeleteArgs(12, 7, taskDeleteTokenFor(t, 12, 7))
		if code := run(args, &stdout, &stderr); code != 1 || stdout.Len() != 0 {
			t.Errorf("delete %d: run = %d, stdout %q, stderr %q", deleteStatus, code, stdout.String(), stderr.String())
		}
		if !strings.Contains(stderr.String(), "回读返回 404") || !strings.Contains(stderr.String(), "不能据此推断任务已被删除") {
			t.Errorf("delete %d: stderr %q must state the 404 fact without a causal claim", deleteStatus, stderr.String())
		}
		server.Close()
	}
}

func TestTasksDeleteTransportErrorOutcomeUnknown(t *testing.T) {
	requests := &[]taskDeleteRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record := taskDeleteRequest{method: r.Method, path: r.URL.Path}
		*requests = append(*requests, record)
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"id":12,"title":"Task 12","project_id":7}`))
			return
		}
		// Abort the DELETE connection so the client sees a transport error
		// whose server-side outcome is unknown.
		if hijacker, ok := w.(http.Hijacker); ok {
			if conn, _, err := hijacker.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
	}))
	defer server.Close()
	configureTest(t, server.URL+"?base-secret#fragment-secret", taskDeleteToken)

	var stdout, stderr bytes.Buffer
	args := applyTaskDeleteArgs(12, 7, taskDeleteTokenFor(t, 12, 7))
	if code := run(args, &stdout, &stderr); code != 1 || stdout.Len() != 0 ||
		!strings.Contains(stderr.String(), "删除请求结果可能未知，请勿盲目重试") ||
		!strings.Contains(stderr.String(), "回读显示任务仍存在") {
		t.Fatalf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
	}
	deletes, gets := 0, 0
	for _, request := range *requests {
		switch request.method {
		case http.MethodDelete:
			deletes++
		case http.MethodGet:
			gets++
		}
	}
	if deletes != 1 || gets != 2 {
		t.Errorf("requests = %#v, want 2 GETs (preflight + reconciliation) and exactly 1 DELETE", *requests)
	}
	for _, secret := range []string{taskDeleteToken, "Authorization", "base-secret", "fragment-secret"} {
		if strings.Contains(stderr.String(), secret) {
			t.Errorf("stderr leaked %q: %q", secret, stderr.String())
		}
	}
}

func TestTasksDeleteConfirmationTokenIsStable(t *testing.T) {
	first, err := taskDeleteConfirmationToken(12, 7)
	if err != nil {
		t.Fatalf("taskDeleteConfirmationToken: %v", err)
	}
	second, err := taskDeleteConfirmationToken(12, 7)
	if err != nil || first != second {
		t.Fatalf("token not deterministic: %q vs %q (err %v)", first, second, err)
	}
	if !validTaskDeleteConfirmationFormat(first) {
		t.Errorf("token format = %q", first)
	}
	if other, _ := taskDeleteConfirmationToken(13, 7); other == first {
		t.Errorf("token does not depend on task id")
	}
	if other, _ := taskDeleteConfirmationToken(12, 8); other == first {
		t.Errorf("token does not depend on project id")
	}
	for _, invalid := range []string{"", "sha256:", first + "a", strings.ToUpper(first), "md5:" + first[7:]} {
		if validTaskDeleteConfirmationFormat(invalid) {
			t.Errorf("validTaskDeleteConfirmationFormat(%q) = true", invalid)
		}
	}
}
