package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	// MaxAttachmentDownloadBytes is the fixed client-side hard cap for one
	// attachment download (100 MiB). It is not configurable.
	MaxAttachmentDownloadBytes int64 = 100 << 20
	// AttachmentTransferTimeout is the fixed per-transfer timeout. It is
	// applied to a derived context inside the download method only and never
	// mutates the shared HTTP client behavior.
	AttachmentTransferTimeout = 10 * time.Minute
	// attachmentMetadataMaxBytes bounds the metadata preflight response.
	attachmentMetadataMaxBytes = 8 << 20
)

// AttachmentMeta is the minimal preflight view of one attachment. Size is nil
// unless the service sent a typed numeric size field.
type AttachmentMeta struct {
	ID   int64
	Size *int64
}

type attachmentListItem struct {
	ID   *int64 `json:"id"`
	Size *int64 `json:"size"`
}

// attachmentDownloadEnvelope decodes only the fields needed for the strict
// download preflight. The legacy raw list command is untouched; this view
// never exposes attachment content or full attachment data.
type attachmentDownloadEnvelope struct {
	Items      []attachmentListItem `json:"items"`
	Page       *int                 `json:"page"`
	PerPage    *int                 `json:"per_page"`
	Total      *int                 `json:"total"`
	TotalPages *int                 `json:"total_pages"`
}

// findDownloadAttachment strictly validates a single-page attachment list
// envelope and locates attachmentID. It requires parsed pagination metadata
// page=1, per_page=1000, total_pages compatible with one complete page,
// total==len(items), total<=1000, positive unique IDs, and non-negative
// typed sizes. Unknown fields are ignored; anything missing, malformed, or
// incomplete is rejected so a lookup can never rely on partial data. A
// missing target maps to ErrNotFound.
func findDownloadAttachment(envelope attachmentDownloadEnvelope, attachmentID int64) (AttachmentMeta, error) {
	if attachmentID < 1 {
		return AttachmentMeta{}, errors.New("attachment id must be positive")
	}
	if envelope.Page == nil || *envelope.Page != 1 {
		return AttachmentMeta{}, errors.New("attachment list envelope page is missing or not 1")
	}
	if envelope.PerPage == nil || *envelope.PerPage != 1000 {
		return AttachmentMeta{}, errors.New("attachment list envelope per_page is missing or not 1000")
	}
	if envelope.Total == nil || envelope.Items == nil {
		return AttachmentMeta{}, errors.New("attachment list envelope is incomplete")
	}
	total := *envelope.Total
	if total < 0 || total > 1000 || len(envelope.Items) > 1000 || len(envelope.Items) != total {
		return AttachmentMeta{}, errors.New("attachment list envelope is incomplete")
	}
	// A complete bounded result has no pages for no items and exactly one page
	// otherwise. Accepting either value for both cases would not prove that the
	// requested page contains the complete attachment set.
	wantPages := 0
	if total > 0 {
		wantPages = 1
	}
	if envelope.TotalPages == nil || *envelope.TotalPages != wantPages {
		return AttachmentMeta{}, errors.New("attachment list envelope total_pages is missing or incompatible with one page")
	}
	seen := make(map[int64]struct{}, len(envelope.Items))
	var match *AttachmentMeta
	for _, item := range envelope.Items {
		if item.ID == nil || *item.ID < 1 {
			return AttachmentMeta{}, errors.New("attachment list contains an item without a valid id")
		}
		if item.Size != nil && (*item.Size < 0 || *item.Size > MaxAttachmentDownloadBytes) {
			return AttachmentMeta{}, errors.New("attachment list contains an invalid size")
		}
		if _, duplicate := seen[*item.ID]; duplicate {
			return AttachmentMeta{}, errors.New("attachment list contains duplicate ids")
		}
		seen[*item.ID] = struct{}{}
		if *item.ID == attachmentID {
			meta := AttachmentMeta{ID: *item.ID, Size: item.Size}
			match = &meta
		}
	}
	// Duplicates anywhere in the envelope are rejected above even when they
	// appear after the matching item.
	if match != nil {
		return *match, nil
	}
	return AttachmentMeta{}, ErrNotFound
}

