package client

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
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
