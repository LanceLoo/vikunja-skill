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

const bulkUpdateToken = "test-secret-token"

// newBulkUpdateServer records requests and serves ascending task GETs plus a
// single bulk PUT. PUT handling can be customized via putStatus/putBody.
type bulkUpdateRequest struct {
	method, path string
	body         map[string]any
	rawBody      string
}

func newBulkUpdateServer(t *testing.T, putStatus int, putBody string, failGET map[string]int) (*httptest.Server, *[]bulkUpdateRequest) {
	t.Helper()
	return newBulkUpdateServerWithGETBodies(t, putStatus, putBody, failGET, nil)
}

// newBulkUpdateServerWithGETBodies additionally allows overriding the JSON
// body returned for specific GET paths (e.g. a mismatched task ID).
func newBulkUpdateServerWithGETBodies(t *testing.T, putStatus int, putBody string, failGET map[string]int, getBodies map[string]string) (*httptest.Server, *[]bulkUpdateRequest) {
	t.Helper()
	requests := &[]bulkUpdateRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+bulkUpdateToken {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		record := bulkUpdateRequest{method: r.Method, path: r.URL.Path}
		if r.Method != http.MethodGet {
			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r.Body)
			record.rawBody = buf.String()
			_ = json.Unmarshal(buf.Bytes(), &record.body)
		}
		*requests = append(*requests, record)
		if status, fail := failGET[r.URL.Path]; fail && r.Method == http.MethodGet {
			w.WriteHeader(status)
			return
		}
		if body, override := getBodies[r.URL.Path]; override && r.Method == http.MethodGet {
			_, _ = w.Write([]byte(body))
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v2/tasks/"):
			var id int
			if _, err := fmt.Sscanf(strings.TrimPrefix(r.URL.Path, "/api/v2/tasks/"), "%d", &id); err != nil {
				t.Errorf("unexpected GET path %q", r.URL.Path)
			}
			_, _ = w.Write([]byte(fmt.Sprintf(`{"id":%d,"title":"Task %d","done":false}`, id, id)))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v2/tasks/bulk":
			w.WriteHeader(putStatus)
			_, _ = w.Write([]byte(putBody))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	return server, requests
}

func bulkUpdateTokenForArgs(t *testing.T, changes map[string]any, ids []int64) string {
	t.Helper()
	tc := taskChanges{}
	if v, ok := changes["title"].(string); ok {
		tc.Title = &v
	}
	if v, ok := changes["description"].(string); ok {
		tc.Description = &v
	}
	if v, ok := changes["priority"].(int); ok {
		tc.Priority = &v
	}
	if v, ok := changes["due_date"].(string); ok {
		tc.DueDate = &v
	}
	token, err := bulkUpdateConfirmationToken(ids, tc)
	if err != nil {
		t.Fatalf("bulkUpdateConfirmationToken: %v", err)
	}
	return token
}

func TestTasksBulkUpdateHelp(t *testing.T) {
	for _, args := range [][]string{{"tasks", "bulk-update", "--help"}, {"tasks", "--help"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 {
			t.Errorf("run(%v) = %d, want 0", args, code)
		}
		if !strings.Contains(stdout.String(), "bulk-update") || !strings.Contains(stdout.String(), "--confirm") || stderr.Len() != 0 {
			t.Errorf("run(%v): stdout %q stderr %q", args, stdout.String(), stderr.String())
		}
	}
}

func TestTasksBulkUpdateInvalidArgumentsDoNotRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	configureTest(t, server.URL, bulkUpdateToken)

	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = fmt.Sprint(i + 1)
	}
	validToken := bulkUpdateTokenForArgs(t, map[string]any{"title": "New"}, []int64{1, 2})

	cases := [][]string{
		{"tasks", "bulk-update"},
		{"tasks", "bulk-update", "--ids", ""},
		{"tasks", "bulk-update", "--ids", "0"},
		{"tasks", "bulk-update", "--ids", "-1"},
		{"tasks", "bulk-update", "--ids", "abc"},
		{"tasks", "bulk-update", "--ids", "1,abc,3"},
		{"tasks", "bulk-update", "--ids", "9223372036854775808"},
		{"tasks", "bulk-update", "--ids", "1,2,2"},
		{"tasks", "bulk-update", "--ids", "1,,2"},
		{"tasks", "bulk-update", "--ids", " 1,2"},
		{"tasks", "bulk-update", "--ids", strings.Join(tooMany, ",")},
		{"tasks", "bulk-update", "12", "--title", "New"},         // positional IDs unsupported
		{"tasks", "bulk-update", "--ids", "1,2"},                 // no change field
		{"tasks", "bulk-update", "--ids", "1,2", "--title", " "}, // invalid title
		{"tasks", "bulk-update", "--ids", "1,2", "--priority", "-1"},
		{"tasks", "bulk-update", "--ids", "1,2", "--due-date", "invalid"},
		{"tasks", "bulk-update", "--ids", "1,2", "--title", "New", "extra"},
		// apply/confirm gating: none of these may load config or send requests.
		{"tasks", "bulk-update", "--ids", "1,2", "--title", "New", "--apply"},
		{"tasks", "bulk-update", "--ids", "1,2", "--title", "New", "--confirm", validToken},
		{"tasks", "bulk-update", "--ids", "1,2", "--title", "New", "--apply", "--confirm", ""},
		{"tasks", "bulk-update", "--ids", "1,2", "--title", "New", "--apply", "--confirm", "sha256:wrong"},
		{"tasks", "bulk-update", "--ids", "2,1", "--title", "Other", "--apply", "--confirm", validToken},
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

func TestTasksBulkUpdatePreview(t *testing.T) {
	server, requests := newBulkUpdateServer(t, http.StatusOK, `{}`, nil)
	defer server.Close()
	configureTest(t, server.URL, bulkUpdateToken)

	wantToken := bulkUpdateTokenForArgs(t, map[string]any{"title": "New", "priority": 3}, []int64{3, 12, 56})

	// Different ID order and flag order must normalize to the same plan/token.
	argSets := [][]string{
		{"tasks", "bulk-update", "--ids", "56,3,12", "--title", "New", "--priority", "3"},
		{"--pretty", "tasks", "bulk-update", "--priority", "3", "--title", "New", "--ids", "12,56,3"},
	}
	for _, args := range argSets {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
			t.Fatalf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
		}
		var preview map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &preview); err != nil {
			t.Fatalf("preview is not JSON: %v", err)
		}
		want := map[string]any{
			"mode":               "preview",
			"operation":          "bulk_update",
			"target_ids":         []any{float64(3), float64(12), float64(56)},
			"requested_count":    float64(3),
			"ready_count":        float64(3),
			"changes":            map[string]any{"title": "New", "priority": float64(3)},
			"confirmation_token": wantToken,
			"failure_strategy":   "server_atomic_batch",
		}
		for key, value := range want {
			if !jsonEqual(preview[key], value) {
				t.Errorf("preview[%q] = %#v, want %#v", key, preview[key], value)
			}
		}
		tasks, ok := preview["tasks"].([]any)
		if !ok || len(tasks) != 3 {
			t.Fatalf("preview tasks = %#v", preview["tasks"])
		}
		for i, id := range []float64{3, 12, 56} {
			if tasks[i].(map[string]any)["id"] != id {
				t.Errorf("preview tasks[%d] = %#v, want id %v", i, tasks[i], id)
			}
		}
	}
	if len(*requests) != 6 {
		t.Fatalf("requests = %#v, want 6 GETs", *requests)
	}
	for i, id := range []string{"3", "12", "56", "3", "12", "56"} {
		got := (*requests)[i]
		if got.method != http.MethodGet || got.path != "/api/v2/tasks/"+id {
			t.Errorf("request %d = %s %s, want GET /api/v2/tasks/%s", i, got.method, got.path, id)
		}
	}
}

