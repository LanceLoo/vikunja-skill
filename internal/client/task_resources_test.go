package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListLabelsAndTaskLabelsV2RequestsPreserveJSON(t *testing.T) {
	responses := map[string]string{
		"/vikunja/api/v2/labels":          `{"items":[{"id":1,"title":"One","unknown":null}],"total":1,"page":2,"per_page":25,"total_pages":1,"$schema":"https://example.test/label.json","extra":null}`,
		"/vikunja/api/v2/tasks/42/labels": `{"items":[{"id":2,"title":"Two","extra":{"nested":null}}],"total":1,"page":2,"per_page":25,"total_pages":1,"$schema":"https://example.test/task-label.json","other":null}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBearer(t, r)
		if r.Method != http.MethodGet || r.URL.Query().Get("page") != "2" || r.URL.Query().Get("per_page") != "25" || r.URL.Query().Get("q") != "a & b" {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		if r.URL.Path == "/vikunja/api/v2/labels" && r.URL.Query().Get("format") != "html" {
			t.Errorf("labels format = %q", r.URL.Query().Get("format"))
		}
		if r.URL.Path == "/vikunja/api/v2/tasks/42/labels" && r.URL.Query().Get("format") != "" {
			t.Errorf("task labels must not send format: %q", r.URL.RawQuery)
		}
		response, ok := responses[r.URL.Path]
		if !ok {
			t.Errorf("path = %q", r.URL.Path)
			return
		}
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	c := New(server.Client())
	labels, err := c.ListLabels(server.URL+"/vikunja", "fake-token", 2, 25, "a & b", "html")
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEquivalent(t, responses["/vikunja/api/v2/labels"], labels)
	taskLabels, err := c.ListTaskLabels(server.URL+"/vikunja", "fake-token", 42, 2, 25, "a & b")
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEquivalent(t, responses["/vikunja/api/v2/tasks/42/labels"], taskLabels)
}

func TestLabelRequestsValidateAndHandleSafeErrors(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++ }))
	defer server.Close()
	c := New(server.Client())
	for _, call := range []func() error{
		func() error { _, err := c.ListLabels(server.URL, "fake-token", 0, 1, "", ""); return err },
		func() error { _, err := c.ListLabels(server.URL, "fake-token", 1, 1, "", "json"); return err },
		func() error { _, err := c.ListTaskLabels(server.URL, "fake-token", 0, 1, 1, ""); return err },
		func() error { _, err := c.ListTaskLabels(server.URL, "fake-token", 1, 1, 1001, ""); return err },
	} {
		if call() == nil {
			t.Error("invalid label request succeeded")
		}
	}
	if requests != 0 {
		t.Fatalf("invalid label requests made %d HTTP requests", requests)
	}
	for _, test := range []struct {
		status int
		want   error
	}{{http.StatusUnauthorized, ErrUnauthorized}, {http.StatusForbidden, ErrForbidden}, {http.StatusNotFound, ErrNotFound}} {
		statusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status) }))
		_, err := New(statusServer.Client()).ListTaskLabels(statusServer.URL, "fake-token", 1, 1, 1, "")
		statusServer.Close()
		if !errors.Is(err, test.want) {
			t.Errorf("status %d: %v", test.status, err)
		}
	}
	c = New(&http.Client{Transport: roundTripError{err: errors.New("fake-token https://host/?secret")}})
	if _, err := c.ListLabels("https://host/base?query", "fake-token", 1, 1, "secret", "markdown"); err == nil || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "query") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe transport error: %v", err)
	}
	serviceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer serviceServer.Close()
	if _, err := New(serviceServer.Client()).ListLabels(serviceServer.URL+"?base-secret", "fake-token", 1, 1, "search-secret", ""); err == nil || !strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "base-secret") || strings.Contains(err.Error(), "search-secret") {
		t.Fatalf("unsafe service error: %v", err)
	}
}

func TestLabelRedirectsAreNotFollowedAndTokenIsSafe(t *testing.T) {
	targetRequested := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/tasks/42/labels":
			assertBearer(t, r)
			w.Header().Set("Location", "/redirect-target?token=fake-token")
			w.WriteHeader(http.StatusFound)
		case "/redirect-target":
			targetRequested = true
			t.Error("redirect target must not be requested")
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	_, err := New(server.Client()).ListTaskLabels(server.URL, "fake-token", 42, 1, 1, "search-secret")
	if err == nil || !strings.Contains(err.Error(), "302") || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "search-secret") {
		t.Fatalf("unsafe redirect error: %v", err)
	}
	if targetRequested {
		t.Fatal("redirect target was requested")
	}
}

func TestListTaskCommentsRequestPreservesJSON(t *testing.T) {
	response := `{"items":[{"id":7,"comment":null,"author":{"id":2},"unknown":{"nested":true}}],"total":1,"page":2,"per_page":25,"total_pages":1,"$schema":"https://example.test/comments.json","extra":null}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/vikunja/api/v2/tasks/42/comments" {
			t.Errorf("method/path = %s %q", r.Method, r.URL.Path)
		}
		assertBearer(t, r)
		query := r.URL.Query()
		if query.Get("page") != "2" || query.Get("per_page") != "25" || query.Get("q") != "a & b" || query.Get("order_by") != "desc" || query.Get("format") != "" {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	comments, err := New(server.Client()).ListTaskComments(server.URL+"/vikunja", "fake-token", 42, 2, 25, "a & b", "desc")
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEquivalent(t, response, comments)
}

func TestListTaskCommentsDefaultsValidationAndSafeErrors(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/v2/tasks/1/comments" || r.URL.RawQuery != "page=1&per_page=1" {
			t.Errorf("path/query = %q %q", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"items":[],"total":0,"page":1,"per_page":1,"total_pages":0}`))
	}))
	defer server.Close()
	c := New(server.Client())
	if _, err := c.ListTaskComments(server.URL, "fake-token", 1, 1, 1, "", ""); err != nil {
		t.Fatal(err)
	}
	for _, call := range []func() error{
		func() error { _, err := c.ListTaskComments(server.URL, "fake-token", 0, 1, 1, "", ""); return err },
		func() error { _, err := c.ListTaskComments(server.URL, "fake-token", 1, 0, 1, "", ""); return err },
		func() error { _, err := c.ListTaskComments(server.URL, "fake-token", 1, 1, 1001, "", ""); return err },
		func() error {
			_, err := c.ListTaskComments(server.URL, "fake-token", 1, 1, 1, "", "newest")
			return err
		},
	} {
		if call() == nil {
			t.Error("invalid task comments request succeeded")
		}
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	for _, test := range []struct {
		status int
		want   error
	}{{http.StatusUnauthorized, ErrUnauthorized}, {http.StatusForbidden, ErrForbidden}, {http.StatusNotFound, ErrNotFound}} {
		statusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status) }))
		_, err := New(statusServer.Client()).ListTaskComments(statusServer.URL, "fake-token", 1, 1, 1, "", "")
		statusServer.Close()
		if !errors.Is(err, test.want) {
			t.Errorf("status %d: %v", test.status, err)
		}
	}
	c = New(&http.Client{Transport: roundTripError{err: errors.New("fake-token https://host/?secret")}})
	if _, err := c.ListTaskComments("https://host/base?query", "fake-token", 1, 1, 1, "search-secret", "asc"); err == nil || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "query") || strings.Contains(err.Error(), "search-secret") {
		t.Fatalf("unsafe transport error: %v", err)
	}
	serviceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer serviceServer.Close()
	if _, err := New(serviceServer.Client()).ListTaskComments(serviceServer.URL+"?base-secret", "fake-token", 1, 1, 1, "search-secret", ""); err == nil || !strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "base-secret") || strings.Contains(err.Error(), "search-secret") {
		t.Fatalf("unsafe service error: %v", err)
	}
}

func TestTaskCommentRedirectsAreNotFollowedAndTokenIsSafe(t *testing.T) {
	targetRequested := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/tasks/42/comments":
			assertBearer(t, r)
			w.Header().Set("Location", "/redirect-target?token=fake-token")
			w.WriteHeader(http.StatusFound)
		case "/redirect-target":
			targetRequested = true
			t.Error("redirect target must not be requested")
		}
	}))
	defer server.Close()
	_, err := New(server.Client()).ListTaskComments(server.URL, "fake-token", 42, 1, 1, "search-secret", "")
	if err == nil || !strings.Contains(err.Error(), "302") || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "search-secret") || targetRequested {
		t.Fatalf("unsafe redirect result: %v, target requested: %t", err, targetRequested)
	}
}

func TestListTaskAttachmentsRequestPreservesJSON(t *testing.T) {
	response := `{"items":[{"id":7,"file":null,"created_by":{"id":2},"unknown":{"nested":true}}],"total":1,"page":2,"per_page":25,"total_pages":1,"$schema":"https://example.test/attachments.json","extra":null}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/vikunja/api/v2/tasks/42/attachments" || r.URL.RawQuery != "page=2&per_page=25" {
			t.Errorf("method/path/query = %s %q %q", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		assertBearer(t, r)
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	attachments, err := New(server.Client()).ListTaskAttachments(server.URL+"/vikunja", "fake-token", 42, 2, 25)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEquivalent(t, response, attachments)
}

func TestListTaskAttachmentsValidationAndSafeErrors(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++ }))
	defer server.Close()
	c := New(server.Client())
	for _, call := range []func() error{
		func() error { _, err := c.ListTaskAttachments(server.URL, "fake-token", 0, 1, 1); return err },
		func() error { _, err := c.ListTaskAttachments(server.URL, "fake-token", 1, 0, 1); return err },
		func() error { _, err := c.ListTaskAttachments(server.URL, "fake-token", 1, 1, 0); return err },
		func() error { _, err := c.ListTaskAttachments(server.URL, "fake-token", 1, 1, 1001); return err },
	} {
		if call() == nil {
			t.Error("invalid task attachments request succeeded")
		}
	}
	if requests != 0 {
		t.Fatalf("invalid task attachments requests = %d", requests)
	}
	for _, test := range []struct {
		status int
		want   error
	}{{http.StatusUnauthorized, ErrUnauthorized}, {http.StatusForbidden, ErrForbidden}, {http.StatusNotFound, ErrNotFound}} {
		statusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status) }))
		_, err := New(statusServer.Client()).ListTaskAttachments(statusServer.URL, "fake-token", 1, 1, 1)
		statusServer.Close()
		if !errors.Is(err, test.want) {
			t.Errorf("status %d: %v", test.status, err)
		}
	}
	c = New(&http.Client{Transport: roundTripError{err: errors.New("fake-token https://host/?secret")}})
	if _, err := c.ListTaskAttachments("https://host/base?query", "fake-token", 1, 1, 1); err == nil || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "query") {
		t.Fatalf("unsafe transport error: %v", err)
	}
	serviceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer serviceServer.Close()
	if _, err := New(serviceServer.Client()).ListTaskAttachments(serviceServer.URL+"?base-secret", "fake-token", 1, 1, 1); err == nil || !strings.Contains(err.Error(), "502") || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "base-secret") {
		t.Fatalf("unsafe service error: %v", err)
	}
}

func TestTaskAttachmentRedirectsAreNotFollowedAndTokenIsSafe(t *testing.T) {
	targetRequested := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/tasks/42/attachments":
			assertBearer(t, r)
			w.Header().Set("Location", "/redirect-target?token=fake-token")
			w.WriteHeader(http.StatusFound)
		case "/redirect-target":
			targetRequested = true
			t.Error("redirect target must not be requested")
		}
	}))
	defer server.Close()
	_, err := New(server.Client()).ListTaskAttachments(server.URL, "fake-token", 42, 1, 1)
	if err == nil || !strings.Contains(err.Error(), "302") || strings.Contains(err.Error(), "fake-token") || targetRequested {
		t.Fatalf("unsafe redirect result: %v, target requested: %t", err, targetRequested)
	}
}
