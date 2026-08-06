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
