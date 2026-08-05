// Package client provides read-only Vikunja health checks.
package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrAuthentication is retained as the common authentication failure class.
	ErrAuthentication = errors.New("authentication failed")
	// ErrUnauthorized indicates that the service did not accept the token or did
	// not receive the authentication header.
	ErrUnauthorized = fmt.Errorf("%w: token was not accepted or authorization header was not received", ErrAuthentication)
	// ErrForbidden indicates that the service recognized the token but rejected it
	// according to the instance or permission policy.
	ErrForbidden = fmt.Errorf("%w: token was rejected by instance or permission policy", ErrAuthentication)
	// ErrNotFound indicates that the requested API resource does not exist.
	ErrNotFound = errors.New("resource not found")
)

// Project is the minimal project representation returned by the Vikunja API.
type Project struct {
	ID              int64   `json:"id"`
	Title           string  `json:"title"`
	Description     string  `json:"description"`
	Identifier      string  `json:"identifier"`
	HexColor        string  `json:"hex_color"`
	ParentProjectID *int64  `json:"parent_project_id"`
	IsArchived      bool    `json:"is_archived"`
	IsFavorite      bool    `json:"is_favorite"`
	Position        float64 `json:"position"`
	MaxPermission   int     `json:"max_permission"`
	Created         string  `json:"created"`
	Updated         string  `json:"updated"`

	raw json.RawMessage
}

// PaginatedProjects is a page of projects returned by the Vikunja API.
type PaginatedProjects struct {
	Items      []Project `json:"items"`
	Total      int       `json:"total"`
	Page       int       `json:"page"`
	PerPage    int       `json:"per_page"`
	TotalPages int       `json:"total_pages"`

	raw json.RawMessage
}

// Task is the minimal task representation returned by the Vikunja API.
type Task struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Done        bool   `json:"done"`
	ProjectID   int64  `json:"project_id"`
	Created     string `json:"created"`
	Updated     string `json:"updated"`

	raw json.RawMessage
}

// PaginatedTasks is a page of tasks returned by the Vikunja API.
type PaginatedTasks struct {
	Items      []Task `json:"items"`
	Total      int    `json:"total"`
	Page       int    `json:"page"`
	PerPage    int    `json:"per_page"`
	TotalPages int    `json:"total_pages"`

	raw json.RawMessage
}

// Labels retains the complete paginated v2 label response, including unknown
// fields, null values, and schema metadata.
type Labels = json.RawMessage

// TaskComments retains the complete paginated v2 task comments response,
// including unknown fields, null values, and schema metadata.
type TaskComments = json.RawMessage

// TaskAttachments retains the complete paginated v2 task attachments response,
// including unknown fields, null values, and schema metadata.
type TaskAttachments = json.RawMessage

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

// UnmarshalJSON decodes known fields while retaining the complete API response
// for faithful re-encoding, including unknown and null fields.
func (p *Project) UnmarshalJSON(data []byte) error {
	type project Project
	var decoded project
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*p = Project(decoded)
	p.raw = append(p.raw[:0], data...)
	return nil
}

// MarshalJSON emits the original successful API representation when available.
func (p Project) MarshalJSON() ([]byte, error) {
	if p.raw != nil {
		return p.raw, nil
	}
	type project Project
	return json.Marshal(project(p))
}

// UnmarshalJSON decodes known pagination fields while retaining the complete
// API response for faithful re-encoding.
func (p *PaginatedProjects) UnmarshalJSON(data []byte) error {
	type paginatedProjects PaginatedProjects
	var decoded paginatedProjects
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*p = PaginatedProjects(decoded)
	p.raw = append(p.raw[:0], data...)
	return nil
}

// MarshalJSON emits the original successful API representation when available.
func (p PaginatedProjects) MarshalJSON() ([]byte, error) {
	if p.raw != nil {
		return p.raw, nil
	}
	type paginatedProjects PaginatedProjects
	return json.Marshal(paginatedProjects(p))
}

// UnmarshalJSON decodes known fields while retaining the complete API response
// for faithful re-encoding, including unknown and null fields.
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

