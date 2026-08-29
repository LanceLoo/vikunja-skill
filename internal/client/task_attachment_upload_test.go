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
	"sync/atomic"
	"testing"
	"time"
)

const attachmentUploadToken = "upload-secret-token"

func validAttachmentUploadInput() TaskAttachmentUploadInput {
	return TaskAttachmentUploadInput{TaskID: 12, Filename: "report.txt", Size: 7, Reader: bytes.NewReader([]byte("payload"))}
}

func attachmentUploadSuccess(name string, size int64) string {
	return `{"errors":[],"success":[{"id":5,"task_id":12,"file":{"name":"` + name + `","size":` + strconv.FormatInt(size, 10) + `}}]}`
}

func TestUploadTaskAttachmentRequestAndStrictSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v2/tasks/12/attachments" || r.URL.RawQuery != "" {
			t.Errorf("request = %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		if r.ContentLength < 1 || r.Header.Get("Authorization") != "Bearer "+attachmentUploadToken {
			t.Errorf("content length = %d, authorization = %q", r.ContentLength, r.Header.Get("Authorization"))
		}
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		if len(r.MultipartForm.Value) != 0 || len(r.MultipartForm.File["files"]) != 1 || len(r.MultipartForm.File) != 1 {
			t.Fatalf("multipart fields = %#v, files = %#v", r.MultipartForm.Value, r.MultipartForm.File)
		}
		file := r.MultipartForm.File["files"][0]
		if file.Filename != "report.txt" {
			t.Errorf("filename = %q", file.Filename)
		}
		opened, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer opened.Close()
		payload, _ := io.ReadAll(opened)
		if string(payload) != "payload" {
			t.Errorf("payload = %q", payload)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, attachmentUploadSuccess("report.txt", 7))
	}))
	defer server.Close()

	got, err := New(server.Client()).UploadTaskAttachment(context.Background(), server.URL, attachmentUploadToken, validAttachmentUploadInput())
	if err != nil {
		t.Fatalf("UploadTaskAttachment: %v", err)
	}
	if got != (TaskAttachmentUploadResult{ID: 5, TaskID: 12, Filename: "report.txt", Size: 7}) {
		t.Errorf("result = %#v", got)
	}
}

func TestUploadTaskAttachmentAcceptsNullOrMissingErrors(t *testing.T) {
	for _, response := range []string{
		`{"errors":null,"success":[{"id":5,"task_id":12,"file":{"name":"report.txt","size":7}}]}`,
		`{"success":[{"id":5,"task_id":12,"file":{"name":"report.txt","size":7}}]}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, response)
		}))
		_, err := New(server.Client()).UploadTaskAttachment(context.Background(), server.URL, attachmentUploadToken, validAttachmentUploadInput())
		server.Close()
		if err != nil {
			t.Errorf("response %s: UploadTaskAttachment: %v", response, err)
		}
	}
}

func TestUploadTaskAttachmentPost201UnverifiedResponseClassification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"errors":[],"success":[]}`)
	}))
	defer server.Close()
	_, err := New(server.Client()).UploadTaskAttachment(context.Background(), server.URL, attachmentUploadToken, validAttachmentUploadInput())
	if !errors.Is(err, ErrTaskAttachmentUploadResponseUnverified) {
		t.Fatalf("err = %v, want ErrTaskAttachmentUploadResponseUnverified", err)
	}
}

func TestUploadTaskAttachmentValidationMakesNoRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	cases := []TaskAttachmentUploadInput{
		{TaskID: 0, Filename: "x", Size: 1, Reader: strings.NewReader("x")},
		{TaskID: 1, Filename: "", Size: 1, Reader: strings.NewReader("x")},
		{TaskID: 1, Filename: "../x", Size: 1, Reader: strings.NewReader("x")},
		{TaskID: 1, Filename: "x", Size: -1, Reader: strings.NewReader("x")},
		{TaskID: 1, Filename: "x", Size: MaxAttachmentUploadBytes + 1, Reader: strings.NewReader("x")},
		{TaskID: 1, Filename: "x", Size: 1},
	}
	for _, input := range cases {
		if _, err := New(server.Client()).UploadTaskAttachment(context.Background(), server.URL, attachmentUploadToken, input); err == nil {
			t.Error("invalid upload succeeded")
		}
	}
	if _, err := New(server.Client()).UploadTaskAttachment(nil, server.URL, attachmentUploadToken, validAttachmentUploadInput()); err == nil {
		t.Error("nil context succeeded")
	}
	if requests.Load() != 0 {
		t.Errorf("requests = %d, want 0", requests.Load())
	}
}

