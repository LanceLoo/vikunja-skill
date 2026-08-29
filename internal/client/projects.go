package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
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
	raw             json.RawMessage
}

// PaginatedProjects is a page of projects returned by the Vikunja API.
type PaginatedProjects struct {
	Items      []Project `json:"items"`
	Total      int       `json:"total"`
	Page       int       `json:"page"`
	PerPage    int       `json:"per_page"`
	TotalPages int       `json:"total_pages"`
	raw        json.RawMessage
}

// ProjectTitleUpdateSnapshot is the strictly validated project state used to
// evaluate root-project title updates.
type ProjectTitleUpdateSnapshot struct {
	Project Project
	ID      int64
	Title   string
	IsRoot  bool
}

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
func (p Project) MarshalJSON() ([]byte, error) {
	if p.raw != nil {
		return p.raw, nil
	}
	type project Project
	return json.Marshal(project(p))
}
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
func (p PaginatedProjects) MarshalJSON() ([]byte, error) {
	if p.raw != nil {
		return p.raw, nil
	}
	type paginatedProjects PaginatedProjects
	return json.Marshal(paginatedProjects(p))
}

// ListProjects retrieves one page of projects. Page and perPage must be positive, and the Vikunja API permits at most 1000 entries per page.
func (c *Client) ListProjects(baseURL, token string, page, perPage int) (PaginatedProjects, error) {
	if !validPagination(page, perPage) {
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

// CreateProject creates a root project with a title only.
func (c *Client) CreateProject(ctx context.Context, baseURL, token, title string) (Project, error) {
	if ctx == nil {
		return Project{}, errors.New("context must not be nil")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return Project{}, errors.New("project title must not be empty")
	}
	if utf8.RuneCountInString(title) > 250 {
		return Project{}, errors.New("project title must be at most 250 runes")
	}
	payload, err := json.Marshal(struct {
		Title string `json:"title"`
	}{Title: title})
	if err != nil {
		return Project{}, errors.New("cannot encode project request")
	}
	target := endpoint(baseURL, "projects")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return Project{}, errors.New("cannot create request")
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return Project{}, fmt.Errorf("request to %s failed", safeURL(target))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		if err := responseError(response, "project create request"); err != nil {
			return Project{}, err
		}
		return Project{}, &StatusError{Operation: "project create request", StatusCode: response.StatusCode}
	}
	decoder := json.NewDecoder(response.Body)
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return Project{}, errors.New("cannot decode project create response")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Project{}, errors.New("cannot decode project create response")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return Project{}, errors.New("cannot decode project create response")
	}
	idRaw, present := object["id"]
	if !present || bytes.Equal(bytes.TrimSpace(idRaw), []byte("null")) {
		return Project{}, errors.New("cannot decode project create response")
	}
	var id int64
	if err := json.Unmarshal(idRaw, &id); err != nil || id < 1 {
		return Project{}, errors.New("cannot decode project create response")
	}
	var project Project
	if err := json.Unmarshal(raw, &project); err != nil {
		return Project{}, errors.New("cannot decode project create response")
	}
	return project, nil
}

// GetProjectTitleUpdateSnapshot retrieves a project with the fields required
// to safely evaluate its root-project scope.
func (c *Client) GetProjectTitleUpdateSnapshot(baseURL, token string, id int64) (ProjectTitleUpdateSnapshot, error) {
	if id < 1 {
		return ProjectTitleUpdateSnapshot{}, errors.New("project id must be positive")
	}
	target := projectEndpoint(baseURL, id)
	response, err := c.get(target, token)
	if err != nil {
		return ProjectTitleUpdateSnapshot{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if err := responseError(response, "project title update snapshot request"); err != nil {
			return ProjectTitleUpdateSnapshot{}, err
		}
		return ProjectTitleUpdateSnapshot{}, &StatusError{Operation: "project title update snapshot request", StatusCode: response.StatusCode}
	}
	return decodeProjectTitleUpdateSnapshot(response.Body, id, "")
}

// UpdateProjectTitle updates exactly one project's title and strictly validates
// the direct response. IsRoot describes the returned state and does not by
// itself certify that the project was root before this request.
func (c *Client) UpdateProjectTitle(ctx context.Context, baseURL, token string, id int64, title string) (ProjectTitleUpdateSnapshot, error) {
	if ctx == nil {
		return ProjectTitleUpdateSnapshot{}, errors.New("context must not be nil")
	}
	if id < 1 {
		return ProjectTitleUpdateSnapshot{}, errors.New("project id must be positive")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return ProjectTitleUpdateSnapshot{}, errors.New("project title must not be empty")
	}
	if utf8.RuneCountInString(title) > 250 {
		return ProjectTitleUpdateSnapshot{}, errors.New("project title must be at most 250 runes")
	}
	payload, err := json.Marshal(struct {
		Title string `json:"title"`
	}{Title: title})
	if err != nil {
		return ProjectTitleUpdateSnapshot{}, errors.New("cannot encode project title update request")
	}
	target := projectEndpoint(baseURL, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, target, bytes.NewReader(payload))
	if err != nil {
		return ProjectTitleUpdateSnapshot{}, errors.New("cannot create request")
	}
	req.Header.Set("Content-Type", "application/merge-patch+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return ProjectTitleUpdateSnapshot{}, fmt.Errorf("request to %s failed", safeURL(target))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if err := responseError(response, "project title update request"); err != nil {
			return ProjectTitleUpdateSnapshot{}, err
		}
		return ProjectTitleUpdateSnapshot{}, &StatusError{Operation: "project title update request", StatusCode: response.StatusCode}
	}
	return decodeProjectTitleUpdateSnapshot(response.Body, id, title)
}

