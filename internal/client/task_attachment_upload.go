package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrTaskAttachmentUploadResponseUnverified means the server returned HTTP
// 201, but this client could not safely bound, read, decode, or verify its
// response body. It never identifies transport or non-201 failures.
var ErrTaskAttachmentUploadResponseUnverified = errors.New("task attachment upload response unverified")

const (
	// MaxAttachmentUploadBytes is the fixed client-side hard cap for a single
	// attachment upload (100 MiB). It is independent of server configuration.
	MaxAttachmentUploadBytes int64 = 100 << 20
	// AttachmentUploadTimeout is the fixed timeout for one upload. It is
	// applied to a derived request context and not to the shared HTTP client.
	AttachmentUploadTimeout = 10 * time.Minute

	attachmentUploadResponseMaxBytes int64 = 8 << 20
)

// TaskAttachmentUploadInput describes one caller-owned, already safely-opened
// attachment source. Size may be zero for an empty attachment. Reader is
// consumed exactly once when the request starts.
type TaskAttachmentUploadInput struct {
	TaskID   int64
	Filename string
	Size     int64
	Reader   io.Reader
}

// TaskAttachmentUploadResult is the verified identity and file metadata of
// the attachment returned by a successful upload.
type TaskAttachmentUploadResult struct {
	ID       int64
	TaskID   int64
	Filename string
	Size     int64
}

type attachmentUploadFile struct {
	Name *string `json:"name"`
	Size *int64  `json:"size"`
}

type attachmentUploadResponseItem struct {
	ID     *int64                `json:"id"`
	TaskID *int64                `json:"task_id"`
	File   *attachmentUploadFile `json:"file"`
}

type attachmentUploadEnvelope struct {
	Errors  *[]json.RawMessage              `json:"errors"`
	Success *[]attachmentUploadResponseItem `json:"success"`
}

type attachmentUploadReadbackItem struct {
	ID     *int64                `json:"id"`
	TaskID *int64                `json:"task_id"`
	File   *attachmentUploadFile `json:"file"`
}

type attachmentUploadReadbackEnvelope struct {
	Items      *[]attachmentUploadReadbackItem `json:"items"`
	Page       *int                            `json:"page"`
	PerPage    *int                            `json:"per_page"`
	Total      *int                            `json:"total"`
	TotalPages *int                            `json:"total_pages"`
}

// UploadTaskAttachment uploads exactly one attachment using the v2 task
// attachment endpoint. It accepts only HTTP 201 and returns only a response
// whose single attachment exactly confirms the requested task, file name, and
// file size.
func (c *Client) UploadTaskAttachment(ctx context.Context, baseURL, token string, input TaskAttachmentUploadInput) (TaskAttachmentUploadResult, error) {
	return c.uploadTaskAttachment(ctx, AttachmentUploadTimeout, baseURL, token, input)
}

// uploadTaskAttachment is the testable core with an explicit timeout. The
// scoped client retains New's redirect refusal while disabling its overall
// Timeout in favor of this operation's derived context.
func (c *Client) uploadTaskAttachment(ctx context.Context, timeout time.Duration, baseURL, token string, input TaskAttachmentUploadInput) (TaskAttachmentUploadResult, error) {
	if err := validateTaskAttachmentUpload(ctx, timeout, input); err != nil {
		return TaskAttachmentUploadResult{}, err
	}

	transferCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stream, contentType, contentLength, err := attachmentUploadBody(input)
	if err != nil {
		return TaskAttachmentUploadResult{}, errors.New("cannot create attachment upload body")
	}
	req, err := http.NewRequestWithContext(transferCtx, http.MethodPost, taskAttachmentsUploadEndpoint(baseURL, input.TaskID), stream.reader)
	if err != nil {
		_ = stream.reader.Close()
		return TaskAttachmentUploadResult{}, errors.New("cannot create request")
	}
	req.Header.Set("Content-Type", contentType)
	req.ContentLength = contentLength
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	transferClient := *c.httpClient
	transferClient.Timeout = 0
	stream.start()
	response, err := transferClient.Do(req)
	_ = stream.reader.Close()
	if err != nil {
		return TaskAttachmentUploadResult{}, errors.New("task attachment upload request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		if err := responseError(response, "task attachment upload request"); err != nil {
			return TaskAttachmentUploadResult{}, err
		}
		return TaskAttachmentUploadResult{}, &StatusError{Operation: "task attachment upload request", StatusCode: response.StatusCode}
	}
	if response.ContentLength > attachmentUploadResponseMaxBytes {
		return TaskAttachmentUploadResult{}, attachmentUploadResponseUnverified("attachment upload response exceeds the permitted size")
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(response.Body, attachmentUploadResponseMaxBytes+1))
	if err != nil || int64(len(bodyBytes)) > attachmentUploadResponseMaxBytes {
		return TaskAttachmentUploadResult{}, attachmentUploadResponseUnverified("attachment upload response exceeds the permitted size")
	}
	result, err := verifyAttachmentUploadResponse(bodyBytes, input)
	if err != nil {
		return TaskAttachmentUploadResult{}, attachmentUploadResponseUnverified(err.Error())
	}
	return result, nil
}

