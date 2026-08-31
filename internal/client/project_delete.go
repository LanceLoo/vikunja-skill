package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const deleteBodyLimit int64 = 4 << 20
const deletePageLimit = 100
const deleteResourceLimit = 100000
const deleteScanTimeout = 30 * time.Second

type deleteScanBudget struct{ requests, resources int }
type deleteBudgetKey struct{}

type DeleteProject struct {
	ID       int64 `json:"id"`
	ParentID int64 `json:"parent_project_id"`
}
type DeleteTask struct {
	ID        int64 `json:"id"`
	ProjectID int64 `json:"project_id"`
}
type DeleteView struct {
	ID        int64 `json:"id"`
	ProjectID int64 `json:"project_id"`
}
type DeleteBucket struct {
	ID        int64 `json:"id"`
	ProjectID int64 `json:"project_id"`
	ViewID    int64 `json:"view_id"`
}
type ProjectDeleteSnapshot struct {
	Projects         []DeleteProject `json:"projects"`
	Tasks            []DeleteTask    `json:"tasks"`
	Views            []DeleteView    `json:"views"`
	Buckets          []DeleteBucket  `json:"buckets"`
	RootID           int64           `json:"root_id"`
	Descendants      []int64         `json:"descendants"`
	Instance         string          `json:"instance"`
	DefaultProjectID int64           `json:"default_project_id"`
}

func (c *Client) ScanProjectDelete(ctx context.Context, base, token string, root int64) (ProjectDeleteSnapshot, error) {
	if ctx == nil || root < 1 {
		return ProjectDeleteSnapshot{}, errors.New("invalid delete scan")
	}
	ctx, cancel := context.WithTimeout(ctx, deleteScanTimeout)
	defer cancel()
	ctx = context.WithValue(ctx, deleteBudgetKey{}, &deleteScanBudget{})
	instance, err := DeleteInstanceIdentity(base)
	if err != nil {
		return ProjectDeleteSnapshot{}, err
	}
	projects, err := c.scanProjects(ctx, instance, token)
	if err != nil {
		return ProjectDeleteSnapshot{}, err
	}
	byID := map[int64]DeleteProject{}
	for _, p := range projects {
		if p.ID < 1 || p.ParentID < 0 || p.ID == p.ParentID {
			return ProjectDeleteSnapshot{}, errors.New("invalid project")
		}
		if _, ok := byID[p.ID]; ok {
			return ProjectDeleteSnapshot{}, errors.New("duplicate project")
		}
		byID[p.ID] = p
	}
	if _, ok := byID[root]; !ok {
		return ProjectDeleteSnapshot{}, errors.New("root absent")
	}
	selected := map[int64]bool{}
	for id := range byID {
		cur := id
		seen := map[int64]bool{}
		for cur != 0 {
			if seen[cur] {
				return ProjectDeleteSnapshot{}, errors.New("project cycle")
			}
			seen[cur] = true
			p, ok := byID[cur]
			if !ok {
				return ProjectDeleteSnapshot{}, errors.New("project orphan")
			}
			cur = p.ParentID
		}
		cur = id
		for cur != 0 && cur != root {
			cur = byID[cur].ParentID
		}
		if cur == root {
			selected[id] = true
		}
	}
	s := ProjectDeleteSnapshot{RootID: root, Instance: instance, Projects: []DeleteProject{}, Tasks: []DeleteTask{}, Views: []DeleteView{}, Buckets: []DeleteBucket{}, Descendants: []int64{}}
	for id := range selected {
		s.Projects = append(s.Projects, byID[id])
		if id != root {
			s.Descendants = append(s.Descendants, id)
		}
	}
	sort.Slice(s.Projects, func(i, j int) bool { return s.Projects[i].ID < s.Projects[j].ID })
	sort.Slice(s.Descendants, func(i, j int) bool { return s.Descendants[i] < s.Descendants[j] })
	for _, p := range s.Projects {
		ts, e := c.scanTasks(ctx, instance, token, p.ID)
		if e != nil {
			return s, e
		}
		for _, x := range ts {
			if x.ID < 1 || x.ProjectID != p.ID {
				return s, errors.New("task ownership")
			}
			s.Tasks = append(s.Tasks, x)
		}
		vs, e := c.scanViews(ctx, instance, token, p.ID)
		if e != nil {
			return s, e
		}
		for _, v := range vs {
			if v.ID < 1 || v.ProjectID != p.ID {
				return s, errors.New("view ownership")
			}
			s.Views = append(s.Views, v)
			bs, e := c.scanBuckets(ctx, instance, token, p.ID, v.ID)
			if e != nil {
				return s, e
			}
			for _, b := range bs {
				if b.ID < 1 || b.ViewID != v.ID {
					return s, errors.New("bucket ownership")
				}
				b.ProjectID = p.ID
				s.Buckets = append(s.Buckets, b)
			}
		}
	}
	sort.Slice(s.Tasks, func(i, j int) bool { return s.Tasks[i].ID < s.Tasks[j].ID })
	sort.Slice(s.Views, func(i, j int) bool { return s.Views[i].ID < s.Views[j].ID })
	sort.Slice(s.Buckets, func(i, j int) bool { return s.Buckets[i].ID < s.Buckets[j].ID })
	if duplicateIDs(s.Tasks) || duplicateIDs(s.Views) || duplicateIDs(s.Buckets) {
		return s, errors.New("duplicate resource")
	}
	if len(s.Projects)+len(s.Tasks)+len(s.Views)+len(s.Buckets) > deleteResourceLimit {
		return s, errors.New("resource limit")
	}
	if err := c.scanUser(ctx, instance, token, &s); err != nil {
		return s, err
	}
	if s.DefaultProjectID > 0 {
		if _, ok := byID[s.DefaultProjectID]; !ok {
			return s, errors.New("default absent")
		}
	}
	if selected[s.DefaultProjectID] {
		return s, errors.New("default in scope")
	}
	return s, nil
}

