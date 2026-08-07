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

func TestDeleteTaskRequestMethodPathAndBodyless(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBearer(t, r)
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodDelete || r.URL.Path != "/vikunja/api/v2/tasks/8" || r.URL.RawQuery != "" || len(body) != 0 {
			t.Errorf("delete = %s %s?%s body %q", r.Method, r.URL.Path, r.URL.RawQuery, body)
		}
		if r.Header.Get("Content-Type") != "" {
			t.Errorf("Content-Type = %q, want empty for bodyless DELETE", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	if err := New(server.Client()).DeleteTask(context.Background(), server.URL+"/vikunja", "fake-token", 8); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteTaskValidateBeforeRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	c := New(server.Client())
	tests := []error{
		func() error { return c.DeleteTask(context.Background(), server.URL, "fake-token", 0) }(),
		func() error { return c.DeleteTask(context.Background(), server.URL, "fake-token", -1) }(),
		func() error { return c.DeleteTask(nil, server.URL, "fake-token", 1) }(),
	}
	for _, err := range tests {
		if err == nil {
			t.Error("invalid delete succeeded")
		}
	}
	if requests != 0 {
		t.Errorf("invalid deletes made %d requests", requests)
	}
}

func TestDeleteTaskStatusClassification(t *testing.T) {
	for _, test := range []struct {
		status int
		want   error
	}{{http.StatusUnauthorized, ErrUnauthorized}, {http.StatusForbidden, ErrForbidden}, {http.StatusNotFound, ErrNotFound}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(test.status) }))
		err := New(server.Client()).DeleteTask(context.Background(), server.URL, "fake-token", 1)
		server.Close()
		if !errors.Is(err, test.want) {
			t.Errorf("status %d: %v", test.status, err)
		}
	}
	// Explicit 4xx, 5xx, and any non-204 2xx must be StatusError, never success.
	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusConflict, http.StatusTooManyRequests, http.StatusInternalServerError} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"fake-token https://host/?secret"}`))
		}))
		err := New(server.Client()).DeleteTask(context.Background(), server.URL, "fake-token", 1)
		server.Close()
		var statusErr *StatusError
		if !errors.As(err, &statusErr) || statusErr.StatusCode != status {
			t.Errorf("status %d: err = %v, want *StatusError with StatusCode %d", status, err, status)
		}
		if err == nil || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "secret") {
			t.Errorf("status %d: unsafe error %v", status, err)
		}
	}
}

func TestDeleteTaskTransportAndRedirectSafety(t *testing.T) {
	c := New(&http.Client{Transport: roundTripError{err: errors.New("fake-token https://host/?secret")}})
	if err := c.DeleteTask(context.Background(), "https://host/base?query", "fake-token", 1); err == nil || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "query") {
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
	err := New(server.Client()).DeleteTask(context.Background(), server.URL, "fake-token", 1)
	if err == nil || !strings.Contains(err.Error(), "302") || strings.Contains(err.Error(), "fake-token") || targetRequested {
		t.Fatalf("unsafe redirect result: %v, target requested: %t", err, targetRequested)
	}
}