func TestTasksBulkUpdatePreviewGETFailureWritesNothing(t *testing.T) {
	server, requests := newBulkUpdateServer(t, http.StatusOK, `{}`, map[string]int{"/api/v2/tasks/12": http.StatusNotFound})
	defer server.Close()
	configureTest(t, server.URL, bulkUpdateToken)

	var stdout, stderr bytes.Buffer
	args := []string{"tasks", "bulk-update", "--ids", "3,12,56", "--title", "New"}
	if code := run(args, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "任务或项目不存在") {
		t.Fatalf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
	}
	for _, request := range *requests {
		if request.method != http.MethodGet {
			t.Errorf("unexpected write request %s %s", request.method, request.path)
		}
	}
}

func TestTasksBulkUpdateApply(t *testing.T) {
	putBody := `{"task_ids":[3,12,56],"fields":["title","priority"],"values":{"title":"New","priority":3},"tasks":[{"id":3,"title":"New"},{"id":12,"title":"New"}]}`
	server, requests := newBulkUpdateServer(t, http.StatusOK, putBody, nil)
	defer server.Close()
	configureTest(t, server.URL, bulkUpdateToken)

	changes := map[string]any{"title": "New", "priority": 3}
	token := bulkUpdateTokenForArgs(t, changes, []int64{3, 12, 56})

	var stdout, stderr bytes.Buffer
	args := []string{"tasks", "bulk-update", "--ids", "56,3,12", "--title", "New", "--priority", "3", "--apply", "--confirm", token}
	if code := run(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
	}

	// Exactly the ascending re-reads followed by a single PUT.
	if len(*requests) != 4 {
		t.Fatalf("requests = %#v, want 3 GETs + 1 PUT", *requests)
	}
	for i, id := range []string{"3", "12", "56"} {
		if got := (*requests)[i]; got.method != http.MethodGet || got.path != "/api/v2/tasks/"+id {
			t.Errorf("request %d = %s %s, want GET /api/v2/tasks/%s", i, got.method, got.path, id)
		}
	}
	put := (*requests)[3]
	if put.method != http.MethodPut || put.path != "/api/v2/tasks/bulk" {
		t.Fatalf("write request = %s %s", put.method, put.path)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(put.rawBody), &raw); err != nil {
		t.Fatalf("PUT body is not JSON: %v", err)
	}
	if len(raw) != 3 {
		t.Errorf("PUT body keys = %#v, want exactly task_ids, fields, values", put.rawBody)
	}
	wantBody := map[string]any{
		"task_ids": []any{float64(3), float64(12), float64(56)},
		"fields":   []any{"title", "priority"},
		"values":   map[string]any{"title": "New", "priority": float64(3)},
	}
	if !jsonEqual(put.body, wantBody) {
		t.Errorf("PUT body = %#v, want %#v", put.body, wantBody)
	}

	var output map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("apply output is not JSON: %v", err)
	}
	want := map[string]any{
		"mode":               "apply",
		"operation":          "bulk_update",
		"target_ids":         []any{float64(3), float64(12), float64(56)},
		"requested_count":    float64(3),
		"ready_count":        float64(3),
		"succeeded_count":    float64(3), // a single bulk HTTP 200 means the whole requested batch succeeded
		"returned_count":     float64(2), // transparently len(result.Tasks) as returned by the server
		"changes":            map[string]any{"title": "New", "priority": float64(3)},
		"confirmation_token": token,
		"failure_strategy":   "server_atomic_batch",
	}
	for key, value := range want {
		if !jsonEqual(output[key], value) {
			t.Errorf("output[%q] = %#v, want %#v", key, output[key], value)
		}
	}
	if tasks, ok := output["tasks"].([]any); !ok || len(tasks) != 2 {
		t.Errorf("output tasks = %#v", output["tasks"])
	}
}

