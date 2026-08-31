package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestScanProjectDeleteUsesExactPerProjectTasksAndEmptyNonKanbanBuckets(t *testing.T) {
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v2/projects":
			if r.URL.RawQuery != "page=1&per_page=1000&is_archived=true" {
				t.Errorf("archived project query = %q", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"items":[{"id":1,"parent_project_id":null},{"id":2,"parent_project_id":1}],"total":2,"page":1,"per_page":1000,"total_pages":1}`))
		case "/api/v2/projects/1/tasks":
			_, _ = w.Write([]byte(`{"items":[{"id":10,"project_id":1}],"total":1,"page":1,"per_page":1000,"total_pages":1}`))
		case "/api/v2/projects/2/tasks":
			_, _ = w.Write([]byte(`{"items":[{"id":20,"project_id":2}],"total":1,"page":1,"per_page":1000,"total_pages":1}`))
		case "/api/v2/projects/1/views":
			_, _ = w.Write([]byte(`{"items":[{"id":11,"project_id":1}],"total":1,"page":1,"per_page":1000,"total_pages":1}`))
		case "/api/v2/projects/2/views":
			_, _ = w.Write([]byte(`{"items":[{"id":21,"project_id":2}],"total":1,"page":1,"per_page":1000,"total_pages":1}`))
		case "/api/v2/projects/1/views/11/buckets":
			_, _ = w.Write([]byte(`{"items":[{"id":99,"project_view_id":11}],"total":1,"page":1,"per_page":1000,"total_pages":1}`))
		case "/api/v2/projects/2/views/21/buckets":
			// Proven v2 behavior for non-kanban views: this is a valid empty page.
			_, _ = w.Write([]byte(`{"items":[],"total":0,"page":1,"per_page":1000,"total_pages":0}`))
		case "/api/v2/user":
			_, _ = w.Write([]byte(`{"settings":{"default_project_id":null}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	s, err := New(server.Client()).ScanProjectDelete(context.Background(), server.URL, "token", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Tasks) != 2 || len(s.Buckets) != 1 || s.Buckets[0].ProjectID != 1 || len(requests) != 8 {
		t.Fatalf("snapshot/requests = %+v / %v", s, requests)
	}
	if requests[1] != "GET /api/v2/projects/1/tasks?page=1&per_page=1000" || requests[4] != "GET /api/v2/projects/2/tasks?page=1&per_page=1000" {
		t.Fatalf("tasks were not scanned per-project: %v", requests)
	}
}

func TestProjectDeleteRejectsNullItemsAndMetadata(t *testing.T) {
	for _, body := range []string{
		`{"items":null,"total":0,"page":1,"per_page":1000,"total_pages":0}`,
		`{"items":[],"total":null,"page":1,"per_page":1000,"total_pages":0}`,
		`{"items":[],"total":0,"page":null,"per_page":1000,"total_pages":0}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		_, err := New(server.Client()).ScanProjectDelete(context.Background(), server.URL, "token", 1)
		server.Close()
		if err == nil {
			t.Errorf("accepted %s", body)
		}
	}
}

func TestProjectDeletePaginationRejectsChangedAndShortOrFinalMismatch(t *testing.T) {
	for name, second := range map[string]string{
		"changed-total":      `{"items":[],"total":1001,"page":2,"per_page":1000,"total_pages":2}`,
		"short-intermediate": `{"items":[],"total":1001,"page":1,"per_page":1000,"total_pages":2}`,
		"final-mismatch":     `{"items":[],"total":1001,"page":2,"per_page":1000,"total_pages":2}`,
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if name == "short-intermediate" {
					_, _ = w.Write([]byte(second))
					return
				}
				if calls == 1 {
					_, _ = w.Write([]byte(pagedProjects(1000, 1001, 1, 2, 0)))
					return
				}
				_, _ = w.Write([]byte(second))
			}))
			defer server.Close()
			if _, err := New(server.Client()).scanProjects(context.Background(), server.URL+"/api/v2", "t"); err == nil {
				t.Fatal("accepted malformed pagination")
			}
		})
	}
}

func TestProjectDeletePaginationAcceptsTwoPagesAndRejectsDuplicateIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			_, _ = w.Write([]byte(pagedProjects(1000, 1001, 1, 2, 0)))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":1001,"parent_project_id":null}],"total":1001,"page":2,"per_page":1000,"total_pages":2}`))
	}))
	defer server.Close()
	projects, err := New(server.Client()).scanProjects(context.Background(), server.URL+"/api/v2", "t")
	if err != nil || len(projects) != 1001 {
		t.Fatalf("two pages: %d %v", len(projects), err)
	}
	duplicate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"id":1,"parent_project_id":null},{"id":1,"parent_project_id":null}],"total":2,"page":1,"per_page":1000,"total_pages":1}`))
	}))
	defer duplicate.Close()
	if _, err := New(duplicate.Client()).ScanProjectDelete(context.Background(), duplicate.URL, "t", 1); err == nil {
		t.Fatal("duplicate IDs accepted")
	}
}

func pagedProjects(n, total, page, pages int, start int64) string {
	items := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			items += ","
		}
		items += fmt.Sprintf(`{"id":%d,"parent_project_id":null}`, start+int64(i)+1)
	}
	return fmt.Sprintf(`{"items":[%s],"total":%d,"page":%d,"per_page":1000,"total_pages":%d}`, items, total, page, pages)
}

func TestScanProjectDeleteRejectsMalformedEmptyPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[],"total":0,"page":1,"per_page":1000,"total_pages":1}`))
	}))
	defer server.Close()
	if _, err := New(server.Client()).ScanProjectDelete(context.Background(), server.URL, "token", 1); err == nil {
		t.Fatal("malformed empty envelope qualified")
	}
}

