package client

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
)

// Labels retains the complete paginated v2 label response, including unknown fields, null values, and schema metadata.
type Labels = json.RawMessage

// TaskComments retains the complete paginated v2 task comments response, including unknown fields, null values, and schema metadata.
type TaskComments = json.RawMessage

// TaskAttachments retains the complete paginated v2 task attachments response, including unknown fields, null values, and schema metadata.
type TaskAttachments = json.RawMessage

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
func labelsListEndpoint(baseURL, path string, page, perPage int, search, format string) (string, error) {
	u, err := url.Parse(endpoint(baseURL, path))
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("page", strconv.Itoa(page))
	q.Set("per_page", strconv.Itoa(perPage))
	if search != "" {
		q.Set("q", search)
	}
	if format != "" {
		q.Set("format", format)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
func taskCommentsListEndpoint(baseURL string, taskID int64, page, perPage int, search, orderBy string) (string, error) {
	u, err := url.Parse(endpoint(baseURL, "tasks") + "/" + strconv.FormatInt(taskID, 10) + "/comments")
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("page", strconv.Itoa(page))
	q.Set("per_page", strconv.Itoa(perPage))
	if search != "" {
		q.Set("q", search)
	}
	if orderBy != "" {
		q.Set("order_by", orderBy)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
func taskAttachmentsListEndpoint(baseURL string, taskID int64, page, perPage int) (string, error) {
	u, err := url.Parse(endpoint(baseURL, "tasks") + "/" + strconv.FormatInt(taskID, 10) + "/attachments")
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("page", strconv.Itoa(page))
	q.Set("per_page", strconv.Itoa(perPage))
	u.RawQuery = q.Encode()
	return u.String(), nil
}
func validLabelFormat(format string) bool {
	return format == "" || format == "html" || format == "markdown"
}
func validOrderBy(orderBy string) bool { return orderBy == "" || orderBy == "asc" || orderBy == "desc" }
