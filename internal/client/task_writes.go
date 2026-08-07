package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// MaxBulkUpdateTaskIDs is the client-side upper bound of task IDs accepted
// in a single bulk update, matching the CLI limit.
const MaxBulkUpdateTaskIDs = 100

// CreateTaskInput contains the writable fields accepted when creating a task.
// It is deliberately separate from Task so API response and read-only fields
// cannot be sent back to the service.
type CreateTaskInput struct {
	Title       string
	Description *string
	Priority    *int
	DueDate     *string
}

// UpdateTaskInput contains the writable fields that may be changed on a task.
// Nil fields are omitted from the merge patch; non-nil fields are sent even
// when their values are empty strings, false, or zero.
type UpdateTaskInput struct {
	Title       *string
	Description *string
	Priority    *int
	DueDate     *string
}

// BulkUpdateTasksInput contains the task IDs, the explicit field names to
// write, and the matching writable values for a bulk task update.
type BulkUpdateTasksInput struct {
	TaskIDs []int64
	Fields  []string
	Values  UpdateTaskInput
}

// BulkUpdateTasksResult is the structured BulkTask response of a bulk update.
// Values reuses the existing Task model; read-only fields are only populated
// when the service returns them.
type BulkUpdateTasksResult struct {
	TaskIDs []int64  `json:"task_ids"`
	Fields  []string `json:"fields"`
	Values  Task     `json:"values"`
	Tasks   []Task   `json:"tasks"`
}

type createTaskRequest struct {
	Title       string  `json:"title"`
	Description *string `json:"description,omitempty"`
	Priority    *int    `json:"priority,omitempty"`
	DueDate     *string `json:"due_date,omitempty"`
}

type updateTaskRequest struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Priority    *int    `json:"priority,omitempty"`
	DueDate     *string `json:"due_date,omitempty"`
}

// CreateTask creates a task in projectID.
func (c *Client) CreateTask(ctx context.Context, baseURL, token string, projectID int64, input CreateTaskInput) (Task, error) {
	if err := validateCreateTask(projectID, input); err != nil {
		return Task{}, err
	}
	return c.writeTask(ctx, http.MethodPost, endpoint(baseURL, "projects")+"/"+strconv.FormatInt(projectID, 10)+"/tasks", token, createTaskRequest{
		Title: input.Title, Description: input.Description, Priority: input.Priority, DueDate: input.DueDate,
	}, "application/json", "task create request")
}

// UpdateTask applies the supplied writable fields to taskID.
func (c *Client) UpdateTask(ctx context.Context, baseURL, token string, taskID int64, input UpdateTaskInput) (Task, error) {
	if err := validateUpdateTask(taskID, input); err != nil {
		return Task{}, err
	}
	return c.writeTask(ctx, http.MethodPatch, taskEndpoint(baseURL, taskID), token, updateTaskRequest{
		Title: input.Title, Description: input.Description, Priority: input.Priority, DueDate: input.DueDate,
	}, "application/merge-patch+json", "task update request")
}

// CompleteTask marks taskID as done.
func (c *Client) CompleteTask(ctx context.Context, baseURL, token string, taskID int64) (Task, error) {
	if ctx == nil {
		return Task{}, errors.New("context must not be nil")
	}
	if taskID < 1 {
		return Task{}, errors.New("task id must be positive")
	}
	return c.writeTask(ctx, http.MethodPatch, taskEndpoint(baseURL, taskID), token, struct {
		Done bool `json:"done"`
	}{Done: true}, "application/merge-patch+json", "task complete request")
}

// BulkUpdateTasks applies the supplied writable fields to every task ID in a
// single request. The service rejects the whole batch if any involved project
// is not writable; there are no partial updates.
func (c *Client) BulkUpdateTasks(ctx context.Context, baseURL, token string, input BulkUpdateTasksInput) (BulkUpdateTasksResult, error) {
	if err := validateBulkUpdateTasks(input); err != nil {
		return BulkUpdateTasksResult{}, err
	}
	payload, err := json.Marshal(struct {
		TaskIDs []int64           `json:"task_ids"`
		Fields  []string          `json:"fields"`
		Values  updateTaskRequest `json:"values"`
	}{
		TaskIDs: input.TaskIDs,
		Fields:  input.Fields,
		Values: updateTaskRequest{
			Title: input.Values.Title, Description: input.Values.Description,
			Priority: input.Values.Priority, DueDate: input.Values.DueDate,
		},
	})
	if err != nil {
		return BulkUpdateTasksResult{}, errors.New("cannot encode task request")
	}
	target := endpoint(baseURL, "tasks/bulk")
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(payload))
	if err != nil {
		return BulkUpdateTasksResult{}, errors.New("cannot create request")
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return BulkUpdateTasksResult{}, fmt.Errorf("request to %s failed", safeURL(target))
	}
	defer response.Body.Close()
	if err := responseError(response, "task bulk update request"); err != nil {
		return BulkUpdateTasksResult{}, err
	}
	var result BulkUpdateTasksResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return BulkUpdateTasksResult{}, errors.New("cannot decode task bulk update response")
	}
	return result, nil
}

