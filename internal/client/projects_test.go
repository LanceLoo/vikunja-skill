package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateProjectValidation(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	c := New(server.Client())
	for _, call := range []func() error{
		func() error { _, err := c.CreateProject(nil, server.URL, "fake-token", "Project"); return err },
		func() error {
			_, err := c.CreateProject(context.Background(), server.URL, "fake-token", " \t ")
			return err
		},
		func() error {
			_, err := c.CreateProject(context.Background(), server.URL, "fake-token", strings.Repeat("界", 251))
			return err
		},
	} {
		if call() == nil {
			t.Error("invalid project create succeeded")
		}
	}
	if requests != 0 {
		t.Fatalf("invalid creates made %d requests", requests)
	}
}

func TestCreateProjectRequestDecodeAndAcceptedStatuses(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusCreated} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/vikunja/api/v2/projects" || r.URL.RawQuery != "" {
				t.Errorf("method/path/query = %s %q %q", r.Method, r.URL.Path, r.URL.RawQuery)
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("content type = %q", r.Header.Get("Content-Type"))
			}
			assertBearer(t, r)
			body, _ := io.ReadAll(r.Body)
			if string(body) != `{"title":"New Project"}` {
				t.Errorf("body = %s", body)
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"id":12,"title":"New Project","unknown":null}`))
		}))
		project, err := New(server.Client()).CreateProject(context.Background(), server.URL+"/vikunja", "fake-token", "  New Project  ")
		server.Close()
		if err != nil || project.ID != 12 || project.Title != "New Project" {
			t.Fatalf("status %d: project=%+v err=%v", status, project, err)
		}
		assertJSONEquivalent(t, `{"id":12,"title":"New Project","unknown":null}`, project)
	}
}

func TestCreateProjectRejectsOtherStatusesAndIsSafe(t *testing.T) {
	for _, test := range []struct {
		status int
		want   error
	}{{http.StatusUnauthorized, ErrUnauthorized}, {http.StatusForbidden, ErrForbidden}, {http.StatusNotFound, ErrNotFound}, {http.StatusAccepted, nil}, {http.StatusNoContent, nil}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(test.status)
			_, _ = w.Write([]byte("response-body-secret"))
		}))
		_, err := New(server.Client()).CreateProject(context.Background(), server.URL+"?query-secret#fragment-secret", "fake-token", "title-secret")
		server.Close()
		if err == nil || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "title-secret") || strings.Contains(err.Error(), "query-secret") || strings.Contains(err.Error(), "fragment-secret") || strings.Contains(err.Error(), "response-body-secret") {
			t.Errorf("unsafe status %d error: %v", test.status, err)
		}
		if test.want != nil && !errors.Is(err, test.want) {
			t.Errorf("status %d: %v", test.status, err)
		}
	}
}

func TestCreateProjectRejectsInvalidAcceptedStatusBodies(t *testing.T) {
	invalidBodies := []string{
		`{`,
		`null`,
		`"project"`,
		`[]`,
		`{}`,
		`{"id":null}`,
		`{"id":0}`,
		`{"id":-1}`,
		`{"id":"12"}`,
		`{"id":12.5}`,
		`{"id":9223372036854775808}`,
		`{"id":12} {"id":13}`,
	}
	for _, status := range []int{http.StatusOK, http.StatusCreated} {
		for _, body := range invalidBodies {
			t.Run(http.StatusText(status), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(status)
					_, _ = w.Write([]byte(body))
				}))
				_, err := New(server.Client()).CreateProject(context.Background(), server.URL+"?query-secret#fragment-secret", "fake-token", "title-secret")
				server.Close()
				if err == nil || err.Error() != "cannot decode project create response" {
					t.Errorf("status %d body %q: %v", status, body, err)
				}
				for _, secret := range []string{"fake-token", "title-secret", "query-secret", "fragment-secret"} {
					if strings.Contains(err.Error(), secret) {
						t.Errorf("error exposes %q: %v", secret, err)
					}
				}
			})
		}
	}
}

func TestCreateProjectRedirectAndTransportErrorsAreSafe(t *testing.T) {
	targetRequested := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/projects" {
			w.Header().Set("Location", "/redirect-target?token=fake-token")
			w.WriteHeader(http.StatusFound)
			return
		}
		targetRequested = true
		if r.Header.Get("Authorization") != "" {
			t.Error("bearer token reached redirect target")
		}
	}))
	defer server.Close()
	_, err := New(server.Client()).CreateProject(context.Background(), server.URL, "fake-token", "title-secret")
	if err == nil || !strings.Contains(err.Error(), "302") || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "title-secret") || targetRequested {
		t.Fatalf("redirect error=%v target=%t", err, targetRequested)
	}
	c := New(&http.Client{Transport: roundTripError{err: errors.New("fake-token https://host/?secret")}})
	if _, err := c.CreateProject(context.Background(), "https://host/base?query-secret#fragment-secret", "fake-token", "title-secret"); err == nil || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "title-secret") || strings.Contains(err.Error(), "query-secret") || strings.Contains(err.Error(), "fragment-secret") {
		t.Fatalf("unsafe transport error: %v", err)
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