func attachmentUploadResponseUnverified(message string) error {
	return fmt.Errorf("%w: %s", ErrTaskAttachmentUploadResponseUnverified, message)
}

func validateTaskAttachmentUpload(ctx context.Context, timeout time.Duration, input TaskAttachmentUploadInput) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	if timeout <= 0 {
		return errors.New("attachment upload timeout must be positive")
	}
	if input.TaskID < 1 {
		return errors.New("task id must be positive")
	}
	if !safeAttachmentFilename(input.Filename) {
		return errors.New("attachment filename must be a safe nonempty base name")
	}
	if input.Size < 0 || input.Size > MaxAttachmentUploadBytes {
		return errors.New("attachment size is outside the permitted range")
	}
	if input.Reader == nil {
		return errors.New("attachment reader must not be nil")
	}
	return nil
}

func safeAttachmentFilename(name string) bool {
	if strings.TrimSpace(name) == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, "/\\\x00\r\n") {
		return false
	}
	return true
}

// attachmentUploadStream delays producer startup until its request is fully
// constructed. Closing reader unblocks a producer when transport setup fails.
type attachmentUploadStream struct {
	reader *io.PipeReader
	writer *io.PipeWriter
	prefix []byte
	suffix []byte
	input  TaskAttachmentUploadInput
}

func (s *attachmentUploadStream) start() {
	go func() {
		_, err := s.writer.Write(s.prefix)
		if err == nil {
			_, err = io.CopyN(s.writer, s.input.Reader, s.input.Size)
		}
		if err == nil {
			_, err = s.writer.Write(s.suffix)
		}
		_ = s.writer.CloseWithError(err)
	}()
}

// attachmentUploadBody constructs multipart framing separately from file
// bytes. It does not start a producer; start is called only after request
// construction succeeds.
func attachmentUploadBody(input TaskAttachmentUploadInput) (*attachmentUploadStream, string, int64, error) {
	var framing bytes.Buffer
	writer := multipart.NewWriter(&framing)
	if _, err := writer.CreateFormFile("files", input.Filename); err != nil {
		return nil, "", 0, err
	}
	prefixLength := framing.Len()
	if err := writer.Close(); err != nil {
		return nil, "", 0, err
	}
	framingBytes := framing.Bytes()
	prefix := append([]byte(nil), framingBytes[:prefixLength]...)
	suffix := append([]byte(nil), framingBytes[prefixLength:]...)

	pipeReader, pipeWriter := io.Pipe()
	return &attachmentUploadStream{reader: pipeReader, writer: pipeWriter, prefix: prefix, suffix: suffix, input: input}, writer.FormDataContentType(), int64(len(prefix)+len(suffix)) + input.Size, nil
}

