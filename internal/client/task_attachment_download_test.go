package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

const attachmentDownloadToken = "test-secret-token"

func validEnvelope() attachmentDownloadEnvelope {
	return attachmentDownloadEnvelope{
		Items:      []attachmentListItem{},
		Page:       intPtr(1),
		PerPage:    intPtr(1000),
		Total:      intPtr(0),
		TotalPages: intPtr(0),
	}
}

func TestFindDownloadAttachment(t *testing.T) {
	t.Run("valid envelope with typed size", func(t *testing.T) {
		envelope := validEnvelope()
		idFive, idNine, size := int64(5), int64(9), int64(3)
		envelope.Items = []attachmentListItem{{ID: &idFive, Size: &size}, {ID: &idNine}}
		envelope.Total = intPtr(2)
		envelope.TotalPages = intPtr(1)
		meta, err := findDownloadAttachment(envelope, 5)
		if err != nil {
			t.Fatalf("findDownloadAttachment: %v", err)
		}
		if meta.ID != 5 || meta.Size == nil || *meta.Size != 3 {
			t.Errorf("meta = %#v", meta)
		}
	})
	t.Run("valid envelope without size is nil not error", func(t *testing.T) {
		envelope := validEnvelope()
		idNine := int64(9)
		envelope.Items = []attachmentListItem{{ID: &idNine}}
		envelope.Total = intPtr(1)
		envelope.TotalPages = intPtr(1)
		meta, err := findDownloadAttachment(envelope, 9)
		if err != nil || meta.Size != nil {
			t.Fatalf("meta = %#v, err = %v", meta, err)
		}
	})
	t.Run("absent target is not found", func(t *testing.T) {
		envelope := validEnvelope()
		idNine := int64(9)
		envelope.Items = []attachmentListItem{{ID: &idNine}}
		envelope.Total = intPtr(1)
		envelope.TotalPages = intPtr(1)
		_, err := findDownloadAttachment(envelope, 5)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
	t.Run("invalid values rejected", func(t *testing.T) {
		idFive, size, negativeSize := int64(5), int64(3), int64(-1)
		cases := map[string]func() attachmentDownloadEnvelope{
			"missing page": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				e.Page = nil
				return e
			},
			"page mismatch": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				e.Page = intPtr(2)
				return e
			},
			"negative page": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				e.Page = intPtr(-1)
				return e
			},
			"missing per_page": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				e.PerPage = nil
				return e
			},
			"per_page mismatch": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				e.PerPage = intPtr(50)
				return e
			},
			"negative per_page": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				e.PerPage = intPtr(-1000)
				return e
			},
			"missing total_pages": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				e.TotalPages = nil
				return e
			},
			"total_pages beyond one page": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				e.TotalPages = intPtr(2)
				return e
			},
			"negative total_pages": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				e.TotalPages = intPtr(-1)
				return e
			},
			"empty list has one page": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				e.TotalPages = intPtr(1)
				return e
			},
			"non-empty list has zero pages": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				e.Items = []attachmentListItem{{ID: &idFive}}
				e.Total = intPtr(1)
				return e
			},
			"missing total": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				e.Total = nil
				return e
			},
			"negative total": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				e.Total = intPtr(-1)
				return e
			},
			"total over 1000": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				e.Total = intPtr(1001)
				return e
			},
			"total mismatch": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				e.Items = []attachmentListItem{{ID: &idFive}}
				e.Total = intPtr(2)
				e.TotalPages = intPtr(1)
				return e
			},
			"duplicate ids": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				e.Items = []attachmentListItem{{ID: &idFive}, {ID: &idFive}}
				e.Total = intPtr(2)
				e.TotalPages = intPtr(1)
				return e
			},
			"duplicate id after match": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				other := int64(6)
				e.Items = []attachmentListItem{{ID: &idFive}, {ID: &other}, {ID: &idFive}}
				e.Total = intPtr(3)
				e.TotalPages = intPtr(1)
				return e
			},
			"missing id": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				e.Items = []attachmentListItem{{Size: &size}}
				e.Total = intPtr(1)
				e.TotalPages = intPtr(1)
				return e
			},
			"zero id": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				zero := int64(0)
				e.Items = []attachmentListItem{{ID: &zero}}
				e.Total = intPtr(1)
				e.TotalPages = intPtr(1)
				return e
			},
			"negative size": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				e.Items = []attachmentListItem{{ID: &idFive, Size: &negativeSize}}
				e.Total = intPtr(1)
				e.TotalPages = intPtr(1)
				return e
			},
			"size over cap": func() attachmentDownloadEnvelope {
				e := validEnvelope()
				over := MaxAttachmentDownloadBytes + 1
				e.Items = []attachmentListItem{{ID: &idFive, Size: &over}}
				e.Total = intPtr(1)
				e.TotalPages = intPtr(1)
				return e
			},
		}
		for name, build := range cases {
			if _, err := findDownloadAttachment(build(), 5); err == nil || errors.Is(err, ErrNotFound) {
				t.Errorf("%s: err = %v, want a non-NotFound rejection", name, err)
			}
		}
	})
	t.Run("invalid attachment id", func(t *testing.T) {
		if _, err := findDownloadAttachment(validEnvelope(), 0); err == nil {
			t.Error("err = nil, want rejection")
		}
	})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

