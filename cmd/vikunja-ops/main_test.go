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