func DeleteInstanceIdentity(base string) (string, error) {
	u, e := url.Parse(base)
	if e != nil || !u.IsAbs() || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("invalid instance")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", errors.New("invalid instance")
	}
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	u.Host = net.JoinHostPort(strings.Trim(host, "[]"), port)
	if port == "" {
		u.Host = host
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(u.Path, "/api/v2") {
		u.Path += "/api/v2"
	}
	u.RawPath = ""
	return u.String(), nil
}
func deleteURL(instance, path string, page int, archived bool) string {
	q := "?page=" + strconv.Itoa(page) + "&per_page=1000"
	if archived {
		q += "&is_archived=true"
	}
	return strings.TrimRight(instance, "/") + "/" + path + q
}

func (c *Client) scanProjects(ctx context.Context, i, t string) ([]DeleteProject, error) {
	out := []DeleteProject{}
	e := c.pages(ctx, i, t, "projects", true, func(raw json.RawMessage) (int, error) {
		var a []json.RawMessage
		if err := strictArray(raw, &a); err != nil {
			return 0, err
		}
		for _, x := range a {
			f, e := strictObject(x, []string{"id", "parent_project_id"})
			if e != nil {
				return 0, e
			}
			id, e := intField(f, "id", false)
			if e != nil {
				return 0, e
			}
			parent, e := intField(f, "parent_project_id", true)
			if e != nil {
				return 0, e
			}
			out = append(out, DeleteProject{id, parent})
		}
		return len(a), nil
	})
	return out, e
}
func (c *Client) scanTasks(ctx context.Context, i, t string, p int64) ([]DeleteTask, error) {
	out := []DeleteTask{}
	e := c.pages(ctx, i, t, "projects/"+strconv.FormatInt(p, 10)+"/tasks", false, func(raw json.RawMessage) (int, error) {
		var a []json.RawMessage
		if e := strictArray(raw, &a); e != nil {
			return 0, e
		}
		for _, x := range a {
			f, e := strictObject(x, []string{"id", "project_id"})
			if e != nil {
				return 0, e
			}
			id, e := intField(f, "id", false)
			if e != nil {
				return 0, e
			}
			pid, e := intField(f, "project_id", false)
			if e != nil {
				return 0, e
			}
			out = append(out, DeleteTask{id, pid})
		}
		return len(a), nil
	})
	return out, e
}
func (c *Client) scanViews(ctx context.Context, i, t string, p int64) ([]DeleteView, error) {
	out := []DeleteView{}
	e := c.pages(ctx, i, t, "projects/"+strconv.FormatInt(p, 10)+"/views", false, func(raw json.RawMessage) (int, error) {
		var a []json.RawMessage
		if e := strictArray(raw, &a); e != nil {
			return 0, e
		}
		for _, x := range a {
			f, e := strictObject(x, []string{"id", "project_id"})
			if e != nil {
				return 0, e
			}
			id, e := intField(f, "id", false)
			if e != nil {
				return 0, e
			}
			pid, e := intField(f, "project_id", false)
			if e != nil {
				return 0, e
			}
			out = append(out, DeleteView{id, pid})
		}
		return len(a), nil
	})
	return out, e
}
func (c *Client) scanBuckets(ctx context.Context, i, t string, p, v int64) ([]DeleteBucket, error) {
	out := []DeleteBucket{}
	e := c.pages(ctx, i, t, "projects/"+strconv.FormatInt(p, 10)+"/views/"+strconv.FormatInt(v, 10)+"/buckets", false, func(raw json.RawMessage) (int, error) {
		var a []json.RawMessage
		if e := strictArray(raw, &a); e != nil {
			return 0, e
		}
		for _, x := range a {
			f, e := strictObject(x, []string{"id", "project_view_id"})
			if e != nil {
				return 0, e
			}
			id, e := intField(f, "id", false)
			if e != nil {
				return 0, e
			}
			vid, e := intField(f, "project_view_id", false)
			if e != nil {
				return 0, e
			}
			out = append(out, DeleteBucket{ID: id, ViewID: vid})
		}
		return len(a), nil
	})
	return out, e
}
func (c *Client) pages(ctx context.Context, i, t, path string, archived bool, read func(json.RawMessage) (int, error)) error {
	total := int64(-1)
	pages := int64(-1)
	seen := int64(0)
	for page := 1; page <= deletePageLimit; page++ {
		budget, ok := ctx.Value(deleteBudgetKey{}).(*deleteScanBudget)
		if ok && budget.requests >= deletePageLimit {
			return errors.New("aggregate request limit")
		}
		if ok {
			budget.requests++
		}
		raw, e := c.deleteGET(ctx, deleteURL(i, path, page, archived), t)
		if e != nil {
			return e
		}
		f, e := strictObject(raw, []string{"items", "total", "page", "per_page", "total_pages"})
		if e != nil {
			return errors.New("malformed pagination")
		}
		var rawItems []json.RawMessage
		if e := strictArray(f["items"], &rawItems); e != nil {
			return errors.New("malformed items")
		}
		if ok && budget.resources+len(rawItems) > deleteResourceLimit {
			return errors.New("aggregate resource limit")
		}
		if ok {
			budget.resources += len(rawItems)
		}
		n, e := read(f["items"])
		if e != nil {
			return errors.New("malformed items")
		}
		tot, e := intField(f, "total", false)
		if e != nil || tot < 0 {
			return errors.New("bad total")
		}
		tp, e := intField(f, "total_pages", false)
		if e != nil || tp < 0 || tp != (tot+999)/1000 {
			return errors.New("bad pages")
		}
		pn, e := intField(f, "page", false)
		if e != nil || pn != int64(page) {
			return errors.New("page mismatch")
		}
		pp, e := intField(f, "per_page", false)
		if e != nil || pp != 1000 {
			return errors.New("per page mismatch")
		}
		if total < 0 {
			total = tot
			pages = tp
		} else if total != tot || pages != tp {
			return errors.New("pagination changed")
		}
		seen += int64(n)
		if seen > total || seen > deleteResourceLimit {
			return errors.New("resource limit")
		}
		if total == 0 && page == 1 {
			return nil
		}
		if int64(page) < pages && n != 1000 {
			return errors.New("short intermediate page")
		}
		if int64(page) == pages {
			if seen != total {
				return errors.New("total mismatch")
			}
			return nil
		}
		if int64(page) >= pages {
			return errors.New("pagination mismatch")
		}
	}
	return errors.New("page limit")
}
func (c *Client) scanUser(ctx context.Context, i, t string, s *ProjectDeleteSnapshot) error {
	budget, ok := ctx.Value(deleteBudgetKey{}).(*deleteScanBudget)
	if ok && budget.requests >= deletePageLimit {
		return errors.New("aggregate request limit")
	}
	if ok {
		budget.requests++
	}
	raw, e := c.deleteGET(ctx, strings.TrimRight(i, "/")+"/user", t)
	if e != nil {
		return e
	}
	f, e := strictObject(raw, []string{"settings"})
	if e != nil {
		return e
	}
	sf, e := strictObject(f["settings"], []string{"default_project_id"})
	if e != nil {
		return e
	}
	id, e := intField(sf, "default_project_id", true)
	if e != nil || id < 0 {
		return errors.New("bad default")
	}
	s.DefaultProjectID = id
	return nil
}
func (c *Client) deleteGET(ctx context.Context, target, token string) ([]byte, error) {
	r, e := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if e != nil {
		return nil, e
	}
	r.Header.Set("Authorization", "Bearer "+token)
	res, e := c.httpClient.Do(r)
	if e != nil {
		return nil, errors.New("request failed")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, deleteBodyLimit+1))
	if e != nil || int64(len(b)) > deleteBodyLimit {
		return nil, errors.New("bad response")
	}
	return b, nil
}
func (c *Client) DeleteProject(ctx context.Context, base, token string, id int64) error {
	i, e := DeleteInstanceIdentity(base)
	if ctx == nil || e != nil || id < 1 {
		return errors.New("invalid delete")
	}
	r, e := http.NewRequestWithContext(ctx, http.MethodDelete, strings.TrimRight(i, "/")+"/projects/"+strconv.FormatInt(id, 10), nil)
	if e != nil {
		return e
	}
	r.Header.Set("Authorization", "Bearer "+token)
	res, e := c.httpClient.Do(r)
	if e != nil {
		return errors.New("delete request failed")
	}
	defer res.Body.Close()
	if res.StatusCode != 204 {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return nil
}
func (c *Client) ReadDeletedProject(ctx context.Context, base, token string, id int64) (int, error) {
	i, e := DeleteInstanceIdentity(base)
	if ctx == nil || e != nil || id < 1 {
		return 0, errors.New("invalid readback")
	}
	r, e := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(i, "/")+"/projects/"+strconv.FormatInt(id, 10), nil)
	if e != nil {
		return 0, e
	}
	r.Header.Set("Authorization", "Bearer "+token)
	res, e := c.httpClient.Do(r)
	if e != nil {
		return 0, errors.New("readback failed")
	}
	defer res.Body.Close()
	return res.StatusCode, nil
}
func strictObject(raw []byte, required []string) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	tok, e := d.Token()
	if e != nil {
		return nil, e
	}
	if x, ok := tok.(json.Delim); !ok || x != '{' {
		return nil, errors.New("not object")
	}
	m := map[string]json.RawMessage{}
	for d.More() {
		k, e := d.Token()
		if e != nil {
			return nil, e
		}
		name, ok := k.(string)
		if !ok {
			return nil, errors.New("bad key")
		}
		if _, ok := m[name]; ok {
			return nil, errors.New("duplicate key")
		}
		var v json.RawMessage
		if d.Decode(&v) != nil {
			return nil, errors.New("bad value")
		}
		m[name] = v
	}
	if _, e = d.Token(); e != nil {
		return nil, e
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("trailing")
	}
	for _, k := range required {
		if _, ok := m[k]; !ok {
			return nil, errors.New("missing")
		}
	}
	return m, nil
}
func strictArray(raw []byte, out *[]json.RawMessage) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("null is not array")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if d.Decode(out) != nil || d.Decode(&struct{}{}) != io.EOF {
		return errors.New("not array")
	}
	return nil
}
func intField(m map[string]json.RawMessage, key string, nullOK bool) (int64, error) {
	b := m[key]
	if nullOK && bytes.Equal(b, []byte("null")) {
		return 0, nil
	}
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		return 0, errors.New("null is not integer")
	}
	var n int64
	d := json.NewDecoder(bytes.NewReader(b))
	if d.Decode(&n) != nil || d.Decode(&struct{}{}) != io.EOF {
		return 0, errors.New("not integer")
	}
	return n, nil
}
func duplicateIDs[T any](v []T) bool {
	seen := map[int64]bool{}
	for _, x := range v {
		var id int64
		switch y := any(x).(type) {
		case DeleteTask:
			id = y.ID
		case DeleteView:
			id = y.ID
		case DeleteBucket:
			id = y.ID
		}
		if seen[id] {
			return true
		}
		seen[id] = true
	}
	return false
}
