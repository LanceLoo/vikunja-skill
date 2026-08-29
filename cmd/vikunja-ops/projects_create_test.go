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

func TestProjectsCreateHelpValidationAndPreview(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", "token")
	for _, args := range [][]string{
		{"projects", "create", "--help"},
		{"projects", "--help"},
	} {
		var out, err bytes.Buffer
		if code := run(args, &out, &err); code != 0 || out.Len() == 0 || err.Len() != 0 {
			t.Errorf("help %v: code=%d stdout=%q stderr=%q", args, code, out.String(), err.String())
		}
	}
	tooLong := strings.Repeat("界", 251)
	for _, args := range [][]string{
		{"projects", "create"},
		{"projects", "create", "--title", ""},
		{"projects", "create", "--title", " \t "},
		{"projects", "create", "--title", tooLong},
		{"projects", "create", "--title", "x", "extra"},
		{"projects", "create", "--title", "x", "--confirm", "anything"},
		{"projects", "create", "--title", "x", "--unknown"},
		{"projects", "create", "--title=x", "-title=y"},
		{"projects", "create", "-title", "x", "--title=y"},
		{"projects", "create", "--title", "x", "--apply", "-apply"},
		{"projects", "create", "--title", "x", "-apply=true", "--apply=false"},
	} {
		var out, err bytes.Buffer
		if code := run(args, &out, &err); code != 2 || out.Len() != 0 || err.Len() == 0 {
			t.Errorf("invalid %v: code=%d stdout=%q stderr=%q", args, code, out.String(), err.String())
		}
	}
	var out, err bytes.Buffer
	if code := run([]string{"projects", "create", "--title", "Root"}, &out, &err); code != 0 || err.Len() != 0 || requests != 0 {
		t.Fatalf("preview code=%d requests=%d stdout=%q stderr=%q", code, requests, out.String(), err.String())
	}
	var preview map[string]any
	if json.Unmarshal(out.Bytes(), &preview) != nil || preview["mode"] != "preview" || preview["title"] != "Root" || !jsonEqual(preview["next_args"], []any{"projects", "create", "--title=Root", "--apply"}) {
		t.Errorf("preview = %q", out.String())
	}
	out.Reset()
	err.Reset()
	if code := run([]string{"projects", "create", "--title", "  Root  "}, &out, &err); code != 0 || !strings.Contains(out.String(), `"title":"Root"`) || strings.Contains(out.String(), "  Root  ") {
		t.Errorf("normalized preview code=%d stdout=%q stderr=%q", code, out.String(), err.String())
	}
}

func TestProjectsCreateFlagLookingTitleValuesAreNotFlags(t *testing.T) {
	for _, args := range [][]string{
		{"projects", "create", "--title=--apply"},
		{"projects", "create", "--title=--title"},
		{"projects", "create", "--title", "--apply"},
	} {
		var out, err bytes.Buffer
		if code := run(args, &out, &err); code != 0 || err.Len() != 0 {
			t.Errorf("flag-looking title %v: code=%d stdout=%q stderr=%q", args, code, out.String(), err.String())
		}
	}
}

func TestProjectsCreateNoTrustedIDDoesNotReconcile(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"rejected", `{"detail":"server-body-secret"}`, http.StatusConflict},
		{"accepted-malformed", `{`, http.StatusCreated},
		{"accepted-no-id", `{"title":"Root"}`, http.StatusCreated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			t.Setenv("VIKUNJA_URL", server.URL+"?url-secret#fragment-secret")
			t.Setenv("VIKUNJA_TOKEN", "credential-secret")
			var out, err bytes.Buffer
			if code := run([]string{"projects", "create", "--title", "sensitive-title", "--apply"}, &out, &err); code != 1 || out.Len() != 0 || requests != 1 {
				t.Fatalf("code=%d requests=%d stdout=%q stderr=%q", code, requests, out.String(), err.String())
			}
			for _, secret := range []string{"credential-secret", "server-body-secret", "url-secret", "fragment-secret", "sensitive-title"} {
				if strings.Contains(err.String(), secret) {
					t.Errorf("stderr leaked %q: %q", secret, err.String())
				}
			}
			if !strings.Contains(err.String(), "未获得可安全回读的项目 ID") || !strings.Contains(err.String(), "创建结果可能未知") {
				t.Errorf("no-ID failure was not conservative: %q", err.String())
			}
		})
	}
}

