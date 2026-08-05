package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestChecksUseExpectedEndpointsHeadersAndPathPrefix(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		switch r.URL.Path {
		case "/vikunja/api/v2/openapi.json":
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("OpenAPI Authorization = %q", got)
			}
		case "/vikunja/api/v1/token/test":
			assertBearer(t, r)
			if r.URL.RawQuery != "" {
				t.Errorf("token query = %q", r.URL.RawQuery)
			}
		case "/vikunja/api/v2/projects":
			assertBearer(t, r)
			if r.URL.RawQuery != "page=1&per_page=1" {
				t.Errorf("projects query = %q", r.URL.RawQuery)
			}
		case "/vikunja/api/v2/token/test":
			t.Error("v2 token/test must not be requested")
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	c := New(server.Client())
	base := server.URL + "/vikunja"
	if err := c.CheckOpenAPI(base); err != nil {
		t.Fatal(err)
	}
	if err := c.VerifyTokenV1(base, "fake-token"); err != nil {
		t.Fatal(err)
	}
	if err := c.CheckProjectsRead(base, "fake-token"); err != nil {
		t.Fatal(err)
	}
	if requests != 3 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestVerifyTokenV1AuthenticationStatuses(t *testing.T) {
	testAuthenticationStatuses(t, func(c *Client, base string) error { return c.VerifyTokenV1(base, "fake-token") })
}

func TestCheckProjectsReadStatuses(t *testing.T) {
	testAuthenticationStatuses(t, func(c *Client, base string) error { return c.CheckProjectsRead(base, "fake-token") })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer server.Close()
	err := New(server.Client()).CheckProjectsRead(server.URL, "fake-token")
	if err == nil || !strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "fake-token") {
		t.Fatalf("unexpected error %v", err)
	}
}

func testAuthenticationStatuses(t *testing.T, check func(*Client, string) error) {
	t.Helper()
	for _, test := range []struct {
		status int
		want   error
	}{
		{http.StatusUnauthorized, ErrUnauthorized}, {http.StatusForbidden, ErrForbidden},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status) }))
		err := check(New(server.Client()), server.URL)
		server.Close()
		if !errors.Is(err, test.want) || !errors.Is(err, ErrAuthentication) {
			t.Errorf("status %d: %v", test.status, err)
		}
	}
}

func TestTransportErrorIsSafe(t *testing.T) {
	c := New(&http.Client{Transport: roundTripError{err: errors.New("fake-token https://host/?secret")}})
	err := c.CheckProjectsRead("https://host/base?query", "fake-token")
	if err == nil || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "query") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestListProjectsRequestAndDecode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != "/vikunja/api/v2/projects" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.RawQuery != "page=2&per_page=25" {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		assertBearer(t, r)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"items":[{"id":12,"title":"One","description":"Project description","identifier":"ONE","hex_color":"ff0000","parent_project_id":0,"is_archived":false,"is_favorite":true,"position":1.5,"max_permission":2,"created":"2026-08-05T12:00:00Z","updated":"2026-08-05T13:00:00Z"}],"total":3,"page":2,"per_page":25,"total_pages":1}`))
	}))
	defer server.Close()

	got, err := New(server.Client()).ListProjects(server.URL+"/vikunja", "fake-token", 2, 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != 12 || got.Items[0].Title != "One" || got.Items[0].Description != "Project description" || got.Items[0].Identifier != "ONE" || got.Items[0].HexColor != "ff0000" || got.Items[0].ParentProjectID == nil || *got.Items[0].ParentProjectID != 0 || got.Items[0].IsArchived || !got.Items[0].IsFavorite || got.Items[0].Position != 1.5 || got.Items[0].MaxPermission != 2 || got.Items[0].Created != "2026-08-05T12:00:00Z" || got.Items[0].Updated != "2026-08-05T13:00:00Z" || got.Total != 3 || got.Page != 2 || got.PerPage != 25 || got.TotalPages != 1 {
		t.Fatalf("decoded projects = %+v", got)
	}
}

func TestListProjectsRejectsInvalidPagination(t *testing.T) {
	c := New(nil)
	for _, test := range []struct{ page, perPage int }{{0, 1}, {1, 0}, {-1, 1}, {1, 1001}} {
		if _, err := c.ListProjects("https://example.test", "fake-token", test.page, test.perPage); err == nil {
			t.Errorf("ListProjects(%d, %d) succeeded", test.page, test.perPage)
		}
	}
}

func TestProjectReadStatusesAndSafeErrors(t *testing.T) {
	for _, test := range []struct {
		status int
		want   error
	}{{http.StatusUnauthorized, ErrUnauthorized}, {http.StatusForbidden, ErrForbidden}, {http.StatusNotFound, ErrNotFound}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status) }))
		_, err := New(server.Client()).GetProject(server.URL, "fake-token", 1)
		server.Close()
		if !errors.Is(err, test.want) {
			t.Errorf("status %d: %v", test.status, err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	_, err := New(server.Client()).ListProjects(server.URL, "fake-token", 1, 1)
	server.Close()
	if err == nil || !strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "fake-token") {
		t.Fatalf("unsafe status error: %v", err)
	}
}

func TestProjectReadErrorsAreSafe(t *testing.T) {
	c := New(&http.Client{Transport: roundTripError{err: errors.New("fake-token https://host/?secret")}})
	if _, err := c.ListProjects("https://host/base?query", "fake-token", 1, 1); err == nil || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "query") {
		t.Fatalf("unsafe transport error: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"items":`)) }))
	defer server.Close()
	if _, err := New(server.Client()).ListProjects(server.URL+"?secret=fake-token", "fake-token", 1, 1); err == nil || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe decode error: %v", err)
	}
}

func TestGetProjectRequestAndDecodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/vikunja/api/v2/projects/12" {
			t.Errorf("method/path = %s %q", r.Method, r.URL.Path)
		}
		assertBearer(t, r)
		_, _ = w.Write([]byte(`{"id":12,"title":"Escaped","description":"Project description","identifier":"ESC","hex_color":"00ff00","parent_project_id":7,"is_archived":false,"is_favorite":true,"position":2,"max_permission":3,"created":"2026-08-05T12:00:00Z","updated":"2026-08-05T13:00:00Z"}`))
	}))
	defer server.Close()
	got, err := New(server.Client()).GetProject(server.URL+"/vikunja", "fake-token", 12)
	if err != nil || got.ID != 12 || got.Title != "Escaped" || got.Description != "Project description" || got.Identifier != "ESC" || got.HexColor != "00ff00" || got.ParentProjectID == nil || *got.ParentProjectID != 7 || got.IsArchived || !got.IsFavorite || got.Position != 2 || got.MaxPermission != 3 || got.Created != "2026-08-05T12:00:00Z" || got.Updated != "2026-08-05T13:00:00Z" {
		t.Fatalf("GetProject() = %+v, %v", got, err)
	}
}

func TestProjectResponsesPreserveOriginalJSON(t *testing.T) {
	listResponse := `{"items":[{"id":12,"title":"One","owner":{"id":8,"name":"Ada"},"parent_project_id":null,"unknown":{"nested":true}}],"total":1,"page":1,"per_page":50,"total_pages":1,"extra":null}`
	getResponse := `{"id":12,"title":"One","owner":{"id":8,"name":"Ada"},"parent_project_id":null,"unknown":[1,2,3]}`
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path == "/api/v2/projects" {
			_, _ = w.Write([]byte(listResponse))
			return
		}
		if r.URL.Path == "/api/v2/projects/12" {
			_, _ = w.Write([]byte(getResponse))
			return
		}
		t.Errorf("unexpected path %q", r.URL.Path)
	}))
	defer server.Close()

	list, err := New(server.Client()).ListProjects(server.URL, "fake-token", 1, 50)
	if err != nil || list.Items[0].ParentProjectID != nil {
		t.Fatalf("ListProjects() = %+v, %v", list, err)
	}
	assertJSONEquivalent(t, listResponse, list)
	project, err := New(server.Client()).GetProject(server.URL, "fake-token", 12)
	if err != nil || project.ParentProjectID != nil {
		t.Fatalf("GetProject() = %+v, %v", project, err)
	}
	assertJSONEquivalent(t, getResponse, project)
	if requests != 2 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestRedirectsAreNotFollowedAndTokenIsSafe(t *testing.T) {
	targetRequested := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/projects":
			assertBearer(t, r)
			w.Header().Set("Location", "/redirect-target?token=fake-token")
			w.WriteHeader(http.StatusFound)
		case "/redirect-target":
			targetRequested = true
			t.Error("redirect target must not be requested")
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	_, err := New(server.Client()).ListProjects(server.URL, "fake-token", 1, 1)
	if err == nil || !strings.Contains(err.Error(), "302") || strings.Contains(err.Error(), "fake-token") {
		t.Fatalf("unsafe redirect error: %v", err)
	}
	if targetRequested {
		t.Fatal("redirect target was requested")
	}
}

