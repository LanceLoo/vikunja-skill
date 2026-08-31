package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func relationToken(t *testing.T, operation string) string {
	t.Helper()
	token, err := taskRelationConfirmationToken(operation, 12, 34, "related")
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestTaskRelationConfirmationCanonicalBinding(t *testing.T) {
	create := relationToken(t, "relation_create")
	delete := relationToken(t, "relation_delete")
	if create == delete || !validTaskDeleteConfirmationFormat(create) {
		t.Fatalf("tokens invalid or identical: %q %q", create, delete)
	}
	for _, tc := range []struct {
		operation, payload, token string
	}{
		{"relation_create", `{"version":1,"operation":"relation_create","taskID":12,"otherTaskID":34,"relationKind":"related"}`, create},
		{"relation_delete", `{"version":1,"operation":"relation_delete","taskID":12,"relationKind":"related","otherTaskID":34}`, delete},
	} {
		sum := sha256.Sum256([]byte(tc.payload))
		if want := "sha256:" + hex.EncodeToString(sum[:]); tc.token != want {
			t.Errorf("%s token = %q, want canonical %q", tc.operation, tc.token, want)
		}
	}
	for _, change := range []struct {
		other int64
		kind  string
	}{{35, "related"}, {34, "blocked"}} {
		got, err := taskRelationConfirmationToken("relation_create", 12, change.other, change.kind)
		if err != nil || got == create {
			t.Errorf("token did not bind change %#v", change)
		}
	}
}

func TestTaskRelationsWriteLocalValidationAndHelp(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	configureTest(t, server.URL, "secret")
	bad := [][]string{
		{"tasks", "relations", "create"},
		{"tasks", "relations", "create", "--task-id", "12", "--other-task-id", "12", "--relation-kind", "related"},
		{"tasks", "relations", "create", "--task-id=12", "-task-id=13", "--other-task-id", "34", "--relation-kind", "related"},
		{"tasks", "relations", "delete", "--task-id", "12", "--other-task-id", "34", "-other-task-id", "35", "--relation-kind", "related"},
		{"tasks", "relations", "create", "--task-id", "12", "--other-task-id", "34", "--relation-kind", "related", "--apply", "-apply"},
		{"tasks", "relations", "create", "--task-id", "12", "--other-task-id", "34", "--relation-kind", "related", "--confirm=x", "-confirm=y"},
		{"tasks", "relations", "create", "--task-id", "12", "--other-task-id", "34", "--relation-kind", "RELATED"},
		{"tasks", "relations", "create", "--task-id", "12", "--other-task-id", "34", "--relation-kind", "related", "--confirm", relationToken(t, "relation_create")},
		{"tasks", "relations", "create", "--task-id", "12", "--other-task-id", "34", "--relation-kind", "related", "--apply", "--confirm", "sha256:" + strings.Repeat("a", 64)},
	}
	for _, args := range bad {
		var out, err bytes.Buffer
		if code := run(args, &out, &err); code != 2 || out.Len() != 0 {
			t.Errorf("run(%v) = %d, stdout=%q stderr=%q", args, code, out.String(), err.String())
		}
	}
	for _, op := range []string{"create", "delete"} {
		var out, err bytes.Buffer
		if code := run([]string{"tasks", "relations", op, "--help"}, &out, &err); code != 0 || !strings.Contains(out.String(), "relation-kind") || err.Len() != 0 {
			t.Errorf("help %s: %d %q %q", op, code, out.String(), err.String())
		}
	}
	if requests != 0 {
		t.Errorf("local validation made %d requests", requests)
	}
}

func TestTaskRelationsWriteBadFormsPrecedeMissingConfig(t *testing.T) {
	configureTest(t, "", "")
	for _, args := range [][]string{
		{"tasks", "relations", "create", "--task-id", "0", "--other-task-id", "34", "--relation-kind", "related"},
		{"tasks", "relations", "delete", "--task-id", "12", "--other-task-id", "34", "--relation-kind", " related"},
		{"tasks", "relations", "create", "--task-id", "12", "--other-task-id", "34", "--relation-kind", "related", "--apply", "--confirm", "bad"},
	} {
		var out, err bytes.Buffer
		if code := run(args, &out, &err); code != 2 || out.Len() != 0 || strings.Contains(err.String(), "missing required configuration") {
			t.Errorf("run(%v) = %d stdout=%q stderr=%q", args, code, out.String(), err.String())
		}
	}
}

func TestTaskRelationsReadRetained(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/tasks/42" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":42,"related_tasks":{"unknown":null,"related":null,"metadata":{"x":1}}}`))
	}))
	defer server.Close()
	configureTest(t, server.URL, "secret")
	var out, err bytes.Buffer
	if code := run([]string{"tasks", "relations", "42"}, &out, &err); code != 0 || err.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), err.String())
	}
	if want := "{\"unknown\":null,\"related\":null,\"metadata\":{\"x\":1}}\n"; out.String() != want {
		t.Errorf("raw output = %q, want %q", out.String(), want)
	}
}

func TestTaskRelationPreviewAndApply(t *testing.T) {
	for _, operation := range []string{"create", "delete"} {
		t.Run(operation, func(t *testing.T) {
			requests := []string{}
			writeBody := ""
			writeSeen := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				if r.Method == http.MethodGet {
					body := `{"id":12,"related_tasks":{}}`
					if operation == "delete" && !writeSeen {
						body = `{"id":12,"related_tasks":{"related":[{"id":34}]}}`
					}
					if operation == "create" && writeSeen {
						body = `{"id":12,"related_tasks":{"related":[{"id":34}]}}`
					}
					_, _ = w.Write([]byte(body))
					return
				}
				writeSeen = true
				if operation == "create" {
					if r.Header.Get("Content-Type") != "application/json" {
						t.Error("missing content type")
					}
					body, _ := io.ReadAll(r.Body)
					writeBody = string(body)
					w.WriteHeader(http.StatusCreated)
				} else {
					if r.ContentLength != 0 {
						t.Errorf("delete content length = %d", r.ContentLength)
					}
					w.WriteHeader(http.StatusNoContent)
				}
			}))
			defer server.Close()
			configureTest(t, server.URL, "secret")
			base := []string{"tasks", "relations", operation, "--task-id", "12", "--other-task-id", "34", "--relation-kind", "related"}
			var out, err bytes.Buffer
			if code := run(base, &out, &err); code != 0 || len(requests) != 1 {
				t.Fatalf("preview code=%d req=%v stderr=%q", code, requests, err.String())
			}
			var preview map[string]any
			_ = json.Unmarshal(out.Bytes(), &preview)
			if preview["mode"] != "preview" || preview["operation"] != "relation_"+operation {
				t.Errorf("preview %#v", preview)
			}
			requests = nil
			out.Reset()
			err.Reset()
			token := relationToken(t, "relation_"+operation)
			if code := run(append(base, "--apply", "--confirm", token), &out, &err); code != 0 || len(requests) != 3 {
				t.Fatalf("apply code=%d req=%v stdout=%q stderr=%q", code, requests, out.String(), err.String())
			}
			writePath := "/api/v2/tasks/12/relations"
			writeMethod := http.MethodPost
			if operation == "delete" {
				writePath, writeMethod = "/api/v2/tasks/12/relations/related/34", http.MethodDelete
			}
			wantRequests := []string{http.MethodGet + " /api/v2/tasks/12", writeMethod + " " + writePath, http.MethodGet + " /api/v2/tasks/12"}
			if strings.Join(requests, "|") != strings.Join(wantRequests, "|") {
				t.Errorf("requests = %v, want %v", requests, wantRequests)
			}
			if operation == "create" && writeBody != `{"other_task_id":34,"relation_kind":"related"}` {
				t.Errorf("POST body = %q", writeBody)
			}
			var result map[string]any
			_ = json.Unmarshal(out.Bytes(), &result)
			if result["readback"] != map[string]string{"create": "present", "delete": "absent"}[operation] {
				t.Errorf("result %#v", result)
			}
		})
	}
}

func TestTaskRelationWriteFailureStillReconcilesWithoutLeaks(t *testing.T) {
	for _, operation := range []string{"create", "delete"} {
		t.Run(operation, func(t *testing.T) {
			requests := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				switch r.Method {
				case http.MethodGet:
					body := `{"id":12,"related_tasks":{}}`
					if operation == "delete" {
						body = `{"id":12,"related_tasks":{"related":[{"id":34}]}}`
					}
					_, _ = w.Write([]byte(body))
				default:
					w.WriteHeader(http.StatusAccepted) // client rejects this otherwise-2xx result
					_, _ = w.Write([]byte(`{"detail":"server-body-secret"}`))
				}
			}))
			defer server.Close()
			configureTest(t, server.URL+"?url-secret#fragment-secret", "credential-secret")
			args := []string{"tasks", "relations", operation, "--task-id", "12", "--other-task-id", "34", "--relation-kind", "related", "--apply", "--confirm", relationToken(t, "relation_"+operation)}
			var out, err bytes.Buffer
			if code := run(args, &out, &err); code != 1 || out.Len() != 0 || len(requests) != 3 {
				t.Fatalf("code=%d requests=%v stdout=%q stderr=%q", code, requests, out.String(), err.String())
			}
			for _, secret := range []string{"credential-secret", "server-body-secret", "url-secret", "fragment-secret", "Authorization"} {
				if strings.Contains(err.String(), secret) {
					t.Errorf("stderr leaked %q: %q", secret, err.String())
				}
			}
		})
	}
}

func TestTaskRelationAcknowledgedWriteReadbackFailureOrMismatch(t *testing.T) {
	for _, operation := range []string{"create", "delete"} {
		for _, postFailure := range []bool{false, true} {
			t.Run(operation+map[bool]string{false: "-mismatch", true: "-failure"}[postFailure], func(t *testing.T) {
				requests := []string{}
				gets := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests = append(requests, r.Method+" "+r.URL.Path)
					if r.Method == http.MethodGet {
						gets++
						if gets == 2 && postFailure {
							w.WriteHeader(http.StatusInternalServerError)
							return
						}
						body := `{"id":12,"related_tasks":{}}`
						if operation == "delete" {
							body = `{"id":12,"related_tasks":{"related":[{"id":34}]}}`
						}
						_, _ = w.Write([]byte(body))
						return
					}
					if operation == "create" {
						w.WriteHeader(http.StatusOK)
					} else {
						w.WriteHeader(http.StatusNoContent)
					}
				}))
				defer server.Close()
				configureTest(t, server.URL, "secret")
				args := []string{"tasks", "relations", operation, "--task-id", "12", "--other-task-id", "34", "--relation-kind", "related", "--apply", "--confirm", relationToken(t, "relation_"+operation)}
				var out, err bytes.Buffer
				if code := run(args, &out, &err); code != 1 || out.Len() != 0 || len(requests) != 3 {
					t.Fatalf("code=%d requests=%v stdout=%q stderr=%q", code, requests, out.String(), err.String())
				}
			})
		}
	}
}