func TestProjectsCreateReadbackMismatch(t *testing.T) {
	for _, tc := range []struct {
		name, body string
	}{
		{"id", `{"id":78,"title":"Root"}`},
		{"title", `{"id":77,"title":"different sensitive title"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests == 1 {
					_, _ = w.Write([]byte(`{"id":77,"title":"Root"}`))
					return
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			t.Setenv("VIKUNJA_URL", server.URL)
			t.Setenv("VIKUNJA_TOKEN", "credential-secret")
			var out, err bytes.Buffer
			if code := run([]string{"projects", "create", "--title", "Root", "--apply"}, &out, &err); code != 1 || out.Len() != 0 || requests != 2 {
				t.Fatalf("code=%d requests=%d stdout=%q stderr=%q", code, requests, out.String(), err.String())
			}
			if strings.Contains(err.String(), "different sensitive title") || strings.Contains(err.String(), "credential-secret") {
				t.Errorf("unsafe stderr %q", err.String())
			}
		})
	}
}

func TestProjectsCreateApplyAndPretty(t *testing.T) {
	const token = "project-create-token"
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch len(requests) {
		case 1:
			if r.Method != http.MethodPost || r.URL.Path != "/api/v2/projects" || r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("create request = %s %s content-type=%q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
			}
			body, _ := io.ReadAll(r.Body)
			if string(body) != `{"title":"Root"}` {
				t.Errorf("create body = %q", body)
			}
			_, _ = w.Write([]byte(`{"id":77,"title":"Root"}`))
		case 2:
			if r.Method != http.MethodGet || r.URL.Path != "/api/v2/projects/77" {
				t.Errorf("readback request = %s %s", r.Method, r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"id":77,"title":"Root","unknown":null}`))
		}
	}))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", token)
	var out, err bytes.Buffer
	if code := run([]string{"--pretty", "projects", "create", "--title", "Root", "--apply"}, prettyJSONWriter{&out}, &err); code != 0 || err.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), err.String())
	}
	if strings.Join(requests, "|") != "POST /api/v2/projects|GET /api/v2/projects/77" {
		t.Errorf("requests = %v", requests)
	}
	if !strings.Contains(out.String(), "\n  \"") {
		t.Errorf("pretty output = %q", out.String())
	}
	var result map[string]any
	if json.Unmarshal(out.Bytes(), &result) != nil || result["create_status"] != "returned_project" || result["readback_status"] != "id_and_title_matched" || result["project_id"] != float64(77) {
		t.Errorf("result = %q", out.String())
	}
}

func TestProjectsCreateFailureAndReconciliationAreSafe(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		createStatus, getStatus int
	}{
		{"write-401", http.StatusUnauthorized, http.StatusOK},
		{"write-403", http.StatusForbidden, http.StatusOK},
		{"write-404", http.StatusNotFound, http.StatusOK},
		{"write-500", http.StatusInternalServerError, http.StatusOK},
		{"readback-500", http.StatusOK, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests == 1 {
					w.WriteHeader(tc.createStatus)
					if tc.createStatus == http.StatusOK {
						_, _ = w.Write([]byte(`{"id":77,"title":"Root"}`))
					} else {
						_, _ = w.Write([]byte(`{"detail":"server-body-secret"}`))
					}
					return
				}
				w.WriteHeader(tc.getStatus)
			}))
			defer server.Close()
			t.Setenv("VIKUNJA_URL", server.URL+"?url-secret#fragment-secret")
			t.Setenv("VIKUNJA_TOKEN", "credential-secret")
			var out, err bytes.Buffer
			if code := run([]string{"projects", "create", "--title", "Root", "--apply"}, &out, &err); code != 1 || out.Len() != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), err.String())
			}
			want := 1
			if tc.name == "readback-500" {
				want = 2
			}
			if requests != want {
				t.Errorf("requests = %d, want %d", requests, want)
			}
			if tc.name != "readback-500" && (!strings.Contains(err.String(), "未获得可安全回读的项目 ID") || !strings.Contains(err.String(), "创建结果可能未知")) {
				t.Errorf("write failure was not conservative: %q", err.String())
			}
			for _, secret := range []string{"credential-secret", "server-body-secret", "url-secret", "fragment-secret"} {
				if strings.Contains(err.String(), secret) {
					t.Errorf("stderr leaked %q: %q", secret, err.String())
				}
			}
		})
	}
}

func TestProjectsCreateRedirectIsRejectedWithoutCredentialLeak(t *testing.T) {
	redirected := 0
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected++ }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/captured", http.StatusFound)
	}))
	defer origin.Close()
	t.Setenv("VIKUNJA_URL", origin.URL+"?url-secret#fragment-secret")
	t.Setenv("VIKUNJA_TOKEN", "credential-secret")
	var out, err bytes.Buffer
	if code := run([]string{"projects", "create", "--title", "Root", "--apply"}, &out, &err); code != 1 || out.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), err.String())
	}
	if redirected != 0 {
		t.Errorf("redirect target received %d requests", redirected)
	}
	for _, secret := range []string{"credential-secret", "url-secret", "fragment-secret", target.URL} {
		if strings.Contains(err.String(), secret) {
			t.Errorf("stderr leaked %q: %q", secret, err.String())
		}
	}
}
