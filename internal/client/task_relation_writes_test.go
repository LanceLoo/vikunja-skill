package client

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTaskRelationKinds(t *testing.T) {
	want := map[string]string{"subtask": "parenttask", "parenttask": "subtask", "related": "related", "duplicateof": "duplicates", "duplicates": "duplicateof", "blocking": "blocked", "blocked": "blocking", "precedes": "follows", "follows": "precedes", "copiedfrom": "copiedto", "copiedto": "copiedfrom"}
	for kind, inverse := range want {
		if !ValidTaskRelationKind(kind) {
			t.Errorf("%q is invalid", kind)
		}
		if got, ok := InverseTaskRelationKind(kind); !ok || got != inverse {
			t.Errorf("inverse(%q) = %q, %t", kind, got, ok)
		}
	}
	for _, kind := range []string{"", "Related", " related", "related "} {
		if ValidTaskRelationKind(kind) {
			t.Errorf("%q is valid", kind)
		}
		if _, ok := InverseTaskRelationKind(kind); ok {
			t.Errorf("inverse(%q) exists", kind)
		}
	}
}

func TestGetTaskRelationSnapshotValidationAndQueries(t *testing.T) {
	raw := `{ "id" : 42, "related_tasks" : { "related" : [ {"id":7,"x":null} ], "blocking" : [{"id":8}] } }`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/tasks/42" {
			t.Errorf("method/path = %s %s", r.Method, r.URL.Path)
		}
		assertBearer(t, r)
		_, _ = w.Write([]byte(raw))
	}))
	defer server.Close()
	snapshot, err := New(server.Client()).GetTaskRelationSnapshot(server.URL, "fake-token", 42)
	if err != nil {
		t.Fatal(err)
	}
	if string(snapshot.Raw) != `{ "related" : [ {"id":7,"x":null} ], "blocking" : [{"id":8}] }` {
		t.Fatalf("raw = %s", snapshot.Raw)
	}
	if !snapshot.Has("related", 7) || snapshot.Has("blocking", 7) || !snapshot.HasAnyDirectRelation(8) || snapshot.HasAnyDirectRelation(9) {
		t.Fatal("unexpected query result")
	}
	if got := strings.Join(snapshot.KindsFor(8), ","); got != "blocking" {
		t.Fatalf("kinds = %q", got)
	}
	if snapshot.KindsFor(9) != nil {
		t.Fatal("missing kinds must be nil")
	}
}

func TestGetTaskRelationSnapshotEmptyAndRejectsMalformed(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"id":1,"related_tasks":{}}`))
	}))
	defer server.Close()
	c := New(server.Client())
	if snapshot, err := c.GetTaskRelationSnapshot(server.URL, "fake-token", 1); err != nil || len(snapshot.Raw) == 0 || snapshot.HasAnyDirectRelation(2) {
		t.Fatalf("empty snapshot = %#v, %v", snapshot, err)
	}
	if _, err := c.GetTaskRelationSnapshot(server.URL, "fake-token", 0); err == nil || requests != 1 {
		t.Fatalf("invalid task request err=%v requests=%d", err, requests)
	}
	malformed := []string{`{"id":42}`, `{"id":42,"related_tasks":null}`, `{"id":42,"related_tasks":[]}`, `{"id":42,"related_tasks":{"unknown":[]}}`, `{"id":42,"related_tasks":{"related":null}}`, `{"id":42,"related_tasks":{"related":{}}}`, `{"id":42,"related_tasks":{"related":[null]}}`, `{"id":42,"related_tasks":{"related":[1]}}`, `{"id":42,"related_tasks":{"related":[{}]}}`, `{"id":42,"related_tasks":{"related":[{"id":null}]}}`, `{"id":42,"related_tasks":{"related":[{"id":1.5}]}}`, `{"id":42,"related_tasks":{"related":[{"id":0}]}}`, `{"id":42,"related_tasks":{"related":[{"id":42}]}}`, `{"id":42,"related_tasks":{"related":[{"id":7},{"id":7}]}}`, `{"id":42,"related_tasks":{"related":[{"id":7}],"blocking":[{"id":7}]}}`, `{"id":42,"related_tasks":{}} {}`}
	for _, response := range malformed {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(response)) }))
		_, err := New(s.Client()).GetTaskRelationSnapshot(s.URL, "fake-token", 42)
		s.Close()
		if err == nil {
			t.Errorf("malformed snapshot accepted: %s", response)
		}
	}
	for _, response := range []string{`{"related_tasks":{}}`, `{"id":null,"related_tasks":{}}`, `{"id":"42","related_tasks":{}}`, `{"id":42.5,"related_tasks":{}}`, `{"id":0,"related_tasks":{}}`, `{"id":-1,"related_tasks":{}}`, `{"id":999,"related_tasks":{}}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(response)) }))
		_, err := New(s.Client()).GetTaskRelationSnapshot(s.URL, "fake-token", 42)
		s.Close()
		if err == nil || err.Error() != "cannot decode task relation snapshot response" {
			t.Errorf("invalid source id accepted or unstable error: %s: %v", response, err)
		}
	}
}