const attachmentMetadataBody = `{"$schema":"https://x","items":[{"id":5,"size":4,"name":"` + attachmentDownloadToken + `-server-name.bin"}],"page":1,"per_page":1000,"total":1,"total_pages":1}`

func newAttachmentMetaServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/tasks/12/attachments" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.RawQuery; got != "page=1&per_page=1000" {
			t.Errorf("query = %q, want page=1&per_page=1000", got)
		}
		if r.Header.Get("Authorization") != "Bearer "+attachmentDownloadToken {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestGetTaskAttachmentDownloadMetaSuccess(t *testing.T) {
	server := newAttachmentMetaServer(t, http.StatusOK, attachmentMetadataBody)
	defer server.Close()
	meta, err := New(nil).GetTaskAttachmentDownloadMeta(context.Background(), server.URL, attachmentDownloadToken, 12, 5)
	if err != nil {
		t.Fatalf("GetTaskAttachmentDownloadMeta: %v", err)
	}
	if meta.ID != 5 || meta.Size == nil || *meta.Size != 4 {
		t.Errorf("meta = %#v", meta)
	}
}

func TestGetTaskAttachmentDownloadMetaBodyBoundAndCompleteness(t *testing.T) {
	valid := `{"items":[{"id":5,"size":4}],"page":1,"per_page":1000,"total":1,"total_pages":1}`
	cases := []struct {
		name          string
		body          []byte
		contentLength int64 // -1 uses chunked encoding
		wantOK        bool
	}{
		{"known over limit", []byte(valid), attachmentMetadataMaxBytes + 1, false},
		{"second JSON value", []byte(valid + ` {"unexpected":true}`), int64(len(valid + ` {"unexpected":true}`)), false},
		{"trailing garbage", []byte(valid + ` leaked-response-bytes`), int64(len(valid + ` leaked-response-bytes`)), false},
		{"exact limit valid JSON", append([]byte(valid), bytes.Repeat([]byte(" "), int(attachmentMetadataMaxBytes)-len(valid))...), attachmentMetadataMaxBytes, true},
		{"chunked over limit", bytes.Repeat([]byte("x"), int(attachmentMetadataMaxBytes)+1), -1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.contentLength >= 0 {
					w.Header().Set("Content-Length", strconv.FormatInt(tc.contentLength, 10))
				}
				_, _ = w.Write(tc.body)
			}))
			defer server.Close()
			meta, err := New(nil).GetTaskAttachmentDownloadMeta(context.Background(), server.URL, attachmentDownloadToken, 12, 5)
			if tc.wantOK {
				if err != nil || meta.ID != 5 {
					t.Fatalf("meta = %#v, err = %v", meta, err)
				}
				return
			}
			if err == nil {
				t.Fatal("err = nil, want rejection")
			}
			for _, secret := range []string{attachmentDownloadToken, server.URL, "leaked-response-bytes"} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("error leaked %q: %v", secret, err)
				}
			}
		})
	}
}

