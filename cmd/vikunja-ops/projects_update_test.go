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

func TestProjectsUpdateLocalValidationAndFlagLookingTitles(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	configureTest(t, server.URL, "secret")
	for _, args := range [][]string{
		{"projects", "update", "--help"},
	} {
		var out, err bytes.Buffer
		if code := run(args, &out, &err); code != 0 || out.Len() == 0 || err.Len() != 0 {
			t.Errorf("help %v: %d %q %q", args, code, out.String(), err.String())
		}
	}
	for _, args := range [][]string{
		{"projects", "update", "0", "--title", "x"}, {"projects", "update", "77"}, {"projects", "update", "77", "--title", " "},
		{"projects", "update", "77", "--title=x", "-title=y"}, {"projects", "update", "77", "--title", "x", "--apply", "-apply"},
		{"projects", "update", "77", "--title", "x", "--confirm", "no"},
	} {
		var out, err bytes.Buffer
		if code := run(args, &out, &err); code != 2 || out.Len() != 0 {
			t.Errorf("invalid %v: %d %q %q", args, code, out.String(), err.String())
		}
	}
	if requests != 0 {
		t.Errorf("local forms requested %d times", requests)
	}
	// The values after --title are data even when they look like options.
	for _, value := range []string{"--apply", "--title", "--confirm"} {
		if !projectUpdateDuplicateFlags([]string{"--title", value}) { // false means accepted by scanner
			continue
		}
		t.Errorf("flag-looking title %q treated as duplicate", value)
	}
}

func TestProjectsUpdatePreviewNoopAndSuccess(t *testing.T) {
	for _, tc := range []struct {
		name, current string
		apply         bool
		want          int
	}{{"preview", "Old", false, 1}, {"noop", "New", true, 1}, {"success", "Old", true, 3}} {
		t.Run(tc.name, func(t *testing.T) {
			requests := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				switch len(requests) {
				case 1:
					_, _ = w.Write([]byte(`{"id":77,"title":"` + tc.current + `","parent_project_id":0}`))
				case 2:
					if r.Method != http.MethodPatch || r.URL.Path != "/api/v2/projects/77" {
						t.Errorf("patch %s %s", r.Method, r.URL.Path)
					}
					body, _ := io.ReadAll(r.Body)
					if string(body) != `{"title":"New"}` {
						t.Errorf("patch body %q", body)
					}
					_, _ = w.Write([]byte(`{"id":77,"title":"New","parent_project_id":0}`))
				case 3:
					_, _ = w.Write([]byte(`{"id":77,"title":"New","parent_project_id":0}`))
				}
			}))
			defer server.Close()
			configureTest(t, server.URL, "secret")
			args := []string{"projects", "update", "77", "--title", "New"}
			if tc.apply {
				args = append(args, "--apply")
			}
			var out, err bytes.Buffer
			if code := run(args, &out, &err); code != 0 || err.Len() != 0 || len(requests) != tc.want {
				t.Fatalf("code=%d req=%v stdout=%q stderr=%q", code, requests, out.String(), err.String())
			}
			var result map[string]any
			_ = json.Unmarshal(out.Bytes(), &result)
			if tc.name == "preview" && !jsonEqual(result["next_args"], []any{"projects", "update", "77", "--title=New", "--apply"}) {
				t.Errorf("preview %#v", result)
			}
			if result["default_project_status"] != "unknown_not_exposed" {
				t.Errorf("default status %#v", result)
			}
		})
	}
}

func TestProjectsUpdatePreflightAndPostAttemptFailures(t *testing.T) {
	for _, tc := range []struct {
		name               string
		first, patch, last int
	}{
		{"child-preflight", http.StatusOK, 0, 0}, {"preflight-500", http.StatusInternalServerError, 0, 0}, {"patch-500", http.StatusOK, http.StatusInternalServerError, http.StatusOK}, {"readback-500", http.StatusOK, http.StatusOK, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests == 1 {
					w.WriteHeader(tc.first)
					if tc.first == http.StatusOK {
						body := `{"id":77,"title":"Old","parent_project_id":0}`
						if tc.name == "child-preflight" {
							body = `{"id":77,"title":"Old","parent_project_id":3}`
						}
						_, _ = w.Write([]byte(body))
					}
					return
				}
				if requests == 2 {
					w.WriteHeader(tc.patch)
					if tc.patch == http.StatusOK {
						_, _ = w.Write([]byte(`{"id":77,"title":"New","parent_project_id":0}`))
					}
					return
				}
				w.WriteHeader(tc.last)
			}))
			defer server.Close()
			configureTest(t, server.URL, "secret")
			var out, err bytes.Buffer
			if code := run([]string{"projects", "update", "77", "--title", "New", "--apply"}, &out, &err); code != 1 || out.Len() != 0 {
				t.Fatalf("code=%d req=%d stdout=%q stderr=%q", code, requests, out.String(), err.String())
			}
			want := 1
			if tc.patch != 0 {
				want = 3
			}
			if requests != want {
				t.Errorf("requests=%d, want %d", requests, want)
			}
			if strings.Contains(err.String(), "secret") || strings.Contains(err.String(), "Old") || strings.Contains(err.String(), "New") {
				t.Errorf("unsafe stderr %q", err.String())
			}
		})
	}
}

func TestProjectsUpdateUntrustedPatchCannotBeProvedByMatchingReadback(t *testing.T) {
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch len(requests) {
		case 1:
			// A null parent is also a root project and must pass strict preflight.
			_, _ = w.Write([]byte(`{"id":77,"title":"Old","parent_project_id":null}`))
		case 2:
			// Accepted HTTP but malformed/untrusted PATCH response.
			_, _ = w.Write([]byte(`{"id":77,"title":"unexpected","parent_project_id":null}`))
		case 3:
			// A matching current state cannot establish that this PATCH caused it.
			_, _ = w.Write([]byte(`{"id":77,"title":"New","parent_project_id":null}`))
		}
	}))
	defer server.Close()
	configureTest(t, server.URL+"?url-secret#fragment-secret", "token-secret")
	var out, err bytes.Buffer
	if code := run([]string{"projects", "update", "77", "--title", "New", "--apply"}, &out, &err); code != 1 || out.Len() != 0 || len(requests) != 3 {
		t.Fatalf("code=%d requests=%v stdout=%q stderr=%q", code, requests, out.String(), err.String())
	}
	if strings.Contains(err.String(), "token-secret") || strings.Contains(err.String(), "url-secret") || strings.Contains(err.String(), "fragment-secret") || strings.Contains(err.String(), "unexpected") {
		t.Errorf("unsafe stderr %q", err.String())
	}
}