func TestUploadTaskAttachmentZeroByteSuccess(t *testing.T) {
	input := TaskAttachmentUploadInput{TaskID: 12, Filename: "empty.txt", Size: 0, Reader: strings.NewReader("")}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Fatal(err)
		}
		file := r.MultipartForm.File["files"][0]
		opened, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer opened.Close()
		contents, _ := io.ReadAll(opened)
		if file.Filename != input.Filename || len(contents) != 0 {
			t.Errorf("file = %q, contents = %q", file.Filename, contents)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, attachmentUploadSuccess(input.Filename, 0))
	}))
	defer server.Close()
	if _, err := New(server.Client()).UploadTaskAttachment(context.Background(), server.URL, attachmentUploadToken, input); err != nil {
		t.Fatalf("zero-byte upload: %v", err)
	}
}

func TestUploadTaskAttachmentInvalidRequestDoesNotStartProducer(t *testing.T) {
	readStarted := make(chan struct{}, 1)
	input := TaskAttachmentUploadInput{
		TaskID: 12, Filename: "report.txt", Size: 1,
		Reader: readerFunc(func([]byte) (int, error) { readStarted <- struct{}{}; return 0, io.EOF }),
	}
	done := make(chan error, 1)
	go func() {
		_, err := New(nil).UploadTaskAttachment(context.Background(), "://invalid-base-url", attachmentUploadToken, input)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("invalid request succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("invalid request did not return")
	}
	select {
	case <-readStarted:
		t.Error("multipart producer read source before request construction")
	default:
	}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func TestVerifyTaskAttachmentUploadReadback(t *testing.T) {
	uploaded := TaskAttachmentUploadResult{ID: 5, TaskID: 12, Filename: "report.txt", Size: 7}
	valid := `{"items":[{"id":5,"task_id":12,"file":{"name":"report.txt","size":7}}],"page":1,"per_page":1000,"total":1,"total_pages":1}`
	t.Run("happy path", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/api/v2/tasks/12/attachments" || r.URL.RawQuery != "page=1&per_page=1000" {
				t.Errorf("request = %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			}
			if r.Header.Get("Authorization") != "Bearer "+attachmentUploadToken {
				t.Error("missing auth")
			}
			_, _ = io.WriteString(w, valid)
		}))
		defer server.Close()
		if err := New(server.Client()).VerifyTaskAttachmentUploadReadback(context.Background(), server.URL, attachmentUploadToken, uploaded); err != nil {
			t.Fatal(err)
		}
	})
	cases := []string{
		`not json`, valid + ` {}`,
		`{"items":[],"page":1,"per_page":1000,"total":1,"total_pages":1}`,
		`{"items":[],"page":2,"per_page":1000,"total":0,"total_pages":0}`,
		`{"items":[{"id":5,"task_id":12,"file":{"name":"report.txt","size":7}}],"page":1,"per_page":1000,"total":1,"total_pages":0}`,
		`{"items":[{"id":5,"task_id":12,"file":{"name":"report.txt","size":7}},{"id":6,"task_id":0,"file":{"name":"x","size":0}}],"page":1,"per_page":1000,"total":2,"total_pages":1}`,
		`{"items":[{"id":5,"task_id":12,"file":{"name":"report.txt","size":7}},{"id":5,"task_id":12,"file":{"name":"report.txt","size":7}}],"page":1,"per_page":1000,"total":2,"total_pages":1}`,
		`{"items":[{"id":6,"task_id":12,"file":{"name":"report.txt","size":7}}],"page":1,"per_page":1000,"total":1,"total_pages":1}`,
	}
	for _, body := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }))
		if err := New(server.Client()).VerifyTaskAttachmentUploadReadback(context.Background(), server.URL, attachmentUploadToken, uploaded); err == nil {
			t.Errorf("invalid readback accepted: %s", body)
		}
		server.Close()
	}
}