func TestGetTaskAttachmentDownloadMetaOnly200Accepted(t *testing.T) {
	// 201/202/204 and any other non-200 are rejected even with a valid body.
	for _, status := range []int{http.StatusCreated, http.StatusAccepted, http.StatusNoContent, http.StatusInternalServerError} {
		server := newAttachmentMetaServer(t, status, attachmentMetadataBody)
		_, err := New(nil).GetTaskAttachmentDownloadMeta(context.Background(), server.URL, attachmentDownloadToken, 12, 5)
		var statusErr *StatusError
		if !errors.As(err, &statusErr) || statusErr.StatusCode != status {
			t.Errorf("status %d: err = %v, want StatusError %d", status, err, status)
		}
		server.Close()
	}
	for _, tc := range []struct {
		status int
		want   error
	}{{http.StatusUnauthorized, ErrUnauthorized}, {http.StatusForbidden, ErrForbidden}, {http.StatusNotFound, ErrNotFound}} {
		server := newAttachmentMetaServer(t, tc.status, `{"message":"server detail with `+attachmentDownloadToken+`"}`)
		_, err := New(nil).GetTaskAttachmentDownloadMeta(context.Background(), server.URL, attachmentDownloadToken, 12, 5)
		if !errors.Is(err, tc.want) {
			t.Errorf("status %d: err = %v, want %v", tc.status, err, tc.want)
		}
		if strings.Contains(errString(err), attachmentDownloadToken) || strings.Contains(errString(err), "server detail") {
			t.Errorf("status %d: error leaked response body: %v", tc.status, err)
		}
		server.Close()
	}
}

func TestGetTaskAttachmentDownloadMetaRejectsBadEnvelopes(t *testing.T) {
	cases := map[string]string{
		"not json":            `{`,
		"malformed":           `[1,2]`,
		"missing page":        `{"items":[],"per_page":1000,"total":0,"total_pages":0}`,
		"page mismatch":       `{"items":[],"page":2,"per_page":1000,"total":0,"total_pages":0}`,
		"missing per_page":    `{"items":[],"page":1,"total":0,"total_pages":0}`,
		"per_page mismatch":   `{"items":[],"page":1,"per_page":50,"total":0,"total_pages":0}`,
		"missing total":       `{"items":[],"page":1,"per_page":1000,"total_pages":0}`,
		"missing total_pages": `{"items":[],"page":1,"per_page":1000,"total":0}`,
		"total_pages 2":       `{"items":[],"page":1,"per_page":1000,"total":0,"total_pages":2}`,
		"empty list one page": `{"items":[],"page":1,"per_page":1000,"total":0,"total_pages":1}`,
		"nonempty zero pages": `{"items":[{"id":5}],"page":1,"per_page":1000,"total":1,"total_pages":0}`,
		"negative total":      `{"items":[],"page":1,"per_page":1000,"total":-1,"total_pages":0}`,
		"total mismatch":      `{"items":[{"id":5}],"page":1,"per_page":1000,"total":2,"total_pages":1}`,
		"total over 1000":     `{"items":[],"page":1,"per_page":1000,"total":1001,"total_pages":2}`,
		"duplicate ids":       `{"items":[{"id":5},{"id":5}],"page":1,"per_page":1000,"total":2,"total_pages":1}`,
		"untyped id":          `{"items":[{"id":"5"}],"page":1,"per_page":1000,"total":1,"total_pages":1}`,
		"untyped size":        `{"items":[{"id":5,"size":"4"}],"page":1,"per_page":1000,"total":1,"total_pages":1}`,
		"negative size":       `{"items":[{"id":5,"size":-1}],"page":1,"per_page":1000,"total":1,"total_pages":1}`,
		"size over cap":       `{"items":[{"id":5,"size":104857601}],"page":1,"per_page":1000,"total":1,"total_pages":1}`,
		"missing items":       `{"page":1,"per_page":1000,"total":0,"total_pages":0}`,
	}
	for name, body := range cases {
		server := newAttachmentMetaServer(t, http.StatusOK, body)
		_, err := New(nil).GetTaskAttachmentDownloadMeta(context.Background(), server.URL, attachmentDownloadToken, 12, 5)
		if err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, want a non-NotFound rejection", name, err)
		}
		server.Close()
	}
}

