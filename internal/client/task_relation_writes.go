package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
)

const relationSnapshotMaxResponseBytes int64 = 8 << 20

var taskRelationInverses = map[string]string{
	"subtask":     "parenttask",
	"parenttask":  "subtask",
	"related":     "related",
	"duplicateof": "duplicates",
	"duplicates":  "duplicateof",
	"blocking":    "blocked",
	"blocked":     "blocking",
	"precedes":    "follows",
	"follows":     "precedes",
	"copiedfrom":  "copiedto",
	"copiedto":    "copiedfrom",
}

// ValidTaskRelationKind reports whether kind is an API-supported relation kind.
func ValidTaskRelationKind(kind string) bool {
	_, ok := taskRelationInverses[kind]
	return ok
}

// InverseTaskRelationKind returns the inverse API relation kind for kind.
func InverseTaskRelationKind(kind string) (string, bool) {
	inverse, ok := taskRelationInverses[kind]
	return inverse, ok
}

// TaskRelationSnapshot is a validated, point-in-time view of a task's relations.
// Raw preserves the related_tasks JSON exactly as it appeared in the response.
type TaskRelationSnapshot struct {
	Raw       TaskRelations
	relations map[int64][]string
}

// Has reports whether otherTaskID has the exact relation kind.
func (s TaskRelationSnapshot) Has(kind string, otherTaskID int64) bool {
	for _, candidate := range s.relations[otherTaskID] {
		if candidate == kind {
			return true
		}
	}
	return false
}

// HasAnyDirectRelation reports whether otherTaskID has any direct relation.
func (s TaskRelationSnapshot) HasAnyDirectRelation(otherTaskID int64) bool {
	return len(s.relations[otherTaskID]) != 0
}

// KindsFor returns the sorted relation kinds for otherTaskID.
func (s TaskRelationSnapshot) KindsFor(otherTaskID int64) []string {
	kinds := append([]string(nil), s.relations[otherTaskID]...)
	sort.Strings(kinds)
	return kinds
}

// CreateTaskRelationInput contains the two tasks and exact relation kind to create.
type CreateTaskRelationInput struct {
	TaskID       int64
	OtherTaskID  int64
	RelationKind string
}

// GetTaskRelationSnapshot retrieves and strictly validates a task's related_tasks object.
func (c *Client) GetTaskRelationSnapshot(baseURL, token string, taskID int64) (TaskRelationSnapshot, error) {
	if taskID < 1 {
		return TaskRelationSnapshot{}, errors.New("task id must be positive")
	}
	response, err := c.get(endpoint(baseURL, "tasks")+"/"+strconv.FormatInt(taskID, 10), token)
	if err != nil {
		return TaskRelationSnapshot{}, err
	}
	defer response.Body.Close()
	if err := responseError(response, "task relation snapshot request"); err != nil {
		return TaskRelationSnapshot{}, err
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, relationSnapshotMaxResponseBytes+1))
	if err != nil || int64(len(body)) > relationSnapshotMaxResponseBytes {
		return TaskRelationSnapshot{}, errors.New("cannot decode task relation snapshot response")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	var detail map[string]json.RawMessage
	if err := decoder.Decode(&detail); err != nil || detail == nil {
		return TaskRelationSnapshot{}, errors.New("cannot decode task relation snapshot response")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return TaskRelationSnapshot{}, errors.New("cannot decode task relation snapshot response")
	}
	if !validSnapshotSourceID(detail, taskID) {
		return TaskRelationSnapshot{}, errors.New("cannot decode task relation snapshot response")
	}
	raw, present := detail["related_tasks"]
	if !present || isJSONNull(raw) {
		return TaskRelationSnapshot{}, errors.New("task relation snapshot missing related_tasks")
	}
	relations, err := parseTaskRelations(raw, taskID)
	if err != nil {
		return TaskRelationSnapshot{}, err
	}
	return TaskRelationSnapshot{Raw: TaskRelations(raw), relations: relations}, nil
}