func TestDeleteInstanceIdentityNormalizesIPv6DefaultPortAndAPIPath(t *testing.T) {
	for raw, want := range map[string]string{
		"HTTPS://[2001:DB8::1]:443/base///": "https://[2001:db8::1]/base/api/v2",
		"http://[::1]:8080/api/v2/":         "http://[::1]:8080/api/v2",
	} {
		got, err := DeleteInstanceIdentity(raw)
		if err != nil || got != want {
			t.Errorf("%q = %q, %v; want %q", raw, got, err, want)
		}
	}
}

func TestScanProjectDeleteChildScopeAndDefaultQualification(t *testing.T) {
	for name, defaultID := range map[string]int64{
		"none": 0, "ancestor": 1, "target": 2, "descendant": 3, "missing": 99,
	} {
		t.Run(name, func(t *testing.T) {
			requests := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.URL.Path)
				switch r.URL.Path {
				case "/api/v2/projects":
					_, _ = w.Write([]byte(`{"items":[{"id":1,"parent_project_id":null},{"id":2,"parent_project_id":1},{"id":3,"parent_project_id":2},{"id":4,"parent_project_id":1}],"total":4,"page":1,"per_page":1000,"total_pages":1}`))
				case "/api/v2/projects/2/tasks", "/api/v2/projects/3/tasks", "/api/v2/projects/2/views", "/api/v2/projects/3/views":
					_, _ = w.Write([]byte(`{"items":[],"total":0,"page":1,"per_page":1000,"total_pages":0}`))
				case "/api/v2/user":
					value := "null"
					if defaultID != 0 {
						value = fmt.Sprintf("%d", defaultID)
					}
					_, _ = w.Write([]byte(`{"settings":{"default_project_id":` + value + `}}`))
				default:
					t.Errorf("unexpected resource scan %s", r.URL.Path)
				}
			}))
			defer server.Close()

			s, err := New(server.Client()).ScanProjectDelete(context.Background(), server.URL, "token", 2)
			if defaultID == 2 || defaultID == 3 || defaultID == 99 {
				if err == nil {
					t.Fatalf("default %d qualified", defaultID)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(s.Projects) != 2 || s.Projects[0] != (DeleteProject{ID: 2, ParentID: 1}) || s.Projects[1] != (DeleteProject{ID: 3, ParentID: 2}) {
				t.Fatalf("selected projects = %#v", s.Projects)
			}
			for _, path := range requests {
				if path == "/api/v2/projects/1/tasks" || path == "/api/v2/projects/4/tasks" || path == "/api/v2/projects/1/views" || path == "/api/v2/projects/4/views" {
					t.Fatalf("scanned out-of-scope resource %s", path)
				}
			}
		})
	}
}

func TestScanProjectDeleteStillRejectsInvalidCompleteGraph(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/projects" {
			t.Errorf("invalid graph should stop before %s", r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":1,"parent_project_id":null},{"id":2,"parent_project_id":1},{"id":3,"parent_project_id":4},{"id":4,"parent_project_id":3}],"total":4,"page":1,"per_page":1000,"total_pages":1}`))
	}))
	defer server.Close()
	if _, err := New(server.Client()).ScanProjectDelete(context.Background(), server.URL, "token", 2); err == nil {
		t.Fatal("cycle outside selected subtree qualified")
	}
}