func TestNewDoesNotModifySuppliedHTTPClient(t *testing.T) {
	originalRedirectError := errors.New("original redirect policy")
	supplied := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return originalRedirectError
		},
	}
	_ = New(supplied)
	if err := supplied.CheckRedirect(nil, nil); !errors.Is(err, originalRedirectError) {
		t.Fatalf("supplied client redirect policy was changed: %v", err)
	}
}

func TestListTasksRequestDecodeAndPreservesJSON(t *testing.T) {
	response := `{"items":[{"id":12,"title":"A task","description":null,"done":false,"project_id":7,"extra":{"nested":null}}],"total":1,"page":2,"per_page":25,"total_pages":1,"unknown":null}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/vikunja/api/v2/projects/7/tasks" {
			t.Errorf("method/path = %s %q", r.Method, r.URL.Path)
		}
		assertBearer(t, r)
		query := r.URL.Query()
		if query.Get("page") != "2" || query.Get("per_page") != "25" || query.Get("q") != "a & b" || query.Get("filter") != "done = false" || query.Get("filter_timezone") != "Europe/Berlin" || query.Get("filter_include_nulls") != "true" || !reflect.DeepEqual(query["sort_by"], []string{"title", "created"}) || !reflect.DeepEqual(query["order_by"], []string{"asc", "desc"}) {
			t.Errorf("query = %#v", query)
		}
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	got, err := New(server.Client()).ListTasks(server.URL+"/vikunja", "fake-token", TaskListOptions{Page: 2, PerPage: 25, ProjectID: 7, Query: "a & b", Filter: "done = false", FilterTimezone: "Europe/Berlin", FilterIncludeNulls: true, SortBy: []string{"title", "created"}, OrderBy: []string{"asc", "desc"}})
	if err != nil || len(got.Items) != 1 || got.Items[0].ID != 12 || got.Items[0].Title != "A task" || got.Items[0].Done || got.Items[0].ProjectID != 7 || got.Total != 1 || got.Page != 2 || got.PerPage != 25 || got.TotalPages != 1 {
		t.Fatalf("ListTasks() = %+v, %v", got, err)
	}
	assertJSONEquivalent(t, response, got)
}

func TestListTasksDefaultPathAndValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/tasks" || r.URL.RawQuery != "page=1&per_page=1" {
			t.Errorf("path/query = %q %q", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"items":[],"total":0,"page":1,"per_page":1,"total_pages":0}`))
	}))
	defer server.Close()
	if _, err := New(server.Client()).ListTasks(server.URL, "fake-token", TaskListOptions{Page: 1, PerPage: 1}); err != nil {
		t.Fatal(err)
	}
	c := New(nil)
	for _, options := range []TaskListOptions{{Page: 0, PerPage: 1}, {Page: 1, PerPage: 0}, {Page: 1, PerPage: 1001}, {Page: 1, PerPage: 1, ProjectID: -1}, {Page: 1, PerPage: 1, SortBy: []string{"title"}}, {Page: 1, PerPage: 1, SortBy: []string{""}, OrderBy: []string{"asc"}}, {Page: 1, PerPage: 1, SortBy: []string{" \t"}, OrderBy: []string{"asc"}}, {Page: 1, PerPage: 1, SortBy: []string{"title"}, OrderBy: []string{""}}, {Page: 1, PerPage: 1, SortBy: []string{"title"}, OrderBy: []string{" \t"}}, {Page: 1, PerPage: 1, SortBy: []string{"title"}, OrderBy: []string{"sideways"}}} {
		if _, err := c.ListTasks("https://example.test", "fake-token", options); err == nil {
			t.Errorf("ListTasks(%+v) succeeded", options)
		}
	}
}