func TestGetTaskAttachmentDownloadMetaAbsentAttachment(t *testing.T) {
	server := newAttachmentMetaServer(t, http.StatusOK, `{"items":[{"id":9,"size":1}],"page":1,"per_page":1000,"total":1,"total_pages":1}`)
	defer server.Close()
	_, err := New(nil).GetTaskAttachmentDownloadMeta(context.Background(), server.URL, attachmentDownloadToken, 12, 5)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetTaskAttachmentDownloadMetaValidation(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	api := New(nil)
	if _, err := api.GetTaskAttachmentDownloadMeta(nil, server.URL, attachmentDownloadToken, 12, 5); err == nil {
		t.Error("nil context: err = nil")
	}
	for _, ids := range [][2]int64{{0, 5}, {12, 0}, {-1, 5}, {12, -5}} {
		if _, err := api.GetTaskAttachmentDownloadMeta(context.Background(), server.URL, attachmentDownloadToken, ids[0], ids[1]); err == nil {
			t.Errorf("ids %v: err = nil, want rejection", ids)
		}
	}
	if requests != 0 {
		t.Errorf("requests = %d, want 0", requests)
	}
}

func newAttachmentByteServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/tasks/12/attachments/5" && r.Header.Get("Authorization") != "Bearer "+attachmentDownloadToken {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		handler(w, r)
	}))
}

func TestGetTaskAttachmentBytesSuccess(t *testing.T) {
	server := newAttachmentByteServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/tasks/12/attachments/5" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Length", "4")
		_, _ = w.Write([]byte("data"))
	})
	defer server.Close()

	stream, err := New(nil).GetTaskAttachmentBytes(context.Background(), server.URL, attachmentDownloadToken, 12, 5)
	if err != nil {
		t.Fatalf("GetTaskAttachmentBytes: %v", err)
	}
	defer stream.Close()
	if stream.StatusCode != http.StatusOK || stream.ContentLength != 4 {
		t.Errorf("stream = %#v", stream)
	}
	body, err := io.ReadAll(stream)
	if err != nil || string(body) != "data" {
		t.Errorf("body = %q, err = %v", body, err)
	}
}