// CreateTaskRelation creates one direct task relation and returns its HTTP status.
func (c *Client) CreateTaskRelation(baseURL, token string, input CreateTaskRelationInput) (int, error) {
	if err := validateTaskRelation(input.TaskID, input.OtherTaskID, input.RelationKind); err != nil {
		return 0, err
	}
	payload, err := json.Marshal(struct {
		OtherTaskID  int64  `json:"other_task_id"`
		RelationKind string `json:"relation_kind"`
	}{input.OtherTaskID, input.RelationKind})
	if err != nil {
		return 0, errors.New("cannot encode task relation request")
	}
	return c.writeTaskRelation(http.MethodPost, endpoint(baseURL, "tasks")+"/"+strconv.FormatInt(input.TaskID, 10)+"/relations", token, bytes.NewReader(payload), "application/json", "task relation create request", http.StatusOK, http.StatusCreated)
}

// DeleteTaskRelation deletes one direct task relation and returns its HTTP status.
func (c *Client) DeleteTaskRelation(baseURL, token string, taskID int64, relationKind string, otherTaskID int64) (int, error) {
	if err := validateTaskRelation(taskID, otherTaskID, relationKind); err != nil {
		return 0, err
	}
	target := endpoint(baseURL, "tasks") + "/" + strconv.FormatInt(taskID, 10) + "/relations/" + relationKind + "/" + strconv.FormatInt(otherTaskID, 10)
	return c.writeTaskRelation(http.MethodDelete, target, token, nil, "", "task relation delete request", http.StatusOK, http.StatusNoContent)
}

func (c *Client) writeTaskRelation(method, target, token string, body io.Reader, contentType, operation string, accepted ...int) (int, error) {
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		return 0, errors.New("cannot create request")
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request to %s failed", safeURL(target))
	}
	defer response.Body.Close()
	for _, status := range accepted {
		if response.StatusCode == status {
			return response.StatusCode, nil
		}
	}
	if err := responseError(response, operation); err != nil {
		return 0, err
	}
	return 0, &StatusError{Operation: operation, StatusCode: response.StatusCode}
}

func validateTaskRelation(taskID, otherTaskID int64, relationKind string) error {
	if taskID < 1 || otherTaskID < 1 {
		return errors.New("task ids must be positive")
	}
	if taskID == otherTaskID {
		return errors.New("task ids must be different")
	}
	if !ValidTaskRelationKind(relationKind) {
		return errors.New("invalid task relation kind")
	}
	return nil
}

func parseTaskRelations(raw json.RawMessage, sourceTaskID int64) (map[int64][]string, error) {
	var buckets map[string]json.RawMessage
	if err := json.Unmarshal(raw, &buckets); err != nil || buckets == nil {
		return nil, errors.New("invalid task relation snapshot")
	}
	relations := make(map[int64][]string)
	for kind, bucket := range buckets {
		if !ValidTaskRelationKind(kind) || isJSONNull(bucket) {
			return nil, errors.New("invalid task relation snapshot")
		}
		var items []json.RawMessage
		if err := json.Unmarshal(bucket, &items); err != nil || items == nil {
			return nil, errors.New("invalid task relation snapshot")
		}
		seenBucket := make(map[int64]struct{}, len(items))
		for _, item := range items {
			if isJSONNull(item) {
				return nil, errors.New("invalid task relation snapshot")
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(item, &object); err != nil || object == nil {
				return nil, errors.New("invalid task relation snapshot")
			}
			idRaw, present := object["id"]
			if !present || isJSONNull(idRaw) {
				return nil, errors.New("invalid task relation snapshot")
			}
			var id int64
			if err := json.Unmarshal(idRaw, &id); err != nil || id < 1 || id == sourceTaskID {
				return nil, errors.New("invalid task relation snapshot")
			}
			if _, duplicate := seenBucket[id]; duplicate || len(relations[id]) != 0 {
				return nil, errors.New("invalid task relation snapshot")
			}
			seenBucket[id] = struct{}{}
			relations[id] = append(relations[id], kind)
		}
	}
	return relations, nil
}

func validSnapshotSourceID(detail map[string]json.RawMessage, taskID int64) bool {
	raw, present := detail["id"]
	if !present || isJSONNull(raw) {
		return false
	}
	var id int64
	return json.Unmarshal(raw, &id) == nil && id > 0 && id == taskID
}

func isJSONNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }
