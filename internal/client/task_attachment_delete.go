package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const (
	attachmentDeletePageSize               = 1000
	attachmentDeleteMaxPages               = 100
	attachmentDeleteResponseMaxBytes int64 = 8 << 20
)

// TaskAttachmentDeleteMeta is the validated metadata of one attachment found
// during a complete deletion preflight scan.
type TaskAttachmentDeleteMeta struct {
	ID     int64
	TaskID int64
	Name   string
	Size   int64
}

// TaskAttachmentDeleteReadback reports the observed state after deletion.
// Present is false only after a complete valid scan establishes absence.
type TaskAttachmentDeleteReadback struct {
	Present bool
	Meta    TaskAttachmentDeleteMeta
}

type attachmentDeleteFile struct {
	Name *string `json:"name"`
	Size *int64  `json:"size"`
}

type attachmentDeleteItem struct {
	ID     *int64                `json:"id"`
	TaskID *int64                `json:"task_id"`
	File   *attachmentDeleteFile `json:"file"`
}

type attachmentDeleteEnvelope struct {
	Items      *[]attachmentDeleteItem `json:"items"`
	Page       *int                    `json:"page"`
	PerPage    *int                    `json:"per_page"`
	Total      *int                    `json:"total"`
	TotalPages *int                    `json:"total_pages"`
}

// DeleteTaskAttachment deletes exactly one attachment. Only HTTP 204 confirms
// endpoint success; notably, HTTP 404 is returned as ErrNotFound rather than
// being interpreted as a successful deletion claim.
func (c *Client) DeleteTaskAttachment(ctx context.Context, baseURL, token string, taskID, attachmentID int64) error {
	if err := validateTaskAttachmentDelete(ctx, taskID, attachmentID); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, taskAttachmentEndpoint(baseURL, taskID, attachmentID), nil)
	if err != nil {
		return errors.New("cannot create request")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return errors.New("task attachment delete request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		if err := responseError(response, "task attachment delete request"); err != nil {
			return err
		}
		return &StatusError{Operation: "task attachment delete request", StatusCode: response.StatusCode}
	}
	return nil
}

// GetTaskAttachmentDeleteMeta performs a complete, bounded metadata scan to
// locate attachmentID. It returns ErrNotFound only after a valid complete scan
// proves that the target is absent. It never downloads attachment bytes.
func (c *Client) GetTaskAttachmentDeleteMeta(ctx context.Context, baseURL, token string, taskID, attachmentID int64) (TaskAttachmentDeleteMeta, error) {
	if err := validateTaskAttachmentDelete(ctx, taskID, attachmentID); err != nil {
		return TaskAttachmentDeleteMeta{}, err
	}
	result, err := c.scanTaskAttachmentsForDelete(ctx, baseURL, token, taskID, attachmentID)
	if err != nil {
		return TaskAttachmentDeleteMeta{}, err
	}
	if !result.Present {
		return TaskAttachmentDeleteMeta{}, ErrNotFound
	}
	return result.Meta, nil
}

// VerifyTaskAttachmentDeletedReadback observes the target after a deletion.
// An absent target is represented by Present false; an API HTTP 404 remains
// ErrNotFound and is not converted into a verified absence result.
func (c *Client) VerifyTaskAttachmentDeletedReadback(ctx context.Context, baseURL, token string, taskID, attachmentID int64) (TaskAttachmentDeleteReadback, error) {
	if err := validateTaskAttachmentDelete(ctx, taskID, attachmentID); err != nil {
		return TaskAttachmentDeleteReadback{}, err
	}
	return c.scanTaskAttachmentsForDelete(ctx, baseURL, token, taskID, attachmentID)
}

func validateTaskAttachmentDelete(ctx context.Context, taskID, attachmentID int64) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	if taskID < 1 {
		return errors.New("task id must be positive")
	}
	if attachmentID < 1 {
		return errors.New("attachment id must be positive")
	}
	return nil
}