func TestTasksBulkUpdateApplyServerOmitsTasks(t *testing.T) {
	server, _ := newBulkUpdateServer(t, http.StatusOK, `{"task_ids":[3],"fields":["description"],"values":{"description":"Done"}}`, nil)
	defer server.Close()
	configureTest(t, server.URL, bulkUpdateToken)

	token := bulkUpdateTokenForArgs(t, map[string]any{"description": "Done"}, []int64{3})
	var stdout, stderr bytes.Buffer
	args := []string{"tasks", "bulk-update", "--ids", "3", "--description", "Done", "--apply", "--confirm", token}
	if code := run(args, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
	}
	var output map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("apply output is not JSON: %v", err)
	}
	if output["succeeded_count"] != float64(1) {
		t.Errorf("succeeded_count = %#v, want requested count 1 even when server omits tasks", output["succeeded_count"])
	}
	if output["returned_count"] != float64(0) {
		t.Errorf("returned_count = %#v, want 0 when server omits tasks", output["returned_count"])
	}
}

func TestTasksBulkUpdateApplyPreflightGETFailureZeroPUT(t *testing.T) {
	server, requests := newBulkUpdateServer(t, http.StatusOK, `{}`, map[string]int{"/api/v2/tasks/56": http.StatusForbidden})
	defer server.Close()
	configureTest(t, server.URL, bulkUpdateToken)

	token := bulkUpdateTokenForArgs(t, map[string]any{"title": "New"}, []int64{3, 12, 56})
	var stdout, stderr bytes.Buffer
	args := []string{"tasks", "bulk-update", "--ids", "3,12,56", "--title", "New", "--apply", "--confirm", token}
	if code := run(args, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "权限不足") {
		t.Fatalf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
	}
	for _, request := range *requests {
		if request.method != http.MethodGet {
			t.Errorf("unexpected write request %s %s after preflight failure", request.method, request.path)
		}
	}
}

