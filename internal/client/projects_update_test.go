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

func TestProjectTitleUpdateValidationMakesNoRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	c := New(server.Client())
	for _, call := range []func() error{
		func() error { _, err := c.GetProjectTitleUpdateSnapshot(server.URL, "fake-token", 0); return err },
		func() error { _, err := c.UpdateProjectTitle(nil, server.URL, "fake-token", 1, "title"); return err },
		func() error {
			_, err := c.UpdateProjectTitle(context.Background(), server.URL, "fake-token", 0, "title")
			return err
		},
		func() error {
			_, err := c.UpdateProjectTitle(context.Background(), server.URL, "fake-token", 1, " \t")
			return err
		},
		func() error {
			_, err := c.UpdateProjectTitle(context.Background(), server.URL, "fake-token", 1, strings.Repeat("界", 251))
			return err
		},
	} {
		if call() == nil {
			t.Error("invalid operation succeeded")
		}
	}
	if requests != 0 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestProjectTitleUpdateSnapshotGetStrictlyDecodes(t *testing.T) {
	for _, test := range []struct {
		name, body string
		root       bool
	}{
		{"null parent", `{"id":12,"title":"Root","parent_project_id":null,"unknown":true}`, true},
		{"zero parent", `{"id":12,"title":"Root","parent_project_id":0}`, true},
		{"child parent", `{"id":12,"title":"Child","parent_project_id":7}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v2/projects/12" || r.URL.RawQuery != "" {
					t.Errorf("request = %s %q %q", r.Method, r.URL.Path, r.URL.RawQuery)
				}
				assertBearer(t, r)
				_, _ = w.Write([]byte(test.body))
			}))
			got, err := New(server.Client()).GetProjectTitleUpdateSnapshot(server.URL, "fake-token", 12)
			server.Close()
			if err != nil || got.ID != 12 || got.IsRoot != test.root {
				t.Fatalf("snapshot=%+v err=%v", got, err)
			}
			assertJSONEquivalent(t, test.body, got.Project)
		})
	}
}

func TestProjectTitleUpdatePatchRequestAndResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/vikunja/api/v2/projects/12" || r.URL.RawQuery != "" {
			t.Errorf("request = %s %q %q", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		if r.Header.Get("Content-Type") != "application/merge-patch+json" {
			t.Errorf("content type = %q", r.Header.Get("Content-Type"))
		}
		assertBearer(t, r)
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"title":"Updated"}` {
			t.Errorf("body = %s", body)
		}
		_, _ = w.Write([]byte(`{"id":12,"title":"Updated","parent_project_id":null,"unknown":true}`))
	}))
	defer server.Close()
	got, err := New(server.Client()).UpdateProjectTitle(context.Background(), server.URL+"/vikunja", "fake-token", 12, "  Updated  ")
	if err != nil || got.ID != 12 || got.Title != "Updated" || !got.IsRoot {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	assertJSONEquivalent(t, `{"id":12,"title":"Updated","parent_project_id":null,"unknown":true}`, got.Project)
}