func (c *Client) scanTaskAttachmentsForDelete(ctx context.Context, baseURL, token string, taskID, targetID int64) (TaskAttachmentDeleteReadback, error) {
	seen := make(map[int64]struct{})
	var total int
	var totalPages int
	for page := 1; ; page++ {
		if page > attachmentDeleteMaxPages {
			return TaskAttachmentDeleteReadback{}, errors.New("attachment deletion scan exceeds the permitted page count")
		}
		envelope, err := c.getTaskAttachmentDeletePage(ctx, baseURL, token, taskID, page)
		if err != nil {
			return TaskAttachmentDeleteReadback{}, err
		}
		if envelope.Items == nil || envelope.Page == nil || envelope.PerPage == nil || envelope.Total == nil || envelope.TotalPages == nil ||
			*envelope.Page != page || *envelope.PerPage != attachmentDeletePageSize || *envelope.Total < 0 {
			return TaskAttachmentDeleteReadback{}, errors.New("attachment deletion scan response is incomplete")
		}
		if page == 1 {
			total, totalPages = *envelope.Total, *envelope.TotalPages
			wantPages := (total + attachmentDeletePageSize - 1) / attachmentDeletePageSize
			if totalPages != wantPages || totalPages > attachmentDeleteMaxPages {
				return TaskAttachmentDeleteReadback{}, errors.New("attachment deletion scan pagination is inconsistent")
			}
			if totalPages == 0 && len(*envelope.Items) == 0 {
				return TaskAttachmentDeleteReadback{}, nil
			}
		} else if *envelope.Total != total || *envelope.TotalPages != totalPages {
			return TaskAttachmentDeleteReadback{}, errors.New("attachment deletion scan pagination changed")
		}
		if page > totalPages {
			return TaskAttachmentDeleteReadback{}, errors.New("attachment deletion scan pagination is inconsistent")
		}
		wantItems := attachmentDeletePageSize
		if page == totalPages {
			wantItems = total - (page-1)*attachmentDeletePageSize
		}
		if len(*envelope.Items) != wantItems {
			return TaskAttachmentDeleteReadback{}, errors.New("attachment deletion scan item count is inconsistent")
		}
		var found *TaskAttachmentDeleteMeta
		for _, item := range *envelope.Items {
			if item.ID == nil || *item.ID < 1 || item.TaskID == nil || *item.TaskID != taskID || item.File == nil || item.File.Name == nil || strings.TrimSpace(*item.File.Name) == "" || item.File.Size == nil || *item.File.Size < 0 {
				return TaskAttachmentDeleteReadback{}, errors.New("attachment deletion scan contains an invalid item")
			}
			if _, duplicate := seen[*item.ID]; duplicate {
				return TaskAttachmentDeleteReadback{}, errors.New("attachment deletion scan contains duplicate ids")
			}
			seen[*item.ID] = struct{}{}
			if *item.ID == targetID {
				meta := TaskAttachmentDeleteMeta{ID: *item.ID, TaskID: *item.TaskID, Name: *item.File.Name, Size: *item.File.Size}
				found = &meta
			}
		}
		if found != nil { // Keep scanning: a later invalid/duplicate page must fail closed.
			if page == totalPages {
				return TaskAttachmentDeleteReadback{Present: true, Meta: *found}, nil
			}
			// Retain match through remaining pages.
			return c.scanRemainingTaskAttachmentDeletePages(ctx, baseURL, token, taskID, targetID, page+1, total, totalPages, seen, *found)
		}
		if page == totalPages {
			return TaskAttachmentDeleteReadback{}, nil
		}
	}
}

func (c *Client) scanRemainingTaskAttachmentDeletePages(ctx context.Context, baseURL, token string, taskID, targetID int64, page, total, totalPages int, seen map[int64]struct{}, found TaskAttachmentDeleteMeta) (TaskAttachmentDeleteReadback, error) {
	// Reuse the complete scanner validation by continuing with a small local scan.
	for ; page <= totalPages; page++ {
		envelope, err := c.getTaskAttachmentDeletePage(ctx, baseURL, token, taskID, page)
		if err != nil {
			return TaskAttachmentDeleteReadback{}, err
		}
		if envelope.Items == nil || envelope.Page == nil || envelope.PerPage == nil || envelope.Total == nil || envelope.TotalPages == nil || *envelope.Page != page || *envelope.PerPage != attachmentDeletePageSize || *envelope.Total != total || *envelope.TotalPages != totalPages {
			return TaskAttachmentDeleteReadback{}, errors.New("attachment deletion scan pagination is inconsistent")
		}
		want := attachmentDeletePageSize
		if page == totalPages {
			want = total - (page-1)*attachmentDeletePageSize
		}
		if len(*envelope.Items) != want {
			return TaskAttachmentDeleteReadback{}, errors.New("attachment deletion scan item count is inconsistent")
		}
		for _, item := range *envelope.Items {
			if item.ID == nil || *item.ID < 1 || item.TaskID == nil || *item.TaskID != taskID || item.File == nil || item.File.Name == nil || strings.TrimSpace(*item.File.Name) == "" || item.File.Size == nil || *item.File.Size < 0 {
				return TaskAttachmentDeleteReadback{}, errors.New("attachment deletion scan contains an invalid item")
			}
			if _, ok := seen[*item.ID]; ok {
				return TaskAttachmentDeleteReadback{}, errors.New("attachment deletion scan contains duplicate ids")
			}
			seen[*item.ID] = struct{}{}
		}
	}
	return TaskAttachmentDeleteReadback{Present: true, Meta: found}, nil
}

func (c *Client) getTaskAttachmentDeletePage(ctx context.Context, baseURL, token string, taskID int64, page int) (attachmentDeleteEnvelope, error) {
	target, err := taskAttachmentsListEndpoint(baseURL, taskID, page, attachmentDeletePageSize)
	if err != nil {
		return attachmentDeleteEnvelope{}, errors.New("cannot create attachment deletion scan request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return attachmentDeleteEnvelope{}, errors.New("cannot create request")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return attachmentDeleteEnvelope{}, errors.New("attachment deletion scan request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if err := responseError(response, "attachment deletion scan request"); err != nil {
			return attachmentDeleteEnvelope{}, err
		}
		return attachmentDeleteEnvelope{}, &StatusError{Operation: "attachment deletion scan request", StatusCode: response.StatusCode}
	}
	if response.ContentLength > attachmentDeleteResponseMaxBytes {
		return attachmentDeleteEnvelope{}, errors.New("attachment deletion scan response exceeds the permitted size")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, attachmentDeleteResponseMaxBytes+1))
	if err != nil || int64(len(body)) > attachmentDeleteResponseMaxBytes {
		return attachmentDeleteEnvelope{}, errors.New("attachment deletion scan response exceeds the permitted size")
	}
	var envelope attachmentDeleteEnvelope
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&envelope); err != nil {
		return attachmentDeleteEnvelope{}, errors.New("cannot decode attachment deletion scan response")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return attachmentDeleteEnvelope{}, errors.New("attachment deletion scan response has trailing content")
	}
	return envelope, nil
}