// MarshalJSON emits the original successful API representation when available.
func (t Task) MarshalJSON() ([]byte, error) {
	if t.raw != nil {
		return t.raw, nil
	}
	type task Task
	return json.Marshal(task(t))
}

// UnmarshalJSON decodes known pagination fields while retaining the complete
// API response for faithful re-encoding.
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

// MarshalJSON emits the original successful API representation when available.
func (p PaginatedTasks) MarshalJSON() ([]byte, error) {
	if p.raw != nil {
		return p.raw, nil
	}
	type paginatedTasks PaginatedTasks
	return json.Marshal(paginatedTasks(p))
}

// Client performs Vikunja API checks.
type Client struct{ httpClient *http.Client }

// New constructs a client. A nil client uses a 10-second total request timeout.
func New(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	// Copy the supplied client so disabling redirects cannot alter the caller's
	// client. Redirects can otherwise forward the bearer token to another URL.
	clientCopy := *httpClient
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Client{httpClient: &clientCopy}
}

// CheckOpenAPI verifies that the OpenAPI document endpoint is reachable.
func (c *Client) CheckOpenAPI(baseURL string) error {
	response, err := c.get(endpoint(baseURL, "openapi.json"), "")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("OpenAPI request failed with HTTP status %d", response.StatusCode)
	}
	return nil
}

// VerifyTokenV1 verifies a token against the v1 API without exposing its value.
func (c *Client) VerifyTokenV1(baseURL, token string) error {
	return c.checkTokenEndpoint(tokenTestEndpoint(baseURL), token, "token validation")
}

// CheckProjectsRead verifies that the token can read projects through the v2 API.
func (c *Client) CheckProjectsRead(baseURL, token string) error {
	return c.checkTokenEndpoint(projectsEndpoint(baseURL), token, "projects read request")
}

// ListProjects retrieves one page of projects. Page and perPage must be
// positive, and the Vikunja API permits at most 1000 entries per page.
func (c *Client) ListProjects(baseURL, token string, page, perPage int) (PaginatedProjects, error) {
	if page <= 0 || perPage <= 0 || perPage > 1000 {
		return PaginatedProjects{}, errors.New("page and per_page must be positive, with per_page at most 1000")
	}

	target, err := projectsListEndpoint(baseURL, page, perPage)
	if err != nil {
		return PaginatedProjects{}, errors.New("cannot create projects request")
	}
	response, err := c.get(target, token)
	if err != nil {
		return PaginatedProjects{}, err
	}
	defer response.Body.Close()
	if err := responseError(response, "projects list request"); err != nil {
		return PaginatedProjects{}, err
	}

	var projects PaginatedProjects
	if err := json.NewDecoder(response.Body).Decode(&projects); err != nil {
		return PaginatedProjects{}, errors.New("cannot decode projects response")
	}
	return projects, nil
}

