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

func TestTaskWritesRequestsAndZeroValues(t *testing.T) {
	empty, dueDate, zero := "", "2026-08-31T12:00:00Z", 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBearer(t, r)
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/vikunja/api/v2/projects/7/tasks":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" || string(body) != `{"title":"New","description":"","priority":0,"due_date":"2026-08-31T12:00:00Z"}` {
				t.Errorf("create = %s %q %s", r.Method, r.Header.Get("Content-Type"), body)
			}
		case "/vikunja/api/v2/tasks/8":
			if r.Method != http.MethodPatch || r.Header.Get("Content-Type") != "application/merge-patch+json" || string(body) != `{"description":"","priority":0,"due_date":"2026-08-31T12:00:00Z"}` {
				t.Errorf("update = %s %q %s", r.Method, r.Header.Get("Content-Type"), body)
			}
		case "/vikunja/api/v2/tasks/9":
			if r.Method != http.MethodPatch || r.Header.Get("Content-Type") != "application/merge-patch+json" || string(body) != `{"done":true}` {
				t.Errorf("complete = %s %q %s", r.Method, r.Header.Get("Content-Type"), body)
			}
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":1,"title":"result"}`))
	}))
	defer server.Close()
	c := New(server.Client())
	if _, err := c.CreateTask(context.Background(), server.URL+"/vikunja", "fake-token", 7, CreateTaskInput{Title: "New", Description: &empty, Priority: &zero, DueDate: &dueDate}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateTask(context.Background(), server.URL+"/vikunja", "fake-token", 8, UpdateTaskInput{Description: &empty, Priority: &zero, DueDate: &dueDate}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CompleteTask(context.Background(), server.URL+"/vikunja", "fake-token", 9); err != nil {
		t.Fatal(err)
	}
}

func TestTaskWritesOmitNilDueDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "due_date") {
			t.Errorf("nil due_date was sent: %s", body)
		}
		_, _ = w.Write([]byte(`{"id":1,"title":"result"}`))
	}))
	defer server.Close()
	c := New(server.Client())
	if _, err := c.CreateTask(context.Background(), server.URL, "fake-token", 1, CreateTaskInput{Title: "New"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateTask(context.Background(), server.URL, "fake-token", 1, UpdateTaskInput{Title: stringPtr("updated")}); err != nil {
		t.Fatal(err)
	}
}

func TestTaskWritesValidateBeforeRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	c := New(server.Client())
	badTitle, negative := " \t", -1
	tests := []error{
		func() error {
			_, err := c.CreateTask(context.Background(), server.URL, "fake-token", 0, CreateTaskInput{Title: "ok"})
			return err
		}(),
		func() error {
			_, err := c.CreateTask(context.Background(), server.URL, "fake-token", 1, CreateTaskInput{})
			return err
		}(),
		func() error {
			_, err := c.UpdateTask(context.Background(), server.URL, "fake-token", 0, UpdateTaskInput{Title: stringPtr("updated")})
			return err
		}(),
		func() error {
			_, err := c.UpdateTask(context.Background(), server.URL, "fake-token", 1, UpdateTaskInput{})
			return err
		}(),
		func() error {
			_, err := c.UpdateTask(context.Background(), server.URL, "fake-token", 1, UpdateTaskInput{Title: &badTitle})
			return err
		}(),
		func() error {
			_, err := c.UpdateTask(context.Background(), server.URL, "fake-token", 1, UpdateTaskInput{Priority: &negative})
			return err
		}(),
		func() error {
			_, err := c.CreateTask(context.Background(), server.URL, "fake-token", 1, CreateTaskInput{Title: "ok", DueDate: stringPtr("")})
			return err
		}(),
		func() error {
			_, err := c.UpdateTask(context.Background(), server.URL, "fake-token", 1, UpdateTaskInput{DueDate: stringPtr("not-a-date")})
			return err
		}(),
		func() error { _, err := c.CompleteTask(nil, server.URL, "fake-token", 1); return err }(),
	}
	for _, err := range tests {
		if err == nil {
			t.Error("invalid task write succeeded")
		}
	}
	if requests != 0 {
		t.Errorf("invalid writes made %d requests", requests)
	}
}

func TestTaskWritesStatusTransportAndRedirectSafety(t *testing.T) {
	for _, test := range []struct {
		status int
		want   error
	}{{http.StatusUnauthorized, ErrUnauthorized}, {http.StatusForbidden, ErrForbidden}, {http.StatusNotFound, ErrNotFound}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(test.status) }))
		_, err := New(server.Client()).CompleteTask(context.Background(), server.URL, "fake-token", 1)
		server.Close()
		if !errors.Is(err, test.want) {
			t.Errorf("status %d: %v", test.status, err)
		}
	}
	c := New(&http.Client{Transport: roundTripError{err: errors.New("fake-token https://host/?secret")}})
	if _, err := c.CompleteTask(context.Background(), "https://host/base?query", "fake-token", 1); err == nil || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "query") {
		t.Fatalf("unsafe transport error: %v", err)
	}
	targetRequested := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/tasks/1" {
			assertBearer(t, r)
			w.Header().Set("Location", "/redirect-target?token=fake-token")
			w.WriteHeader(http.StatusFound)
			return
		}
		targetRequested = true
	}))
	defer server.Close()
	_, err := New(server.Client()).CompleteTask(context.Background(), server.URL, "fake-token", 1)
	if err == nil || !strings.Contains(err.Error(), "302") || strings.Contains(err.Error(), "fake-token") || targetRequested {
		t.Fatalf("unsafe redirect result: %v, target requested: %t", err, targetRequested)
	}
}

func stringPtr(value string) *string { return &value }