func TestListTasksRejectsInvalidSortOptionsBeforeRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()

	for _, options := range []TaskListOptions{
		{Page: 1, PerPage: 1, SortBy: []string{"title"}, OrderBy: []string{"sideways"}},
		{Page: 1, PerPage: 1, SortBy: []string{" \t"}, OrderBy: []string{"asc"}},
		{Page: 1, PerPage: 1, SortBy: []string{"title"}, OrderBy: []string{" \t"}},
	} {
		if _, err := New(server.Client()).ListTasks(server.URL, "fake-token", options); err == nil {
			t.Errorf("ListTasks(%+v) succeeded", options)
		}
	}
	if requests != 0 {
		t.Errorf("invalid sort options made %d HTTP requests, want 0", requests)
	}
}

func TestListTasksResponseStatuses(t *testing.T) {
	for _, test := range []struct {
		status int
		want   error
	}{
		{http.StatusUnauthorized, ErrUnauthorized},
		{http.StatusForbidden, ErrForbidden},
		{http.StatusNotFound, ErrNotFound},
	} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
			}))
			defer server.Close()

			_, err := New(server.Client()).ListTasks(server.URL, "fake-token", TaskListOptions{Page: 1, PerPage: 1})
			if !errors.Is(err, test.want) {
				t.Errorf("ListTasks() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestGetTaskStatusesValidationAndRedirectSafety(t *testing.T) {
	for _, id := range []int64{-1, 0} {
		if _, err := New(nil).GetTask("https://example.test", "fake-token", id); err == nil || !strings.Contains(err.Error(), "positive") {
			t.Fatalf("GetTask(%d) error = %v, want positive-id validation error", id, err)
		}
	}
	for _, test := range []struct {
		status int
		want   error
	}{{http.StatusUnauthorized, ErrUnauthorized}, {http.StatusForbidden, ErrForbidden}, {http.StatusNotFound, ErrNotFound}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status) }))
		_, err := New(server.Client()).GetTask(server.URL, "fake-token", 1)
		server.Close()
		if !errors.Is(err, test.want) {
			t.Errorf("status %d: %v", test.status, err)
		}
	}
	targetRequested := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/tasks":
			assertBearer(t, r)
			w.Header().Set("Location", "/redirect-target?token=fake-token")
			w.WriteHeader(http.StatusFound)
		case "/redirect-target":
			targetRequested = true
		}
	}))
	defer server.Close()
	_, err := New(server.Client()).ListTasks(server.URL, "fake-token", TaskListOptions{Page: 1, PerPage: 1})
	if err == nil || !strings.Contains(err.Error(), "302") || strings.Contains(err.Error(), "fake-token") || targetRequested {
		t.Fatalf("unsafe redirect result: %v, target requested: %t", err, targetRequested)
	}
}

func TestGetTaskRequestAndDecode(t *testing.T) {
	response := `{"id":1,"title":"Task","description":"Details","done":true,"project_id":9,"created":"2026-08-05T12:00:00Z","updated":"2026-08-05T13:00:00Z","unknown":null}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/vikunja/api/v2/tasks/1" || r.URL.RawQuery != "" {
			t.Errorf("method/path/query = %s %q %q", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		assertBearer(t, r)
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	got, err := New(server.Client()).GetTask(server.URL+"/vikunja", "fake-token", 1)
	if err != nil || got.ID != 1 || got.Title != "Task" || got.Description != "Details" || !got.Done || got.ProjectID != 9 || got.Created != "2026-08-05T12:00:00Z" || got.Updated != "2026-08-05T13:00:00Z" {
		t.Fatalf("GetTask() = %+v, %v", got, err)
	}
	assertJSONEquivalent(t, response, got)
}

func TestTaskReadErrorsAreSafe(t *testing.T) {
	c := New(&http.Client{Transport: roundTripError{err: errors.New("fake-token https://host/?secret")}})
	if _, err := c.ListTasks("https://host/base?query", "fake-token", TaskListOptions{Page: 1, PerPage: 1}); err == nil || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "query") {
		t.Fatalf("unsafe transport error: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer server.Close()
	if _, err := New(server.Client()).GetTask(server.URL, "fake-token", 1); err == nil || !strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "fake-token") {
		t.Fatalf("unsafe status error: %v", err)
	}
}

func assertJSONEquivalent(t *testing.T, want string, got any) {
	t.Helper()
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var wantValue, gotValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &gotValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("JSON = %s, want semantic equivalent of %s", encoded, want)
	}
}

func assertBearer(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer fake-token" {
		t.Errorf("Authorization = %q", got)
	}
}

type roundTripError struct{ err error }

func (r roundTripError) RoundTrip(*http.Request) (*http.Response, error) { return nil, r.err }