func TestProjectTitleUpdateStrictResponsesReject(t *testing.T) {
	invalid := []string{
		`{`, `null`, `"project"`, `[]`, `{"id":12,"title":"Title","parent_project_id":null} {}`,
		`{"title":"Title","parent_project_id":null}`, `{"id":null,"title":"Title","parent_project_id":null}`, `{"id":0,"title":"Title","parent_project_id":null}`, `{"id":-1,"title":"Title","parent_project_id":null}`, `{"id":12.5,"title":"Title","parent_project_id":null}`, `{"id":"12","title":"Title","parent_project_id":null}`, `{"id":9223372036854775808,"title":"Title","parent_project_id":null}`, `{"id":12,"id":12,"title":"Title","parent_project_id":null}`,
		`{"id":12,"parent_project_id":null}`, `{"id":12,"title":null,"parent_project_id":null}`, `{"id":12,"title":1,"parent_project_id":null}`, `{"id":12,"title":"Title","title":"Title","parent_project_id":null}`, `{"id":12,"title":"Wrong","parent_project_id":null}`,
		`{"id":12,"title":"Title"}`, `{"id":12,"title":"Title","parent_project_id":"7"}`, `{"id":12,"title":"Title","parent_project_id":-1}`, `{"id":12,"title":"Title","parent_project_id":1.5}`, `{"id":12,"title":"Title","parent_project_id":9223372036854775808}`, `{"id":12,"title":"Title","parent_project_id":null,"parent_project_id":null}`,
		`{"id":999,"title":"Title","parent_project_id":null}`,
	}
	for _, mode := range []string{"get", "patch"} {
		for _, body := range invalid {
			if mode == "get" && body == `{"id":12,"title":"Wrong","parent_project_id":null}` {
				continue
			}
			t.Run(mode, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
				var err error
				if mode == "get" {
					_, err = New(server.Client()).GetProjectTitleUpdateSnapshot(server.URL, "fake-token", 12)
				} else {
					_, err = New(server.Client()).UpdateProjectTitle(context.Background(), server.URL, "fake-token", 12, "Title")
				}
				server.Close()
				if err == nil || err.Error() != "cannot decode project title update snapshot response" {
					t.Errorf("body %q: %v", body, err)
				}
			})
		}
	}
}

func TestProjectTitleUpdateStatusesAndSafeFailures(t *testing.T) {
	for _, mode := range []string{"get", "patch"} {
		for _, test := range []struct {
			status int
			want   error
		}{{http.StatusUnauthorized, ErrUnauthorized}, {http.StatusForbidden, ErrForbidden}, {http.StatusNotFound, ErrNotFound}, {http.StatusCreated, nil}, {http.StatusAccepted, nil}, {http.StatusNoContent, nil}} {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte("body-secret"))
			}))
			var err error
			if mode == "get" {
				_, err = New(server.Client()).GetProjectTitleUpdateSnapshot(server.URL+"?query-secret#fragment-secret", "token-secret", 12)
			} else {
				_, err = New(server.Client()).UpdateProjectTitle(context.Background(), server.URL+"?query-secret#fragment-secret", "token-secret", 12, "title-secret")
			}
			server.Close()
			if err == nil || strings.Contains(err.Error(), "token-secret") || strings.Contains(err.Error(), "title-secret") || strings.Contains(err.Error(), "query-secret") || strings.Contains(err.Error(), "fragment-secret") || strings.Contains(err.Error(), "body-secret") {
				t.Errorf("%s %d unsafe error: %v", mode, test.status, err)
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Errorf("%s %d: %v", mode, test.status, err)
			}
		}
	}
	c := New(&http.Client{Transport: roundTripError{err: errors.New("token-secret https://host/?query-secret#fragment-secret")}})
	if _, err := c.UpdateProjectTitle(context.Background(), "https://host/base?query-secret#fragment-secret", "token-secret", 12, "title-secret"); err == nil || strings.Contains(err.Error(), "token-secret") || strings.Contains(err.Error(), "title-secret") || strings.Contains(err.Error(), "query-secret") || strings.Contains(err.Error(), "fragment-secret") {
		t.Fatalf("unsafe transport error: %v", err)
	}
}

func TestProjectTitleUpdateRedirectsAreNotFollowed(t *testing.T) {
	for _, mode := range []string{"get", "patch"} {
		targetRequested := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v2/projects/12" {
				w.Header().Set("Location", "/target?token=token-secret")
				w.WriteHeader(http.StatusFound)
				return
			}
			targetRequested = true
			if r.Header.Get("Authorization") != "" {
				t.Error("token reached redirect target")
			}
		}))
		var err error
		if mode == "get" {
			_, err = New(server.Client()).GetProjectTitleUpdateSnapshot(server.URL, "token-secret", 12)
		} else {
			_, err = New(server.Client()).UpdateProjectTitle(context.Background(), server.URL, "token-secret", 12, "title-secret")
		}
		server.Close()
		if err == nil || !strings.Contains(err.Error(), "302") || strings.Contains(err.Error(), "token-secret") || strings.Contains(err.Error(), "title-secret") || targetRequested {
			t.Errorf("%s redirect err=%v target=%t", mode, err, targetRequested)
		}
	}
}
