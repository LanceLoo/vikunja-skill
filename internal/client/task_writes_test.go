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

func TestBulkUpdateTasksRequestAndResponse(t *testing.T) {
	priority := 3
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBearer(t, r)
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/vikunja/api/v2/tasks/bulk" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodPut || r.Header.Get("Content-Type") != "application/json" ||
			string(body) != `{"task_ids":[8,9],"fields":["title","priority"],"values":{"title":"updated","priority":3}}` {
			t.Errorf("bulk update = %s %q %s", r.Method, r.Header.Get("Content-Type"), body)
		}
		_, _ = w.Write([]byte(`{"task_ids":[8,9],"fields":["title","priority"],"values":{"id":9,"title":"updated","done":true,"project_id":2},"tasks":[{"id":8,"title":"updated","priority":3,"project_id":2},{"id":9,"title":"updated","priority":3,"done":true,"project_id":2}]}`))
	}))
	defer server.Close()
	c := New(server.Client())
	result, err := c.BulkUpdateTasks(context.Background(), server.URL+"/vikunja", "fake-token", BulkUpdateTasksInput{
		TaskIDs: []int64{8, 9},
		Fields:  []string{"title", "priority"},
		Values:  UpdateTaskInput{Title: stringPtr("updated"), Priority: &priority},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.TaskIDs) != 2 || result.TaskIDs[0] != 8 || result.TaskIDs[1] != 9 {
		t.Errorf("result task ids = %v", result.TaskIDs)
	}
	if len(result.Fields) != 2 || result.Fields[0] != "title" || result.Fields[1] != "priority" {
		t.Errorf("result fields = %v", result.Fields)
	}
	if result.Values.ID != 9 || result.Values.Title != "updated" || !result.Values.Done || result.Values.ProjectID != 2 {
		t.Errorf("result values = %+v", result.Values)
	}
	if len(result.Tasks) != 2 {
		t.Fatalf("result tasks = %+v", result.Tasks)
	}
	if result.Tasks[0].ID != 8 || result.Tasks[0].Title != "updated" || result.Tasks[0].ProjectID != 2 {
		t.Errorf("result tasks[0] = %+v", result.Tasks[0])
	}
	if result.Tasks[1].ID != 9 || !result.Tasks[1].Done || result.Tasks[1].ProjectID != 2 {
		t.Errorf("result tasks[1] = %+v", result.Tasks[1])
	}
}

func TestBulkUpdateTasksValidateBeforeRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	c := New(server.Client())
	valid := func() BulkUpdateTasksInput {
		return BulkUpdateTasksInput{TaskIDs: []int64{1}, Fields: []string{"title"}, Values: UpdateTaskInput{Title: stringPtr("ok")}}
	}
	with := func(change func(*BulkUpdateTasksInput)) error {
		input := valid()
		change(&input)
		_, err := c.BulkUpdateTasks(context.Background(), server.URL, "fake-token", input)
		return err
	}
	tests := []error{
		with(func(i *BulkUpdateTasksInput) { i.TaskIDs = nil }),
		with(func(i *BulkUpdateTasksInput) { i.TaskIDs = []int64{0} }),
		with(func(i *BulkUpdateTasksInput) { i.TaskIDs = []int64{-1} }),
		with(func(i *BulkUpdateTasksInput) { i.TaskIDs = []int64{1, 1} }),
		with(func(i *BulkUpdateTasksInput) {
			i.TaskIDs = make([]int64, MaxBulkUpdateTaskIDs+1)
			for j := range i.TaskIDs {
				i.TaskIDs[j] = int64(j + 1)
			}
		}),
		with(func(i *BulkUpdateTasksInput) { i.Fields = nil }),
		with(func(i *BulkUpdateTasksInput) { i.Fields = []string{" "} }),
		with(func(i *BulkUpdateTasksInput) { i.Fields = []string{"title", "title"} }),
		with(func(i *BulkUpdateTasksInput) { i.Fields = []string{"done"} }),
		with(func(i *BulkUpdateTasksInput) {
			i.Fields = []string{"description"}
		}),
		with(func(i *BulkUpdateTasksInput) {
			i.Values = UpdateTaskInput{Title: stringPtr("ok"), Priority: intPtr(1)}
		}),
		with(func(i *BulkUpdateTasksInput) { i.Values = UpdateTaskInput{Title: stringPtr(" \t")} }),
		with(func(i *BulkUpdateTasksInput) {
			i.Fields = []string{"priority"}
			i.Values = UpdateTaskInput{Priority: intPtr(-1)}
		}),
		with(func(i *BulkUpdateTasksInput) {
			i.Fields = []string{"due_date"}
			i.Values = UpdateTaskInput{DueDate: stringPtr("not-a-date")}
		}),
	}
	for _, err := range tests {
		if err == nil {
			t.Error("invalid bulk update succeeded")
		}
	}
	if requests != 0 {
		t.Errorf("invalid bulk updates made %d requests", requests)
	}
}