func TestTaskRelationWritesRequestsStatusesAndValidation(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		assertBearer(t, r)
		switch r.Method {
		case http.MethodPost:
			if r.URL.Path != "/api/v2/tasks/42/relations" || r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("post path/content type = %s %q", r.URL.Path, r.Header.Get("Content-Type"))
			}
			body, _ := io.ReadAll(r.Body)
			if string(body) != `{"other_task_id":7,"relation_kind":"related"}` {
				t.Errorf("post body = %s", body)
			}
			w.WriteHeader(http.StatusCreated)
		case http.MethodDelete:
			if r.URL.Path != "/api/v2/tasks/42/relations/related/7" {
				t.Errorf("delete path = %s", r.URL.Path)
			}
			body, _ := io.ReadAll(r.Body)
			if len(body) != 0 {
				t.Errorf("delete body = %q", body)
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	c := New(server.Client())
	input := CreateTaskRelationInput{TaskID: 42, OtherTaskID: 7, RelationKind: "related"}
	if status, err := c.CreateTaskRelation(server.URL, "fake-token", input); err != nil || status != 201 {
		t.Fatalf("create = %d, %v", status, err)
	}
	if status, err := c.DeleteTaskRelation(server.URL, "fake-token", 42, "related", 7); err != nil || status != 204 {
		t.Fatalf("delete = %d, %v", status, err)
	}
	for _, call := range []func() error{func() error {
		_, e := c.CreateTaskRelation(server.URL, "fake-token", CreateTaskRelationInput{})
		return e
	}, func() error {
		_, e := c.CreateTaskRelation(server.URL, "fake-token", CreateTaskRelationInput{TaskID: 1, OtherTaskID: 1, RelationKind: "related"})
		return e
	}, func() error { _, e := c.DeleteTaskRelation(server.URL, "fake-token", 1, "Related", 2); return e }} {
		if call() == nil {
			t.Error("invalid write succeeded")
		}
	}
	if requests != 2 {
		t.Fatalf("requests = %d", requests)
	}
	for _, tc := range []struct {
		method string
		status int
		ok     bool
	}{{http.MethodPost, 200, true}, {http.MethodPost, 201, true}, {http.MethodPost, 202, false}, {http.MethodPost, 204, false}, {http.MethodDelete, 200, true}, {http.MethodDelete, 204, true}, {http.MethodDelete, 201, false}, {http.MethodDelete, 202, false}} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status) }))
		var err error
		if tc.method == http.MethodPost {
			_, err = New(s.Client()).CreateTaskRelation(s.URL, "fake-token", input)
		} else {
			_, err = New(s.Client()).DeleteTaskRelation(s.URL, "fake-token", 42, "related", 7)
		}
		s.Close()
		if (err == nil) != tc.ok {
			t.Errorf("%s %d err=%v", tc.method, tc.status, err)
		}
	}
}

func TestTaskRelationWritesSafeErrorsAndRedirects(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusBadGateway} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("response-body-secret"))
		}))
		_, err := New(s.Client()).CreateTaskRelation(s.URL+"?base-secret#fragment-secret", "fake-token", CreateTaskRelationInput{1, 2, "related"})
		s.Close()
		if err == nil || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "base-secret") || strings.Contains(err.Error(), "fragment-secret") || strings.Contains(err.Error(), "response-body-secret") {
			t.Errorf("unsafe status error %d: %v", status, err)
		}
		if status == 401 && !errors.Is(err, ErrUnauthorized) {
			t.Errorf("401 = %v", err)
		}
	}
	targetRequested := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/tasks/1/relations" {
			w.Header().Set("Location", "/target?token=fake-token")
			w.WriteHeader(http.StatusFound)
		} else {
			targetRequested = true
		}
	}))
	defer server.Close()
	_, err := New(server.Client()).CreateTaskRelation(server.URL, "fake-token", CreateTaskRelationInput{1, 2, "related"})
	if err == nil || !strings.Contains(err.Error(), "302") || strings.Contains(err.Error(), "fake-token") || targetRequested {
		t.Fatalf("redirect err=%v target=%t", err, targetRequested)
	}
	targetRequested = false
	deleteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/tasks/1/relations/related/2" {
			w.Header().Set("Location", "/delete-target?token=fake-token")
			w.WriteHeader(http.StatusFound)
			return
		}
		targetRequested = true
		if r.Header.Get("Authorization") != "" {
			t.Error("bearer token reached redirect target")
		}
	}))
	defer deleteServer.Close()
	_, err = New(deleteServer.Client()).DeleteTaskRelation(deleteServer.URL, "fake-token", 1, "related", 2)
	if err == nil || !strings.Contains(err.Error(), "302") || strings.Contains(err.Error(), "fake-token") || targetRequested {
		t.Fatalf("delete redirect err=%v target=%t", err, targetRequested)
	}
}
