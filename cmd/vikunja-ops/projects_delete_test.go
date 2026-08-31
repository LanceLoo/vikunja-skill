package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

type deleteRequest struct{ method, path string }

func previewToken(t *testing.T, id int64) string {
	t.Helper()
	var out bytes.Buffer
	if code := runProjectsDelete([]string{"1"}, &out, io.Discard); code != 0 {
		t.Fatalf("preview code %d", code)
	}
	var v struct {
		Token string `json:"confirmation_token"`
	}
	if err := json.Unmarshal(out.Bytes(), &v); err != nil || v.Token == "" {
		t.Fatalf("preview token: %v %s", err, out.String())
	}
	return v.Token
}

func deleteScopeHandler(t *testing.T, defaultID int64, child bool, taskID int64, requests *[]deleteRequest) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*requests = append(*requests, deleteRequest{r.Method, r.URL.Path})
		switch r.URL.Path {
		case "/api/v2/projects":
			if r.URL.RawQuery != "page=1&per_page=1000&is_archived=true" {
				t.Errorf("project query %q", r.URL.RawQuery)
			}
			items := `[{"id":1,"parent_project_id":null},{"id":9,"parent_project_id":null}]`
			total := 2
			if child {
				items = `[{"id":1,"parent_project_id":null},{"id":2,"parent_project_id":1},{"id":9,"parent_project_id":null}]`
				total = 3
			}
			_, _ = w.Write([]byte(`{"items":` + items + `,"total":` + strconv.Itoa(total) + `,"page":1,"per_page":1000,"total_pages":1}`))
		case "/api/v2/projects/1/tasks":
			_, _ = w.Write([]byte(`{"items":[{"id":` + strconv.FormatInt(taskID, 10) + `,"project_id":1}],"total":1,"page":1,"per_page":1000,"total_pages":1}`))
		case "/api/v2/projects/2/tasks":
			_, _ = w.Write([]byte(`{"items":[],"total":0,"page":1,"per_page":1000,"total_pages":0}`))
		case "/api/v2/projects/1/views", "/api/v2/projects/2/views":
			project := strings.Split(r.URL.Path, "/")[4]
			_, _ = w.Write([]byte(`{"items":[],"total":0,"page":1,"per_page":1000,"total_pages":0}`))
			_ = project
		case "/api/v2/user":
			value := "null"
			if defaultID != 0 {
				value = strconv.FormatInt(defaultID, 10)
			}
			_, _ = w.Write([]byte(`{"settings":{"default_project_id":` + value + `}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}
}

func TestProjectsDeleteInvalidArgumentsMakeNoRequests(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", "secret")
	for _, args := range [][]string{{}, {"0"}, {"1", "--confirm", "sha256:" + strings.Repeat("a", 64)}, {"1", "--apply"}, {"1", "--bogus"}, {"1", "--apply", "--apply", "--confirm", "sha256:" + strings.Repeat("a", 64)}} {
		if got := runProjectsDelete(args, io.Discard, io.Discard); got != 2 {
			t.Errorf("%v: code %d", args, got)
		}
	}
	if requests != 0 {
		t.Fatalf("invalid arguments made %d requests", requests)
	}
}

func TestProjectsDeleteEmptyArraysAndApplySequence(t *testing.T) {
	requests := []string{}
	phase := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		if r.Method == http.MethodDelete {
			if r.URL.RawQuery != "" {
				t.Error("DELETE query")
			}
			if b, _ := io.ReadAll(r.Body); len(b) != 0 {
				t.Error("DELETE body")
			}
			w.WriteHeader(204)
			phase = 1
			return
		}
		if phase == 1 && r.URL.Path == "/api/v2/projects/1" {
			w.WriteHeader(404)
			return
		}
		deletePreviewFixture(t)(w, r)
	}))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", "secret")
	var preview bytes.Buffer
	if runProjectsDelete([]string{"1"}, &preview, io.Discard) != 0 {
		t.Fatal("preview")
	}
	var got struct {
		Token string `json:"confirmation_token"`
	}
	if json.Unmarshal(preview.Bytes(), &got) != nil || got.Token == "" {
		t.Fatal("token")
	}
	requests = nil
	var out bytes.Buffer
	if code := runProjectsDelete([]string{"1", "--apply", "--confirm", got.Token}, &out, io.Discard); code != 0 {
		t.Fatalf("apply %d", code)
	}
	if !strings.Contains(out.String(), "does not prove causation") {
		t.Fatal(out.String())
	}
	if len(requests) != 7 || !strings.HasPrefix(requests[len(requests)-2], "DELETE /api/v2/projects/1?") || requests[len(requests)-1] != "GET /api/v2/projects/1?" {
		t.Fatalf("order %v", requests)
	}
}

func TestProjectsDeleteStaleTokenAndMalformedScanNeverDelete(t *testing.T) {
	deletes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes++
		}
		if r.URL.Path == "/api/v2/projects" {
			_, _ = w.Write([]byte(`{"items":null,"total":0,"page":1,"per_page":1000,"total_pages":0}`))
			return
		}
		w.WriteHeader(500)
	}))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", "secret")
	if code := runProjectsDelete([]string{"1"}, io.Discard, io.Discard); code == 0 {
		t.Fatal("null items preview succeeded")
	}
	if code := runProjectsDelete([]string{"1", "--apply", "--confirm", "sha256:" + strings.Repeat("a", 64)}, io.Discard, io.Discard); code == 0 {
		t.Fatal("malformed apply succeeded")
	}
	if deletes != 0 {
		t.Fatalf("deletes %d", deletes)
	}
}

func TestProjectsDeleteDefaultRootOrDescendantRejectsWithoutDeleteOrReadback(t *testing.T) {
	for _, defaultID := range []int64{1, 2} {
		t.Run(strconv.FormatInt(defaultID, 10), func(t *testing.T) {
			requests := []deleteRequest{}
			server := httptest.NewServer(deleteScopeHandler(t, defaultID, true, 10, &requests))
			defer server.Close()
			t.Setenv("VIKUNJA_URL", server.URL)
			t.Setenv("VIKUNJA_TOKEN", "secret")
			for _, args := range [][]string{{"1"}, {"1", "--apply", "--confirm", "sha256:" + strings.Repeat("a", 64)}} {
				if code := runProjectsDelete(args, io.Discard, io.Discard); code == 0 {
					t.Fatalf("default %d qualified", defaultID)
				}
			}
			for _, r := range requests {
				if r.method == http.MethodDelete || r.path == "/api/v2/projects/1" {
					t.Fatalf("unsafe request %#v", r)
				}
			}
		})
	}
}

func TestProjectsDeleteDeepDefaultGrandchildRejectsWithoutDeleteOrReadback(t *testing.T) {
	deletes, rootReadbacks := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes++
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/api/v2/projects/1" {
			rootReadbacks++
			return
		}
		switch r.URL.Path {
		case "/api/v2/projects":
			if r.URL.RawQuery != "page=1&per_page=1000&is_archived=true" {
				t.Errorf("projects query = %q", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"items":[{"id":1,"parent_project_id":null},{"id":2,"parent_project_id":1},{"id":3,"parent_project_id":2}],"total":3,"page":1,"per_page":1000,"total_pages":1}`))
		case "/api/v2/projects/1/tasks", "/api/v2/projects/2/tasks", "/api/v2/projects/3/tasks", "/api/v2/projects/1/views", "/api/v2/projects/2/views", "/api/v2/projects/3/views":
			_, _ = w.Write([]byte(`{"items":[],"total":0,"page":1,"per_page":1000,"total_pages":0}`))
		case "/api/v2/user":
			_, _ = w.Write([]byte(`{"settings":{"default_project_id":3}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", "secret")

	for _, args := range [][]string{
		{"1"},
		{"1", "--apply", "--confirm", "sha256:" + strings.Repeat("a", 64)},
	} {
		if code := runProjectsDelete(args, io.Discard, io.Discard); code == 0 {
			t.Fatalf("deep default variant %v succeeded", args)
		}
	}
	if deletes != 0 || rootReadbacks != 0 {
		t.Fatalf("deletes=%d root readbacks=%d, want zero", deletes, rootReadbacks)
	}
}

func TestProjectsDeleteApplyDefaultOrScopeChangeRejectsWithoutAttempt(t *testing.T) {
	for name := range map[string]bool{"default": false, "scope": true} {
		t.Run(name, func(t *testing.T) {
			requests := []deleteRequest{}
			userCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v2/user" {
					userCalls++
					d := int64(9)
					if name == "default" && userCalls == 2 {
						d = 8
					}
					deleteScopeHandler(t, d, false, 10, &requests)(w, r)
					return
				}
				child := name == "scope" && userCalls > 0
				task := int64(10)
				if name == "scope" && userCalls > 0 {
					task = 11
				}
				deleteScopeHandler(t, 9, child, task, &requests)(w, r)
			}))
			defer server.Close()
			t.Setenv("VIKUNJA_URL", server.URL)
			t.Setenv("VIKUNJA_TOKEN", "secret")
			token := previewToken(t, 1)
			requests = nil
			if code := runProjectsDelete([]string{"1", "--apply", "--confirm", token}, io.Discard, io.Discard); code == 0 {
				t.Fatal("stale confirmation applied")
			}
			for _, r := range requests {
				if r.method == http.MethodDelete || r.path == "/api/v2/projects/1" {
					t.Fatalf("attempt after mismatch: %#v", r)
				}
			}
		})
	}
}

func TestProjectsDeleteNon204StillDoesOneReadbackAndFails(t *testing.T) {
	requests := []deleteRequest{}
	deleting := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, deleteRequest{r.Method, r.URL.Path})
		if r.Method == http.MethodDelete {
			deleting = true
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if deleting && r.URL.Path == "/api/v2/projects/1" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		deleteScopeHandler(t, 9, false, 10, &requests)(w, r)
	}))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", "secret")
	token := previewToken(t, 1)
	requests = nil
	var err bytes.Buffer
	if code := runProjectsDelete([]string{"1", "--apply", "--confirm", token}, io.Discard, &err); code == 0 {
		t.Fatal("500/404 succeeded")
	}
	deletes, reads := 0, 0
	for _, r := range requests {
		if r.method == http.MethodDelete {
			deletes++
		}
		if r.method == http.MethodGet && r.path == "/api/v2/projects/1" {
			reads++
		}
	}
	if deletes != 1 || reads != 1 || strings.Contains(err.String(), "success") {
		t.Fatalf("requests=%#v err=%s", requests, err.String())
	}
}

func TestProjectsDeletePreviewIsGetOnlyAndShowsQualifiedArrays(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(deletePreviewFixture(t)))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", "secret")
	var out bytes.Buffer
	if got := runProjectsDelete([]string{"1"}, &out, io.Discard); got != 0 {
		t.Fatalf("code %d", got)
	}
	for _, want := range []string{`"projects"`, `"tasks"`, `"views"`, `"buckets"`, "qualified caller-observable scope", "hidden cascades"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("preview missing %q: %s", want, out.String())
		}
	}
}

func TestProjectsDeleteChildApplyDeletesAndReadsExactTargetOnly(t *testing.T) {
	requests := []deleteRequest{}
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, deleteRequest{r.Method, r.URL.Path})
		if r.Method == http.MethodDelete {
			if r.URL.Path != "/api/v2/projects/2" {
				t.Errorf("DELETE target = %s", r.URL.Path)
			}
			deleted = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if deleted && r.Method == http.MethodGet && r.URL.Path == "/api/v2/projects/2" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.URL.Path {
		case "/api/v2/projects":
			_, _ = w.Write([]byte(`{"items":[{"id":1,"parent_project_id":null},{"id":2,"parent_project_id":1},{"id":3,"parent_project_id":2},{"id":4,"parent_project_id":1}],"total":4,"page":1,"per_page":1000,"total_pages":1}`))
		case "/api/v2/projects/2/tasks", "/api/v2/projects/3/tasks", "/api/v2/projects/2/views", "/api/v2/projects/3/views":
			_, _ = w.Write([]byte(`{"items":[],"total":0,"page":1,"per_page":1000,"total_pages":0}`))
		case "/api/v2/user":
			_, _ = w.Write([]byte(`{"settings":{"default_project_id":1}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("VIKUNJA_URL", server.URL)
	t.Setenv("VIKUNJA_TOKEN", "secret")

	var preview bytes.Buffer
	if code := runProjectsDelete([]string{"2"}, &preview, io.Discard); code != 0 {
		t.Fatalf("preview code %d", code)
	}
	var output struct {
		Projects []struct {
			ID       int64 `json:"id"`
			ParentID int64 `json:"parent_project_id"`
		} `json:"projects"`
		Token string `json:"confirmation_token"`
	}
	if err := json.Unmarshal(preview.Bytes(), &output); err != nil || output.Token == "" || len(output.Projects) != 2 || output.Projects[0].ID != 2 || output.Projects[0].ParentID != 1 || output.Projects[1].ID != 3 {
		t.Fatalf("child preview = %s (%v)", preview.String(), err)
	}
	requests = nil
	if code := runProjectsDelete([]string{"2", "--apply", "--confirm", output.Token}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("apply code %d", code)
	}
	deletes, reads := 0, 0
	for _, request := range requests {
		if request.path == "/api/v2/projects/1/tasks" || request.path == "/api/v2/projects/4/tasks" || request.path == "/api/v2/projects/1/views" || request.path == "/api/v2/projects/4/views" {
			t.Fatalf("scanned out-of-scope resource %#v", request)
		}
		if request.method == http.MethodDelete && request.path == "/api/v2/projects/2" {
			deletes++
		}
		if request.method == http.MethodGet && request.path == "/api/v2/projects/2" {
			reads++
		}
	}
	if deletes != 1 || reads != 1 {
		t.Fatalf("exact target delete/readback = %d/%d, requests=%#v", deletes, reads, requests)
	}
}

func TestProjectsDeleteChildDefaultInScopeRejectsWithoutDeleteOrReadback(t *testing.T) {
	for _, defaultID := range []int64{2, 3} {
		t.Run(strconv.FormatInt(defaultID, 10), func(t *testing.T) {
			requests := []deleteRequest{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, deleteRequest{r.Method, r.URL.Path})
				switch r.URL.Path {
				case "/api/v2/projects":
					_, _ = w.Write([]byte(`{"items":[{"id":1,"parent_project_id":null},{"id":2,"parent_project_id":1},{"id":3,"parent_project_id":2}],"total":3,"page":1,"per_page":1000,"total_pages":1}`))
				case "/api/v2/projects/2/tasks", "/api/v2/projects/3/tasks", "/api/v2/projects/2/views", "/api/v2/projects/3/views":
					_, _ = w.Write([]byte(`{"items":[],"total":0,"page":1,"per_page":1000,"total_pages":0}`))
				case "/api/v2/user":
					_, _ = w.Write([]byte(`{"settings":{"default_project_id":` + strconv.FormatInt(defaultID, 10) + `}}`))
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
			}))
			defer server.Close()
			t.Setenv("VIKUNJA_URL", server.URL)
			t.Setenv("VIKUNJA_TOKEN", "secret")
			if code := runProjectsDelete([]string{"2", "--apply", "--confirm", "sha256:" + strings.Repeat("a", 64)}, io.Discard, io.Discard); code == 0 {
				t.Fatal("in-scope default qualified")
			}
			for _, request := range requests {
				if request.method == http.MethodDelete || request.path == "/api/v2/projects/2" {
					t.Fatalf("unsafe request %#v", request)
				}
			}
		})
	}
}

func TestProjectsDeleteChildTokenRejectsObservableDriftWithoutAttempt(t *testing.T) {
	for _, drift := range []string{"parent", "default", "task", "view", "bucket"} {
		t.Run(drift, func(t *testing.T) {
			changed := false
			requests := []deleteRequest{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, deleteRequest{r.Method, r.URL.Path})
				if r.Method != http.MethodGet {
					t.Errorf("unexpected non-scan request %s %s", r.Method, r.URL.Path)
					return
				}
				if r.URL.Path == "/api/v2/projects" {
					if r.URL.RawQuery != "page=1&per_page=1000&is_archived=true" {
						t.Errorf("projects query = %q", r.URL.RawQuery)
					}
				} else if r.URL.Path != "/api/v2/user" && r.URL.RawQuery != "page=1&per_page=1000" {
					t.Errorf("resource query for %s = %q", r.URL.Path, r.URL.RawQuery)
				}

				switch r.URL.Path {
				case "/api/v2/projects":
					parent := "1"
					if changed && drift == "parent" {
						parent = "4"
					}
					_, _ = w.Write([]byte(`{"items":[{"id":1,"parent_project_id":null},{"id":2,"parent_project_id":` + parent + `},{"id":3,"parent_project_id":2},{"id":4,"parent_project_id":null},{"id":5,"parent_project_id":null},{"id":6,"parent_project_id":null}],"total":6,"page":1,"per_page":1000,"total_pages":1}`))
				case "/api/v2/projects/2/tasks":
					taskID := "20"
					if changed && drift == "task" {
						taskID = "21"
					}
					_, _ = w.Write([]byte(`{"items":[{"id":` + taskID + `,"project_id":2}],"total":1,"page":1,"per_page":1000,"total_pages":1}`))
				case "/api/v2/projects/3/tasks", "/api/v2/projects/3/views":
					_, _ = w.Write([]byte(`{"items":[],"total":0,"page":1,"per_page":1000,"total_pages":0}`))
				case "/api/v2/projects/2/views":
					items := `[{"id":30,"project_id":2}]`
					total := "1"
					if changed && drift == "view" {
						// Keep the stable view and its bucket unchanged; the added
						// second view has no buckets, making this view-only drift.
						items = `[{"id":30,"project_id":2},{"id":31,"project_id":2}]`
						total = "2"
					}
					_, _ = w.Write([]byte(`{"items":` + items + `,"total":` + total + `,"page":1,"per_page":1000,"total_pages":1}`))
				case "/api/v2/projects/2/views/30/buckets":
					bucketID := "40"
					if changed && drift == "bucket" {
						bucketID = "41"
					}
					_, _ = w.Write([]byte(`{"items":[{"id":` + bucketID + `,"project_view_id":30}],"total":1,"page":1,"per_page":1000,"total_pages":1}`))
				case "/api/v2/projects/2/views/31/buckets":
					_, _ = w.Write([]byte(`{"items":[],"total":0,"page":1,"per_page":1000,"total_pages":0}`))
				case "/api/v2/user":
					defaultID := "5"
					if changed && drift == "default" {
						defaultID = "6"
					}
					_, _ = w.Write([]byte(`{"settings":{"default_project_id":` + defaultID + `}}`))
				default:
					t.Errorf("unexpected scan request %s", r.URL.Path)
				}
			}))
			defer server.Close()
			t.Setenv("VIKUNJA_URL", server.URL)
			t.Setenv("VIKUNJA_TOKEN", "secret")

			var preview bytes.Buffer
			if code := runProjectsDelete([]string{"2"}, &preview, io.Discard); code != 0 {
				t.Fatalf("preview code %d", code)
			}
			var output struct {
				Token string `json:"confirmation_token"`
			}
			if err := json.Unmarshal(preview.Bytes(), &output); err != nil || output.Token == "" {
				t.Fatalf("preview token: %v %s", err, preview.String())
			}
			if len(requests) != 7 {
				t.Fatalf("preview request count = %d, want 7: %#v", len(requests), requests)
			}

			requests = nil
			changed = true
			var stderr bytes.Buffer
			if code := runProjectsDelete([]string{"2", "--apply", "--confirm", output.Token}, io.Discard, &stderr); code != 1 {
				t.Fatalf("apply code = %d, want 1", code)
			}
			if !strings.Contains(stderr.String(), "confirmation does not match current qualified snapshot") {
				t.Fatalf("apply did not reach token mismatch: %s", stderr.String())
			}
			wantRequests := 7
			if drift == "view" {
				wantRequests = 8 // the added view is scanned and has an empty bucket page
			}
			if len(requests) != wantRequests {
				t.Fatalf("apply scan request count = %d, want %d: %#v", len(requests), wantRequests, requests)
			}
			for _, request := range requests {
				if request.method == http.MethodDelete || (request.method == http.MethodGet && request.path == "/api/v2/projects/2") {
					t.Fatalf("attempted delete or target readback after token drift: %#v", request)
				}
			}
		})
	}
}

func deletePreviewFixture(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("non-GET %s", r.Method)
		}
		switch r.URL.Path {
		case "/api/v2/projects":
			if r.URL.RawQuery != "page=1&per_page=1000&is_archived=true" {
				t.Error(r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"items":[{"id":1,"parent_project_id":null}],"total":1,"page":1,"per_page":1000,"total_pages":1}`))
		case "/api/v2/projects/1/tasks":
			_, _ = w.Write([]byte(`{"items":[{"id":2,"project_id":1}],"total":1,"page":1,"per_page":1000,"total_pages":1}`))
		case "/api/v2/projects/1/views":
			_, _ = w.Write([]byte(`{"items":[{"id":5,"project_id":1}],"total":1,"page":1,"per_page":1000,"total_pages":1}`))
		case "/api/v2/projects/1/views/5/buckets":
			_, _ = w.Write([]byte(`{"items":[{"id":1,"project_view_id":5}],"total":1,"page":1,"per_page":1000,"total_pages":1}`))
		case "/api/v2/user":
			_, _ = w.Write([]byte(`{"settings":{"default_project_id":null}}`))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}
}