// GetTaskAttachmentDownloadMeta performs the strict metadata preflight for a
// single attachment download: exactly one authenticated GET of
// /api/v2/tasks/{task}/attachments?page=1&per_page=1000, accepting only HTTP
// 200. It returns the minimal typed view of the target attachment. Error
// response bodies are never read. This is deliberately stricter than the
// legacy raw attachments list command, which is unchanged.
func (c *Client) GetTaskAttachmentDownloadMeta(ctx context.Context, baseURL, token string, taskID, attachmentID int64) (AttachmentMeta, error) {
	if ctx == nil {
		return AttachmentMeta{}, errors.New("context must not be nil")
	}
	if taskID < 1 {
		return AttachmentMeta{}, errors.New("task id must be positive")
	}
	if attachmentID < 1 {
		return AttachmentMeta{}, errors.New("attachment id must be positive")
	}
	target, err := taskAttachmentsListEndpoint(baseURL, taskID, 1, 1000)
	if err != nil {
		return AttachmentMeta{}, errors.New("cannot create task attachments request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return AttachmentMeta{}, errors.New("cannot create request")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		// Do not expose an endpoint, query, or server name in download
		// diagnostics. Configuration can contain credentials in any of them.
		return AttachmentMeta{}, errors.New("task attachment metadata request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if err := responseError(response, "task attachment metadata request"); err != nil {
			return AttachmentMeta{}, err
		}
		return AttachmentMeta{}, &StatusError{Operation: "task attachment metadata request", StatusCode: response.StatusCode}
	}
	if response.ContentLength > attachmentMetadataMaxBytes {
		return AttachmentMeta{}, errors.New("attachment metadata response exceeds the permitted size")
	}
	// Read the complete successful body through max+1 bytes. This proves both
	// the size bound for chunked responses and that parsing cannot silently
	// accept a valid prefix of an oversized body.
	body, err := io.ReadAll(io.LimitReader(response.Body, attachmentMetadataMaxBytes+1))
	if err != nil || int64(len(body)) > attachmentMetadataMaxBytes {
		return AttachmentMeta{}, errors.New("attachment metadata response exceeds the permitted size")
	}
	var envelope attachmentDownloadEnvelope
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&envelope); err != nil {
		return AttachmentMeta{}, errors.New("cannot decode attachment list envelope")
	}
	// Decode one more value to require EOF (allowing only whitespace after the
	// envelope), rejecting both a second JSON value and arbitrary trailing data.
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return AttachmentMeta{}, errors.New("attachment list envelope has trailing content")
	}
	return findDownloadAttachment(envelope, attachmentID)
}

// AttachmentByteStream is one in-flight attachment byte transfer. Read reads
// response bytes; Close ends the transfer and releases the timeout context.
type AttachmentByteStream struct {
	// StatusCode is always http.StatusOK for a successfully opened stream.
	StatusCode int
	// ContentLength is the response content length, or -1 when unknown.
	ContentLength int64
	body          io.ReadCloser
	cancel        context.CancelFunc
}

// Read reads response bytes.
func (s *AttachmentByteStream) Read(p []byte) (int, error) { return s.body.Read(p) }

// Close closes the response body and cancels the transfer context.
func (s *AttachmentByteStream) Close() error {
	err := s.body.Close()
	s.cancel()
	return err
}

// GetTaskAttachmentBytes opens exactly one byte stream for a single
// attachment via GET /api/v2/tasks/{task}/attachments/{attachment} with the
// fixed 10-minute transfer timeout. Only HTTP 200 is accepted; error
// response bodies are never read. Redirects are refused by the client
// construction, so the bearer token is never forwarded to another URL.
func (c *Client) GetTaskAttachmentBytes(ctx context.Context, baseURL, token string, taskID, attachmentID int64) (*AttachmentByteStream, error) {
	return c.getTaskAttachmentBytes(ctx, AttachmentTransferTimeout, baseURL, token, taskID, attachmentID)
}

// getTaskAttachmentBytes is the testable core of GetTaskAttachmentBytes with
// an explicit transfer timeout. The timeout lives only in a derived context:
// a client-level Timeout (the shared default is 10 seconds) would cut a long
// transfer short, so the transfer uses a scoped copy of the HTTP client with
// Timeout disabled. Ordinary client behavior is never mutated.
func (c *Client) getTaskAttachmentBytes(ctx context.Context, timeout time.Duration, baseURL, token string, taskID, attachmentID int64) (*AttachmentByteStream, error) {
	if ctx == nil {
		return nil, errors.New("context must not be nil")
	}
	if taskID < 1 {
		return nil, errors.New("task id must be positive")
	}
	if attachmentID < 1 {
		return nil, errors.New("attachment id must be positive")
	}
	if timeout <= 0 {
		return nil, errors.New("transfer timeout must be positive")
	}
	transferCtx, cancel := context.WithTimeout(ctx, timeout)
	target := taskAttachmentEndpoint(baseURL, taskID, attachmentID)
	req, err := http.NewRequestWithContext(transferCtx, http.MethodGet, target, nil)
	if err != nil {
		cancel()
		return nil, errors.New("cannot create request")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	transferClient := *c.httpClient
	transferClient.Timeout = 0
	response, err := transferClient.Do(req)
	if err != nil {
		cancel()
		// Do not expose an endpoint, query, or server name in download
		// diagnostics. Configuration can contain credentials in any of them.
		return nil, errors.New("task attachment download request failed")
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		cancel()
		if err := responseError(response, "task attachment download request"); err != nil {
			return nil, err
		}
		return nil, &StatusError{Operation: "task attachment download request", StatusCode: response.StatusCode}
	}
	return &AttachmentByteStream{
		StatusCode:    response.StatusCode,
		ContentLength: response.ContentLength,
		body:          response.Body,
		cancel:        cancel,
	}, nil
}

func taskAttachmentEndpoint(baseURL string, taskID, attachmentID int64) string {
	return endpoint(baseURL, "tasks") + "/" + strconv.FormatInt(taskID, 10) + "/attachments/" + strconv.FormatInt(attachmentID, 10)
}