func TestTasksBulkUpdateBulkForbiddenIsSafe(t *testing.T) {
	server, requests := newBulkUpdateServer(t, http.StatusForbidden, ``, nil)
	defer server.Close()
	configureTest(t, server.URL+"?base-secret#fragment-secret", bulkUpdateToken)

	token := bulkUpdateTokenForArgs(t, map[string]any{"title": "New"}, []int64{3})
	var stdout, stderr bytes.Buffer
	args := []string{"tasks", "bulk-update", "--ids", "3", "--title", "New", "--apply", "--confirm", token}
	if code := run(args, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "权限不足：任务批量更新被拒绝") {
		t.Fatalf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
	}
	for _, secret := range []string{bulkUpdateToken, "Authorization", "base-secret", "fragment-secret", server.URL} {
		if strings.Contains(stderr.String(), secret) {
			t.Errorf("stderr leaked %q: %q", secret, stderr.String())
		}
	}
	puts := 0
	for _, request := range *requests {
		if request.method == http.MethodPut {
			puts++
		}
	}
	if puts != 1 {
		t.Errorf("PUT count = %d, want exactly 1 server-atomic batch attempt", puts)
	}
}

func TestTasksBulkUpdateTransportErrorIsUnknownAndSafe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"id":3,"title":"Task 3"}`))
			return
		}
		// Abort the PUT connection so the client sees a transport error whose
		// server-side outcome is unknown.
		if hijacker, ok := w.(http.Hijacker); ok {
			if conn, _, err := hijacker.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
	}))
	defer server.Close()
	configureTest(t, server.URL+"?base-secret#fragment-secret", bulkUpdateToken)

	token := bulkUpdateTokenForArgs(t, map[string]any{"title": "New"}, []int64{3})
	var stdout, stderr bytes.Buffer
	args := []string{"tasks", "bulk-update", "--ids", "3", "--title", "New", "--apply", "--confirm", token}
	if code := run(args, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "批量请求结果可能未知，请勿盲目重试") {
		t.Fatalf("run(%v) = %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
	}
	for _, secret := range []string{bulkUpdateToken, "Authorization", "base-secret", "fragment-secret"} {
		if strings.Contains(stderr.String(), secret) {
			t.Errorf("stderr leaked %q: %q", secret, stderr.String())
		}
	}
}

func TestTasksBulkUpdatePreflightIDMismatchIsSafe(t *testing.T) {
	for _, body := range []string{
		`{"id":0,"title":"zero"}`,
		`{"id":999,"title":"wrong task","description":"` + bulkUpdateToken + `"}`,
	} {
		server, requests := newBulkUpdateServerWithGETBodies(t, http.StatusOK, `{}`, nil,
			map[string]string{"/api/v2/tasks/12": body})
		configureTest(t, server.URL+"?base-secret#fragment-secret", bulkUpdateToken)

		token := bulkUpdateTokenForArgs(t, map[string]any{"title": "New"}, []int64{3, 12, 56})
		var stdout, stderr bytes.Buffer
		args := []string{"tasks", "bulk-update", "--ids", "3,12,56", "--title", "New", "--apply", "--confirm", token}
		if code := run(args, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "预检失败") {
			t.Errorf("run(%v) body %s = %d, stdout %q, stderr %q", args, body, code, stdout.String(), stderr.String())
		}
		for _, request := range *requests {
			if request.method != http.MethodGet {
				t.Errorf("unexpected write request %s %s after ID mismatch", request.method, request.path)
			}
		}
		for _, secret := range []string{bulkUpdateToken, "Authorization", "base-secret", "fragment-secret", "wrong task"} {
			if strings.Contains(stderr.String(), secret) {
				t.Errorf("stderr leaked %q: %q", secret, stderr.String())
			}
		}
		server.Close()
	}
}

func TestTasksBulkUpdateStatusErrorClassification(t *testing.T) {
	cases := []struct {
		status  int
		want    string
		notWant string
	}{
		{http.StatusBadRequest, "被服务端明确拒绝（HTTP 400）", "未知"},
		{http.StatusConflict, "被服务端明确拒绝（HTTP 409）", "未知"},
		{http.StatusUnprocessableEntity, "被服务端明确拒绝（HTTP 422）", "未知"},
		{http.StatusTooManyRequests, "请求受限", "未知"},
		{http.StatusInternalServerError, "批量请求结果可能未知，请勿盲目重试", "明确拒绝"},
	}
	for _, tc := range cases {
		server, _ := newBulkUpdateServer(t, tc.status, `{"message":"server detail with `+bulkUpdateToken+`"}`, nil)
		configureTest(t, server.URL+"?base-secret#fragment-secret", bulkUpdateToken)

		token := bulkUpdateTokenForArgs(t, map[string]any{"title": "New"}, []int64{3})
		var stdout, stderr bytes.Buffer
		args := []string{"tasks", "bulk-update", "--ids", "3", "--title", "New", "--apply", "--confirm", token}
		if code := run(args, &stdout, &stderr); code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), tc.want) {
			t.Errorf("status %d: run = %d, stdout %q, stderr %q, want %q", tc.status, code, stdout.String(), stderr.String(), tc.want)
		}
		if tc.notWant != "" && strings.Contains(stderr.String(), tc.notWant) {
			t.Errorf("status %d: stderr %q must not contain %q", tc.status, stderr.String(), tc.notWant)
		}
		for _, secret := range []string{bulkUpdateToken, "Authorization", "base-secret", "fragment-secret", "server detail"} {
			if strings.Contains(stderr.String(), secret) {
				t.Errorf("status %d: stderr leaked %q: %q", tc.status, secret, stderr.String())
			}
		}
		server.Close()
	}
}

func TestTasksBulkUpdateConfirmationTokenIsStable(t *testing.T) {
	ids := []int64{3, 12, 56}
	changes := taskChanges{}
	title := "New"
	priority := 3
	changes.Title = &title
	changes.Priority = &priority

	first, err := bulkUpdateConfirmationToken(ids, changes)
	if err != nil {
		t.Fatalf("bulkUpdateConfirmationToken: %v", err)
	}
	second, err := bulkUpdateConfirmationToken([]int64{3, 12, 56}, changes)
	if err != nil || first != second {
		t.Fatalf("token not deterministic: %q vs %q (err %v)", first, second, err)
	}
	if !strings.HasPrefix(first, "sha256:") || len(first) != len("sha256:")+64 || first != strings.ToLower(first) {
		t.Errorf("token format = %q", first)
	}
	other, _ := bulkUpdateConfirmationToken([]int64{3, 12, 57}, changes)
	if other == first {
		t.Errorf("token does not depend on target ids")
	}
	if got := bulkUpdateFields(changes); !jsonEqual(got, []string{"title", "priority"}) {
		t.Errorf("bulkUpdateFields = %#v", got)
	}
	all := taskChanges{Title: &title, Description: &title, Priority: &priority, DueDate: &title}
	if got := bulkUpdateFields(all); !jsonEqual(got, []string{"title", "description", "priority", "due_date"}) {
		t.Errorf("canonical bulkUpdateFields = %#v", got)
	}
}