func TestVerifyTaskAttachmentUploadReadbackBoundsStatusAndRedirect(t *testing.T) {
	uploaded := TaskAttachmentUploadResult{ID: 5, TaskID: 12, Filename: "report.txt", Size: 7}
	for _, declared := range []bool{true, false} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if declared {
				w.Header().Set("Content-Length", strconv.FormatInt(attachmentUploadResponseMaxBytes+1, 10))
			}
			_, _ = w.Write(bytes.Repeat([]byte("x"), int(attachmentUploadResponseMaxBytes+1)))
		}))
		if err := New(server.Client()).VerifyTaskAttachmentUploadReadback(context.Background(), server.URL, attachmentUploadToken, uploaded); err == nil {
			t.Error("oversized readback accepted")
		}
		server.Close()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusCreated) }))
	if err := New(server.Client()).VerifyTaskAttachmentUploadReadback(context.Background(), server.URL, attachmentUploadToken, uploaded); err == nil {
		t.Error("non-200 accepted")
	}
	server.Close()
	var reached atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("token forwarded")
		}
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	if err := New(redirect.Client()).VerifyTaskAttachmentUploadReadback(context.Background(), redirect.URL, attachmentUploadToken, uploaded); err == nil || reached.Load() != 0 {
		t.Error("redirect followed or accepted")
	}
}

func TestUploadTaskAttachmentRejectsInvalidSuccessEnvelopes(t *testing.T) {
	cases := []string{
		`{"errors":[{"message":"no"}],"success":[{"id":5,"task_id":12,"file":{"name":"report.txt","size":7}}]}`,
		`{"errors":[],"success":[]}`,
		`{"errors":[],"success":[{"id":5,"task_id":12,"file":{"name":"report.txt","size":7}},{"id":6,"task_id":12,"file":{"name":"report.txt","size":7}}]}`,
		`{"errors":[],"success":[{"id":0,"task_id":12,"file":{"name":"report.txt","size":7}}]}`,
		`{"errors":[],"success":[{"id":5,"task_id":99,"file":{"name":"report.txt","size":7}}]}`,
		`{"errors":[],"success":[{"id":5,"task_id":12,"file":{"name":"other.txt","size":7}}]}`,
		`{"errors":[],"success":[{"id":5,"task_id":12,"file":{"name":"report.txt","size":6}}]}`,
		`not json`,
		attachmentUploadSuccess("report.txt", 7) + ` {}`,
	}
	for _, body := range cases {
		t.Run("invalid envelope", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			if _, err := New(server.Client()).UploadTaskAttachment(context.Background(), server.URL, attachmentUploadToken, validAttachmentUploadInput()); err == nil {
				t.Error("invalid envelope succeeded")
			}
		})
	}
}

func TestUploadTaskAttachmentNon201NoRetryAndRedirectRefused(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusAccepted, http.StatusBadRequest, http.StatusInternalServerError} {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(status) }))
		_, err := New(server.Client()).UploadTaskAttachment(context.Background(), server.URL, attachmentUploadToken, validAttachmentUploadInput())
		server.Close()
		if err == nil || requests.Load() != 1 {
			t.Errorf("status %d: err = %v, requests = %d", status, err, requests.Load())
		}
		if errors.Is(err, ErrTaskAttachmentUploadResponseUnverified) {
			t.Errorf("status %d: non-201 incorrectly classified as an unverified 201 response", status)
		}
	}
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("authorization forwarded to redirect target")
		}
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer server.Close()
	if _, err := New(server.Client()).UploadTaskAttachment(context.Background(), server.URL, attachmentUploadToken, validAttachmentUploadInput()); err == nil {
		t.Error("redirect succeeded")
	}
	if redirected.Load() != 0 {
		t.Error("redirect target was requested")
	}
}

func TestUploadTaskAttachmentResponseBoundAndScopedTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(bytes.Repeat([]byte("x"), int(attachmentUploadResponseMaxBytes+1)))
	}))
	defer server.Close()
	shared := server.Client()
	shared.Timeout = 13 * time.Second
	api := New(shared)
	if _, err := api.UploadTaskAttachment(context.Background(), server.URL, attachmentUploadToken, validAttachmentUploadInput()); err == nil {
		t.Error("oversized response succeeded")
	}
	if shared.Timeout != 13*time.Second || api.httpClient.Timeout != 13*time.Second {
		t.Error("shared client timeout was changed")
	}

	blocking := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer blocking.Close()
	_, err := api.uploadTaskAttachment(context.Background(), 20*time.Millisecond, blocking.URL, attachmentUploadToken, validAttachmentUploadInput())
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("timeout error = %v, want sanitized request error", err)
	}
}