func TestBulkUpdateTasksRejectsMoreThan100IDs(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	ids := make([]int64, MaxBulkUpdateTaskIDs+1)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	_, err := New(server.Client()).BulkUpdateTasks(context.Background(), server.URL, "fake-token", BulkUpdateTasksInput{
		TaskIDs: ids,
		Fields:  []string{"title"},
		Values:  UpdateTaskInput{Title: stringPtr("ok")},
	})
	if err == nil {
		t.Fatal("bulk update with 101 task ids succeeded")
	}
	if requests != 0 {
		t.Errorf("101-id bulk update made %d requests, want 0", requests)
	}
}

func TestBulkUpdateTasksStatusErrorClassification(t *testing.T) {
	priority := 1
	input := BulkUpdateTasksInput{TaskIDs: []int64{1}, Fields: []string{"priority"}, Values: UpdateTaskInput{Priority: &priority}}
	for _, status := range []int{http.StatusBadRequest, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusInternalServerError} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"fake-token https://host/?secret"}`))
		}))
		_, err := New(server.Client()).BulkUpdateTasks(context.Background(), server.URL, "fake-token", input)
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

func TestBulkUpdateTasksStatusTransportAndRedirectSafety(t *testing.T) {
	priority := 1
	input := BulkUpdateTasksInput{TaskIDs: []int64{1}, Fields: []string{"priority"}, Values: UpdateTaskInput{Priority: &priority}}
	for _, test := range []struct {
		status int
		want   error
	}{{http.StatusUnauthorized, ErrUnauthorized}, {http.StatusForbidden, ErrForbidden}, {http.StatusNotFound, ErrNotFound}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(test.status) }))
		_, err := New(server.Client()).BulkUpdateTasks(context.Background(), server.URL, "fake-token", input)
		server.Close()
		if !errors.Is(err, test.want) {
			t.Errorf("status %d: %v", test.status, err)
		}
	}
	c := New(&http.Client{Transport: roundTripError{err: errors.New("fake-token https://host/?secret")}})
	if _, err := c.BulkUpdateTasks(context.Background(), "https://host/base?query", "fake-token", input); err == nil || strings.Contains(err.Error(), "fake-token") || strings.Contains(err.Error(), "query") {
		t.Fatalf("unsafe transport error: %v", err)
	}
	targetRequested := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/tasks/bulk" {
			assertBearer(t, r)
			w.Header().Set("Location", "/redirect-target?token=fake-token")
			w.WriteHeader(http.StatusFound)
			return
		}
		targetRequested = true
	}))
	defer server.Close()
	_, err := New(server.Client()).BulkUpdateTasks(context.Background(), server.URL, "fake-token", input)
	if err == nil || !strings.Contains(err.Error(), "302") || strings.Contains(err.Error(), "fake-token") || targetRequested {
		t.Fatalf("unsafe redirect result: %v, target requested: %t", err, targetRequested)
	}
}

func intPtr(value int) *int { return &value }

func stringPtr(value string) *string { return &value }
