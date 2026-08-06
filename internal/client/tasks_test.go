package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

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
