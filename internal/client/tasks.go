package client

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// Task is the minimal task representation returned by the Vikunja API.
type Task struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Done        bool   `json:"done"`
	ProjectID   int64  `json:"project_id"`
	Created     string `json:"created"`
	Updated     string `json:"updated"`
	raw         json.RawMessage
}

// PaginatedTasks is a page of tasks returned by the Vikunja API.
type PaginatedTasks struct {
	Items      []Task `json:"items"`
	Total      int    `json:"total"`
	Page       int    `json:"page"`
	PerPage    int    `json:"per_page"`
	TotalPages int    `json:"total_pages"`
	raw        json.RawMessage
}

// TaskListOptions controls a task list request.
type TaskListOptions struct {
	Page               int
	PerPage            int
	ProjectID          int64
	Query              string
	Filter             string
	FilterTimezone     string
	FilterIncludeNulls bool
	SortBy             []string
	OrderBy            []string
}

func (t *Task) UnmarshalJSON(data []byte) error {
	type task Task
	var decoded task
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*t = Task(decoded)
	t.raw = append(t.raw[:0], data...)
	return nil
}
func (t Task) MarshalJSON() ([]byte, error) {
	if t.raw != nil {
		return t.raw, nil
	}
	type task Task
	return json.Marshal(task(t))
}
func (p *PaginatedTasks) UnmarshalJSON(data []byte) error {
	type paginatedTasks PaginatedTasks
	var decoded paginatedTasks
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*p = PaginatedTasks(decoded)
	p.raw = append(p.raw[:0], data...)
	return nil
}
func (p PaginatedTasks) MarshalJSON() ([]byte, error) {
	if p.raw != nil {
		return p.raw, nil
	}
	type paginatedTasks PaginatedTasks
	return json.Marshal(paginatedTasks(p))
}

// ListTasks retrieves one page of tasks, optionally scoped to a project.
func (c *Client) ListTasks(baseURL, token string, options TaskListOptions) (PaginatedTasks, error) {
	if err := validateTaskListOptions(options); err != nil {
		return PaginatedTasks{}, err
	}
	target, err := tasksListEndpoint(baseURL, options)
	if err != nil {
		return PaginatedTasks{}, errors.New("cannot create tasks request")
	}
	response, err := c.get(target, token)
	if err != nil {
		return PaginatedTasks{}, err
	}
	defer response.Body.Close()
	if err := responseError(response, "tasks list request"); err != nil {
		return PaginatedTasks{}, err
	}
	var tasks PaginatedTasks
	if err := json.NewDecoder(response.Body).Decode(&tasks); err != nil {
		return PaginatedTasks{}, errors.New("cannot decode tasks response")
	}
	return tasks, nil
}

// GetTask retrieves a task by its API identifier.
func (c *Client) GetTask(baseURL, token string, id int64) (Task, error) {
	if id < 1 {
		return Task{}, errors.New("task id must be positive")
	}
	response, err := c.get(taskEndpoint(baseURL, id), token)
	if err != nil {
		return Task{}, err
	}
	defer response.Body.Close()
	if err := responseError(response, "task request"); err != nil {
		return Task{}, err
	}
	var task Task
	if err := json.NewDecoder(response.Body).Decode(&task); err != nil {
		return Task{}, errors.New("cannot decode task response")
	}
	return task, nil
}
func tasksListEndpoint(baseURL string, o TaskListOptions) (string, error) {
	path := "tasks"
	if o.ProjectID > 0 {
		path = "projects/" + strconv.FormatInt(o.ProjectID, 10) + "/tasks"
	}
	u, err := url.Parse(endpoint(baseURL, path))
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("page", strconv.Itoa(o.Page))
	q.Set("per_page", strconv.Itoa(o.PerPage))
	if o.Query != "" {
		q.Set("q", o.Query)
	}
	if o.Filter != "" {
		q.Set("filter", o.Filter)
	}
	if o.FilterTimezone != "" {
		q.Set("filter_timezone", o.FilterTimezone)
	}
	if o.FilterIncludeNulls {
		q.Set("filter_include_nulls", "true")
	}
	for _, v := range o.SortBy {
		q.Add("sort_by", v)
	}
	for _, v := range o.OrderBy {
		q.Add("order_by", v)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
func taskEndpoint(baseURL string, id int64) string {
	return endpoint(baseURL, "tasks") + "/" + strconv.FormatInt(id, 10)
}
func validateTaskListOptions(o TaskListOptions) error {
	if o.Page < 1 || o.PerPage < 1 || o.PerPage > 1000 {
		return errors.New("page must be positive, and per_page must be between 1 and 1000")
	}
	if o.ProjectID < 0 {
		return errors.New("project id must not be negative")
	}
	if len(o.SortBy) != len(o.OrderBy) {
		return errors.New("sort_by and order_by must have the same number of values")
	}
	for i := range o.SortBy {
		if strings.TrimSpace(o.SortBy[i]) == "" || strings.TrimSpace(o.OrderBy[i]) == "" {
			return errors.New("sort_by and order_by values must not be empty")
		}
		if o.OrderBy[i] != "asc" && o.OrderBy[i] != "desc" {
			return errors.New("order_by values must be asc or desc")
		}
	}
	return nil
}