// DeleteTask deletes taskID with a single bodyless DELETE request. Success is
// HTTP 204 only; any other 2xx status is reported as a StatusError so callers
// never assume deletion from an ambiguous response. Response bodies are never
// read or included in errors.
func (c *Client) DeleteTask(ctx context.Context, baseURL, token string, taskID int64) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}
	if taskID < 1 {
		return errors.New("task id must be positive")
	}
	target := taskEndpoint(baseURL, taskID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, target, nil)
	if err != nil {
		return errors.New("cannot create request")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request to %s failed", safeURL(target))
	}
	defer response.Body.Close()
	if err := responseError(response, "task delete request"); err != nil {
		return err
	}
	if response.StatusCode != http.StatusNoContent {
		return &StatusError{Operation: "task delete request", StatusCode: response.StatusCode}
	}
	return nil
}

func (c *Client) writeTask(ctx context.Context, method, target, token string, body any, contentType, operation string) (Task, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return Task{}, errors.New("cannot encode task request")
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(payload))
	if err != nil {
		return Task{}, errors.New("cannot create request")
	}
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return Task{}, fmt.Errorf("request to %s failed", safeURL(target))
	}
	defer response.Body.Close()
	if err := responseError(response, operation); err != nil {
		return Task{}, err
	}
	var task Task
	if err := json.NewDecoder(response.Body).Decode(&task); err != nil {
		return Task{}, errors.New("cannot decode task response")
	}
	return task, nil
}

func validateCreateTask(projectID int64, input CreateTaskInput) error {
	if projectID < 1 {
		return errors.New("project id must be positive")
	}
	if strings.TrimSpace(input.Title) == "" {
		return errors.New("task title must not be empty")
	}
	if err := validateTaskPriority(input.Priority); err != nil {
		return err
	}
	return validateTaskDueDate(input.DueDate)
}

func validateUpdateTask(taskID int64, input UpdateTaskInput) error {
	if taskID < 1 {
		return errors.New("task id must be positive")
	}
	if input.Title == nil && input.Description == nil && input.Priority == nil && input.DueDate == nil {
		return errors.New("task update must include at least one field")
	}
	if input.Title != nil && strings.TrimSpace(*input.Title) == "" {
		return errors.New("task title must not be empty")
	}
	if err := validateTaskPriority(input.Priority); err != nil {
		return err
	}
	return validateTaskDueDate(input.DueDate)
}

func validateBulkUpdateTasks(input BulkUpdateTasksInput) error {
	if len(input.TaskIDs) == 0 {
		return errors.New("bulk update must include at least one task id")
	}
	if len(input.TaskIDs) > MaxBulkUpdateTaskIDs {
		return fmt.Errorf("bulk update must include at most %d task ids", MaxBulkUpdateTaskIDs)
	}
	seenIDs := make(map[int64]struct{}, len(input.TaskIDs))
	for _, id := range input.TaskIDs {
		if id < 1 {
			return errors.New("task id must be positive")
		}
		if _, duplicate := seenIDs[id]; duplicate {
			return errors.New("bulk update task ids must not contain duplicates")
		}
		seenIDs[id] = struct{}{}
	}
	if len(input.Fields) == 0 {
		return errors.New("bulk update must include at least one field")
	}
	valueByField := map[string]bool{
		"title":       input.Values.Title != nil,
		"description": input.Values.Description != nil,
		"priority":    input.Values.Priority != nil,
		"due_date":    input.Values.DueDate != nil,
	}
	seenFields := make(map[string]struct{}, len(input.Fields))
	for _, field := range input.Fields {
		if strings.TrimSpace(field) == "" {
			return errors.New("bulk update fields must not be empty")
		}
		if _, duplicate := seenFields[field]; duplicate {
			return errors.New("bulk update fields must not contain duplicates")
		}
		seenFields[field] = struct{}{}
		hasValue, supported := valueByField[field]
		if !supported {
			return fmt.Errorf("bulk update field %q is not writable", field)
		}
		if !hasValue {
			return fmt.Errorf("bulk update field %q has no matching value", field)
		}
	}
	for field, hasValue := range valueByField {
		if hasValue {
			if _, listed := seenFields[field]; !listed {
				return fmt.Errorf("bulk update value for %q is not listed in fields", field)
			}
		}
	}
	if input.Values.Title != nil && strings.TrimSpace(*input.Values.Title) == "" {
		return errors.New("task title must not be empty")
	}
	if err := validateTaskPriority(input.Values.Priority); err != nil {
		return err
	}
	return validateTaskDueDate(input.Values.DueDate)
}

func validateTaskPriority(priority *int) error {
	if priority != nil && *priority < 0 {
		return errors.New("task priority must not be negative")
	}
	return nil
}

func validateTaskDueDate(dueDate *string) error {
	if dueDate == nil {
		return nil
	}
	if *dueDate == "" {
		return errors.New("task due date must not be empty")
	}
	if _, err := time.Parse(time.RFC3339, *dueDate); err != nil {
		return errors.New("task due date must be RFC3339")
	}
	return nil
}
