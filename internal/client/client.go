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
	if err := projectsResponseError(response, "projects list request"); err != nil {
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
	if err := projectsResponseError(response, "project request"); err != nil {
		return Project{}, err
	}

	var project Project
	if err := json.NewDecoder(response.Body).Decode(&project); err != nil {
		return Project{}, errors.New("cannot decode project response")
	}
	return project, nil
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

func projectsResponseError(response *http.Response, operation string) error {
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