func decodeProjectTitleUpdateSnapshot(body io.Reader, expectedID int64, expectedTitle string) (ProjectTitleUpdateSnapshot, error) {
	const decodeError = "cannot decode project title update snapshot response"
	decoder := json.NewDecoder(body)
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return ProjectTitleUpdateSnapshot{}, errors.New(decodeError)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ProjectTitleUpdateSnapshot{}, errors.New(decodeError)
	}
	objectDecoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := objectDecoder.Token()
	if err != nil {
		return ProjectTitleUpdateSnapshot{}, errors.New(decodeError)
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return ProjectTitleUpdateSnapshot{}, errors.New(decodeError)
	}
	var id int64
	var title string
	var parentID int64
	var parentNull, haveID, haveTitle, haveParent bool
	for objectDecoder.More() {
		keyToken, err := objectDecoder.Token()
		if err != nil {
			return ProjectTitleUpdateSnapshot{}, errors.New(decodeError)
		}
		key, ok := keyToken.(string)
		if !ok {
			return ProjectTitleUpdateSnapshot{}, errors.New(decodeError)
		}
		var value json.RawMessage
		if err := objectDecoder.Decode(&value); err != nil {
			return ProjectTitleUpdateSnapshot{}, errors.New(decodeError)
		}
		switch key {
		case "id":
			if haveID || json.Unmarshal(value, &id) != nil || id < 1 {
				return ProjectTitleUpdateSnapshot{}, errors.New(decodeError)
			}
			haveID = true
		case "title":
			if haveTitle || bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &title) != nil {
				return ProjectTitleUpdateSnapshot{}, errors.New(decodeError)
			}
			haveTitle = true
		case "parent_project_id":
			if haveParent {
				return ProjectTitleUpdateSnapshot{}, errors.New(decodeError)
			}
			parentNull = bytes.Equal(bytes.TrimSpace(value), []byte("null"))
			if !parentNull && (json.Unmarshal(value, &parentID) != nil || parentID < 0) {
				return ProjectTitleUpdateSnapshot{}, errors.New(decodeError)
			}
			haveParent = true
		}
	}
	if _, err := objectDecoder.Token(); err != nil || !haveID || !haveTitle || !haveParent || id != expectedID || (expectedTitle != "" && title != expectedTitle) {
		return ProjectTitleUpdateSnapshot{}, errors.New(decodeError)
	}
	var project Project
	if err := json.Unmarshal(raw, &project); err != nil {
		return ProjectTitleUpdateSnapshot{}, errors.New(decodeError)
	}
	return ProjectTitleUpdateSnapshot{Project: project, ID: id, Title: title, IsRoot: parentNull || parentID == 0}, nil
}

func projectsListEndpoint(baseURL string, page, perPage int) (string, error) {
	u, err := url.Parse(endpoint(baseURL, "projects"))
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("page", strconv.Itoa(page))
	q.Set("per_page", strconv.Itoa(perPage))
	u.RawQuery = q.Encode()
	return u.String(), nil
}
func projectEndpoint(baseURL string, id int64) string {
	return endpoint(baseURL, "projects") + "/" + strconv.FormatInt(id, 10)
}
func validPagination(page, perPage int) bool { return page >= 1 && perPage >= 1 && perPage <= 1000 }