func verifyAttachmentUploadResponse(body []byte, input TaskAttachmentUploadInput) (TaskAttachmentUploadResult, error) {
	var envelope attachmentUploadEnvelope
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&envelope); err != nil {
		return TaskAttachmentUploadResult{}, errors.New("cannot decode attachment upload response")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return TaskAttachmentUploadResult{}, errors.New("attachment upload response has trailing content")
	}
	// Vikunja can omit errors or encode an empty error collection as null. Both
	// represent no errors; a present nonempty errors array remains a rejection.
	if (envelope.Errors != nil && len(*envelope.Errors) != 0) || envelope.Success == nil || len(*envelope.Success) != 1 {
		return TaskAttachmentUploadResult{}, errors.New("attachment upload response is not a single successful attachment")
	}
	item := (*envelope.Success)[0]
	if item.ID == nil || *item.ID < 1 || item.TaskID == nil || *item.TaskID != input.TaskID || item.File == nil ||
		item.File.Name == nil || *item.File.Name != input.Filename || item.File.Size == nil || *item.File.Size != input.Size {
		return TaskAttachmentUploadResult{}, errors.New("attachment upload response does not match the uploaded attachment")
	}
	return TaskAttachmentUploadResult{ID: *item.ID, TaskID: *item.TaskID, Filename: *item.File.Name, Size: *item.File.Size}, nil
}

func taskAttachmentsUploadEndpoint(baseURL string, taskID int64) string {
	return endpoint(baseURL, "tasks") + "/" + strconv.FormatInt(taskID, 10) + "/attachments"
}

// VerifyTaskAttachmentUploadReadback verifies one uploaded attachment with a
// single bounded authenticated readback of the complete first attachments page.
func (c *Client) VerifyTaskAttachmentUploadReadback(ctx context.Context, baseURL, token string, uploaded TaskAttachmentUploadResult) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	if uploaded.ID < 1 || uploaded.TaskID < 1 || !safeAttachmentFilename(uploaded.Filename) || uploaded.Size < 0 || uploaded.Size > MaxAttachmentUploadBytes {
		return errors.New("uploaded attachment is invalid")
	}
	target, err := taskAttachmentsListEndpoint(baseURL, uploaded.TaskID, 1, 1000)
	if err != nil {
		return errors.New("cannot create task attachment readback request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return errors.New("cannot create request")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return errors.New("task attachment readback request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if err := responseError(response, "task attachment readback request"); err != nil {
			return err
		}
		return &StatusError{Operation: "task attachment readback request", StatusCode: response.StatusCode}
	}
	if response.ContentLength > attachmentUploadResponseMaxBytes {
		return errors.New("attachment readback response exceeds the permitted size")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, attachmentUploadResponseMaxBytes+1))
	if err != nil || int64(len(body)) > attachmentUploadResponseMaxBytes {
		return errors.New("attachment readback response exceeds the permitted size")
	}
	return verifyAttachmentUploadReadback(body, uploaded)
}

func verifyAttachmentUploadReadback(body []byte, uploaded TaskAttachmentUploadResult) error {
	var envelope attachmentUploadReadbackEnvelope
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&envelope); err != nil {
		return errors.New("cannot decode attachment readback response")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("attachment readback response has trailing content")
	}
	if envelope.Items == nil || envelope.Page == nil || envelope.PerPage == nil || envelope.Total == nil || envelope.TotalPages == nil ||
		*envelope.Page != 1 || *envelope.PerPage != 1000 || *envelope.Total < 0 || *envelope.Total > 1000 || *envelope.Total != len(*envelope.Items) {
		return errors.New("attachment readback response is incomplete")
	}
	wantPages := 0
	if *envelope.Total > 0 {
		wantPages = 1
	}
	if *envelope.TotalPages != wantPages {
		return errors.New("attachment readback response is incomplete")
	}
	seen := make(map[int64]struct{}, len(*envelope.Items))
	matches := 0
	for _, item := range *envelope.Items {
		if item.ID == nil || *item.ID < 1 || item.TaskID == nil || *item.TaskID != uploaded.TaskID || item.File == nil || item.File.Name == nil || strings.TrimSpace(*item.File.Name) == "" || item.File.Size == nil || *item.File.Size < 0 {
			return errors.New("attachment readback response contains an invalid item")
		}
		if _, duplicate := seen[*item.ID]; duplicate {
			return errors.New("attachment readback response contains duplicate ids")
		}
		seen[*item.ID] = struct{}{}
		if *item.ID == uploaded.ID && *item.TaskID == uploaded.TaskID && *item.File.Name == uploaded.Filename && *item.File.Size == uploaded.Size {
			matches++
		}
	}
	if matches != 1 {
		return errors.New("uploaded attachment is missing from readback")
	}
	return nil
}