// GetProject retrieves a project by its API identifier.
func (c *Client) GetProject(baseURL, token string, id int64) (Project, error) {
	response, err := c.get(projectEndpoint(baseURL, id), token)
	if err != nil {
		return Project{}, err
	}
	defer response.Body.Close()
	if err := responseError(response, "project request"); err != nil {
		return Project{}, err
	}

	var project Project
	if err := json.NewDecoder(response.Body).Decode(&project); err != nil {
		return Project{}, errors.New("cannot decode project response")
	}
	return project, nil
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

// ListLabels retrieves one page of labels from the v2 API.
func (c *Client) ListLabels(baseURL, token string, page, perPage int, query, format string) (Labels, error) {
	if !validLabelFormat(format) {
		return nil, errors.New("format must be html or markdown")
	}
	return c.listLabels(baseURL, token, "labels", page, perPage, query, format, "labels list request")
}

// ListTaskLabels retrieves one page of labels assigned to a task from the v2 API.
func (c *Client) ListTaskLabels(baseURL, token string, taskID int64, page, perPage int, query string) (Labels, error) {
	if taskID < 1 {
		return nil, errors.New("task id must be positive")
	}
	return c.listLabels(baseURL, token, "tasks/"+strconv.FormatInt(taskID, 10)+"/labels", page, perPage, query, "", "task labels list request")
}

// ListTaskComments retrieves one page of comments assigned to a task from the v2 API.
func (c *Client) ListTaskComments(baseURL, token string, taskID int64, page, perPage int, query, orderBy string) (TaskComments, error) {
	if taskID < 1 {
		return nil, errors.New("task id must be positive")
	}
	if !validOrderBy(orderBy) {
		return nil, errors.New("order_by must be asc or desc")
	}
	if !validPagination(page, perPage) {
		return nil, errors.New("page and per_page must be positive, with per_page at most 1000")
	}
	target, err := taskCommentsListEndpoint(baseURL, taskID, page, perPage, query, orderBy)
	if err != nil {
		return nil, errors.New("cannot create task comments request")
	}
	response, err := c.get(target, token)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if err := responseError(response, "task comments list request"); err != nil {
		return nil, err
	}

	var comments TaskComments
	if err := json.NewDecoder(response.Body).Decode(&comments); err != nil {
		return nil, errors.New("cannot decode task comments response")
	}
	return comments, nil
}

// ListTaskAttachments retrieves one page of attachments assigned to a task from the v2 API.
func (c *Client) ListTaskAttachments(baseURL, token string, taskID int64, page, perPage int) (TaskAttachments, error) {
	if taskID < 1 {
		return nil, errors.New("task id must be positive")
	}
	if !validPagination(page, perPage) {
		return nil, errors.New("page and per_page must be positive, with per_page at most 1000")
	}
	target, err := taskAttachmentsListEndpoint(baseURL, taskID, page, perPage)
	if err != nil {
		return nil, errors.New("cannot create task attachments request")
	}
	response, err := c.get(target, token)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if err := responseError(response, "task attachments list request"); err != nil {
		return nil, err
	}

	var attachments TaskAttachments
	if err := json.NewDecoder(response.Body).Decode(&attachments); err != nil {
		return nil, errors.New("cannot decode task attachments response")
	}
	return attachments, nil
}

func (c *Client) listLabels(baseURL, token, path string, page, perPage int, query, format, operation string) (Labels, error) {
	if !validPagination(page, perPage) {
		return nil, errors.New("page and per_page must be positive, with per_page at most 1000")
	}
	target, err := labelsListEndpoint(baseURL, path, page, perPage, query, format)
	if err != nil {
		return nil, errors.New("cannot create labels request")
	}
	response, err := c.get(target, token)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if err := responseError(response, operation); err != nil {
		return nil, err
	}

	var labels Labels
	if err := json.NewDecoder(response.Body).Decode(&labels); err != nil {
		return nil, errors.New("cannot decode labels response")
	}
	return labels, nil
}

func (c *Client) checkTokenEndpoint(target, token, operation string) error {
	response, err := c.get(target, token)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		return ErrForbidden
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s failed with HTTP status %d", operation, response.StatusCode)
	}
	return nil
}

func (c *Client) get(target, token string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, errors.New("cannot create request")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		// Do not return transport text: implementations commonly echo the complete
		// request URL, and custom transports could include sensitive headers.
		return nil, fmt.Errorf("request to %s failed", safeURL(target))
	}
	return response, nil
}

func endpoint(baseURL, suffix string) string {
	return strings.TrimRight(baseURL, "/") + "/api/v2/" + suffix
}

func tokenTestEndpoint(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/api/v1/token/test"
}