func TestGetTaskAttachmentBytesStatusMatrix(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{http.StatusUnauthorized, ErrUnauthorized},
		{http.StatusForbidden, ErrForbidden},
		{http.StatusNotFound, ErrNotFound},
	}
	for _, tc := range cases {
		server := newAttachmentByteServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"message":"server detail with ` + attachmentDownloadToken + `"}`))
		})
		_, err := New(nil).GetTaskAttachmentBytes(context.Background(), server.URL, attachmentDownloadToken, 12, 5)
		if !errors.Is(err, tc.want) {
			t.Errorf("status %d: err = %v, want %v", tc.status, err, tc.want)
		}
		if strings.Contains(errString(err), attachmentDownloadToken) || strings.Contains(errString(err), "server detail") {
			t.Errorf("status %d: error leaked response body: %v", tc.status, err)
		}
		server.Close()
	}
	// Any non-200 status, including other 2xx, is a StatusError.
	for _, status := range []int{http.StatusNoContent, http.StatusInternalServerError} {
		server := newAttachmentByteServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		})
		_, err := New(nil).GetTaskAttachmentBytes(context.Background(), server.URL, attachmentDownloadToken, 12, 5)
		var statusErr *StatusError
		if !errors.As(err, &statusErr) || statusErr.StatusCode != status {
			t.Errorf("status %d: err = %v, want StatusError %d", status, err, status)
		}
		server.Close()
	}
}

func TestGetTaskAttachmentBytesRedirectRefusedWithoutTokenForwarding(t *testing.T) {
	targetRequests := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests++
		if r.Header.Get("Authorization") != "" {
			t.Errorf("redirect target received Authorization header")
		}
	}))
	defer target.Close()
	server := newAttachmentByteServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/stolen", http.StatusFound)
	})
	defer server.Close()

	_, err := New(nil).GetTaskAttachmentBytes(context.Background(), server.URL, attachmentDownloadToken, 12, 5)
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusFound {
		t.Fatalf("err = %v, want StatusError 302", err)
	}
	if targetRequests != 0 {
		t.Errorf("redirect target received %d requests, want 0", targetRequests)
	}
}

func TestGetTaskAttachmentBytesValidationAndSharedClientUntouched(t *testing.T) {
	api := New(nil)
	if api.httpClient.Timeout != 10*time.Second {
		t.Fatalf("default timeout = %v, want 10s", api.httpClient.Timeout)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++ }))
	defer server.Close()

	for _, ids := range [][2]int64{{0, 5}, {12, 0}, {-1, 5}} {
		if _, err := api.GetTaskAttachmentBytes(context.Background(), server.URL, attachmentDownloadToken, ids[0], ids[1]); err == nil {
			t.Errorf("ids %v: err = nil, want rejection", ids)
		}
	}
	if _, err := api.GetTaskAttachmentBytes(nil, server.URL, attachmentDownloadToken, 12, 5); err == nil {
		t.Error("nil context: err = nil, want rejection")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := api.GetTaskAttachmentBytes(cancelled, server.URL, attachmentDownloadToken, 12, 5); err == nil {
		t.Error("cancelled context: err = nil, want failure")
	}
	if requests > 1 {
		t.Errorf("requests = %d, want at most the cancelled attempt", requests)
	}
	// The per-transfer timeout is context-derived only; the shared default
	// client behavior must be unchanged.
	if api.httpClient.Timeout != 10*time.Second {
		t.Errorf("default timeout after calls = %v, want 10s", api.httpClient.Timeout)
	}
}

// TestAttachmentTransferTimeoutIsDistinct proves the transfer-specific
// timeout is effective and independent of the shared 10-second client
// timeout: a transfer that stalls longer than the short test timeout fails
// fast, far below what the default client timeout would allow. This does not
// measure or claim any 10-minute wall-clock behavior.
func TestAttachmentTransferTimeoutIsDistinct(t *testing.T) {
	server := newAttachmentByteServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte("data"))
	})
	defer server.Close()

	api := New(nil)
	start := time.Now()
	stream, err := api.getTaskAttachmentBytes(context.Background(), 50*time.Millisecond, server.URL, attachmentDownloadToken, 12, 5)
	if err == nil {
		_, readErr := io.ReadAll(stream)
		stream.Close()
		if readErr == nil {
			t.Fatal("stalled transfer succeeded; transfer timeout is not effective")
		}
	}
	elapsed := time.Since(start)
	if elapsed > 1500*time.Millisecond {
		t.Errorf("stalled transfer took %v; the transfer timeout was not applied (shared client timeout would allow up to 10s)", elapsed)
	}
	if api.httpClient.Timeout != 10*time.Second {
		t.Errorf("shared client timeout mutated to %v, want 10s", api.httpClient.Timeout)
	}
	// The exported method wires the fixed constant.
	if AttachmentTransferTimeout != 10*time.Minute {
		t.Errorf("AttachmentTransferTimeout = %v, want 10m", AttachmentTransferTimeout)
	}
}