func labelsListEndpoint(baseURL, path string, page, perPage int, search, format string) (string, error) {
	u, err := url.Parse(endpoint(baseURL, path))
	if err != nil {
		return "", err
	}
	query := u.Query()
	query.Set("page", strconv.Itoa(page))
	query.Set("per_page", strconv.Itoa(perPage))
	if search != "" {
		query.Set("q", search)
	}
	if format != "" {
		query.Set("format", format)
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func taskCommentsListEndpoint(baseURL string, taskID int64, page, perPage int, search, orderBy string) (string, error) {
	u, err := url.Parse(endpoint(baseURL, "tasks") + "/" + strconv.FormatInt(taskID, 10) + "/comments")
	if err != nil {
		return "", err
	}
	query := u.Query()
	query.Set("page", strconv.Itoa(page))
	query.Set("per_page", strconv.Itoa(perPage))
	if search != "" {
		query.Set("q", search)
	}
	if orderBy != "" {
		query.Set("order_by", orderBy)
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func taskAttachmentsListEndpoint(baseURL string, taskID int64, page, perPage int) (string, error) {
	u, err := url.Parse(endpoint(baseURL, "tasks") + "/" + strconv.FormatInt(taskID, 10) + "/attachments")
	if err != nil {
		return "", err
	}
	query := u.Query()
	query.Set("page", strconv.Itoa(page))
	query.Set("per_page", strconv.Itoa(perPage))
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func projectsEndpoint(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/api/v2/projects?page=1&per_page=1"
}

func projectsListEndpoint(baseURL string, page, perPage int) (string, error) {
	u, err := url.Parse(endpoint(baseURL, "projects"))
	if err != nil {
		return "", err
	}
	query := u.Query()
	query.Set("page", strconv.Itoa(page))
	query.Set("per_page", strconv.Itoa(perPage))
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func projectEndpoint(baseURL string, id int64) string {
	return endpoint(baseURL, "projects") + "/" + strconv.FormatInt(id, 10)
}

func tasksListEndpoint(baseURL string, options TaskListOptions) (string, error) {
	path := "tasks"
	if options.ProjectID > 0 {
		path = "projects/" + strconv.FormatInt(options.ProjectID, 10) + "/tasks"
	}
	u, err := url.Parse(endpoint(baseURL, path))
	if err != nil {
		return "", err
	}
	query := u.Query()
	query.Set("page", strconv.Itoa(options.Page))
	query.Set("per_page", strconv.Itoa(options.PerPage))
	if options.Query != "" {
		query.Set("q", options.Query)
	}
	if options.Filter != "" {
		query.Set("filter", options.Filter)
	}
	if options.FilterTimezone != "" {
		query.Set("filter_timezone", options.FilterTimezone)
	}
	if options.FilterIncludeNulls {
		query.Set("filter_include_nulls", "true")
	}
	for _, sortBy := range options.SortBy {
		query.Add("sort_by", sortBy)
	}
	for _, orderBy := range options.OrderBy {
		query.Add("order_by", orderBy)
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func taskEndpoint(baseURL string, id int64) string {
	return endpoint(baseURL, "tasks") + "/" + strconv.FormatInt(id, 10)
}

func validateTaskListOptions(options TaskListOptions) error {
	if options.Page < 1 || options.PerPage < 1 || options.PerPage > 1000 {
		return errors.New("page must be positive, and per_page must be between 1 and 1000")
	}
	if options.ProjectID < 0 {
		return errors.New("project id must not be negative")
	}
	if len(options.SortBy) != len(options.OrderBy) {
		return errors.New("sort_by and order_by must have the same number of values")
	}
	for i := range options.SortBy {
		if strings.TrimSpace(options.SortBy[i]) == "" || strings.TrimSpace(options.OrderBy[i]) == "" {
			return errors.New("sort_by and order_by values must not be empty")
		}
		if options.OrderBy[i] != "asc" && options.OrderBy[i] != "desc" {
			return errors.New("order_by values must be asc or desc")
		}
	}
	return nil
}

func validPagination(page, perPage int) bool {
	return page >= 1 && perPage >= 1 && perPage <= 1000
}

func validLabelFormat(format string) bool {
	return format == "" || format == "html" || format == "markdown"
}

func validOrderBy(orderBy string) bool {
	return orderBy == "" || orderBy == "asc" || orderBy == "desc"
}

func responseError(response *http.Response, operation string) error {
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		return ErrForbidden
	case http.StatusNotFound:
		return ErrNotFound
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= 300 {
		return fmt.Errorf("%s failed with HTTP status %d", operation, response.StatusCode)
	}
	return nil
}

func safeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "Vikunja endpoint"
	}
	u.RawQuery, u.Fragment = "", ""
	return u.String()
}
