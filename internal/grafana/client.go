package grafana

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	// ErrNotFound is returned when a folder or dashboard does not exist.
	ErrNotFound = errors.New("grafana: not found")
	// ErrConflict is returned when a dashboard changed since the version a
	// write expected, or already exists when the write expected to create
	// it. Grafana answers 412 or, since version 12, 409.
	ErrConflict = errors.New("grafana: version conflict")
)

// Client calls Grafana's HTTP API as [AdminUser]. It is safe for concurrent
// use.
type Client struct {
	base     string
	password string
	hc       *http.Client
}

// New returns a client for the Grafana at baseURL, which includes the
// sub-path Grafana serves from, such as "http://127.0.0.1:3001/grafana".
func New(baseURL, password string, hc *http.Client) *Client {
	return &Client{base: strings.TrimSuffix(baseURL, "/"), password: password, hc: hc}
}

// Health is the answer of Grafana's health endpoint.
type Health struct {
	Database string `json:"database"`
	Version  string `json:"version"`
}

// Health reports whether Grafana is up and its database usable.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var h Health
	if err := c.do(ctx, http.MethodGet, "/api/health", nil, &h); err != nil {
		return Health{}, fmt.Errorf("grafana: health: %w", err)
	}
	if h.Database != "ok" {
		return h, fmt.Errorf("grafana: health: database %q", h.Database)
	}
	return h, nil
}

// Folder is a Grafana folder. An empty ParentUID is the root.
type Folder struct {
	UID       string `json:"uid"`
	Title     string `json:"title"`
	ParentUID string `json:"parentUid,omitempty"`
}

// The UIDs of shed's fixed folders.
const (
	ShedFolder     = "shed"
	ArchivedFolder = "archived"
)

// ProjectFolder returns the UID of a project's folder.
func ProjectFolder(projectID string) string { return "p-" + projectID }

// ServiceFolder returns the UID of a service's folder, which sits in its
// project's folder.
func ServiceFolder(serviceID string) string { return "s-" + serviceID }

// Folder returns the folder with uid, or [ErrNotFound].
func (c *Client) Folder(ctx context.Context, uid string) (Folder, error) {
	var f Folder
	if err := c.do(ctx, http.MethodGet, "/api/folders/"+url.PathEscape(uid), nil, &f); err != nil {
		return Folder{}, fmt.Errorf("grafana: get folder %s: %w", uid, err)
	}
	return f, nil
}

// EnsureFolder makes folder f.UID exist with f's title and parent: it
// creates the folder, or renames and moves it to match. The parent must
// already exist.
func (c *Client) EnsureFolder(ctx context.Context, f Folder) error {
	cur, err := c.Folder(ctx, f.UID)
	if errors.Is(err, ErrNotFound) {
		if err := c.do(ctx, http.MethodPost, "/api/folders", f, nil); err != nil {
			return fmt.Errorf("grafana: create folder %s: %w", f.UID, err)
		}
		return nil
	}
	if err != nil {
		return err
	}
	path := "/api/folders/" + url.PathEscape(f.UID)
	if cur.ParentUID != f.ParentUID {
		body := map[string]string{"parentUid": f.ParentUID}
		if err := c.do(ctx, http.MethodPost, path+"/move", body, nil); err != nil {
			return fmt.Errorf("grafana: move folder %s: %w", f.UID, err)
		}
	}
	if cur.Title != f.Title {
		body := map[string]any{"title": f.Title, "overwrite": true}
		if err := c.do(ctx, http.MethodPut, path, body, nil); err != nil {
			return fmt.Errorf("grafana: rename folder %s: %w", f.UID, err)
		}
	}
	return nil
}

// DashboardMeta describes a stored dashboard.
type DashboardMeta struct {
	UID       string
	Title     string
	Version   int
	FolderUID string
}

// Dashboard returns the dashboard with uid, or [ErrNotFound].
func (c *Client) Dashboard(ctx context.Context, uid string) (DashboardMeta, error) {
	var resp struct {
		Dashboard struct {
			UID     string `json:"uid"`
			Title   string `json:"title"`
			Version int    `json:"version"`
		} `json:"dashboard"`
		Meta struct {
			FolderUID string `json:"folderUid"`
		} `json:"meta"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/dashboards/uid/"+url.PathEscape(uid), nil, &resp); err != nil {
		return DashboardMeta{}, fmt.Errorf("grafana: get dashboard %s: %w", uid, err)
	}
	d := resp.Dashboard
	return DashboardMeta{UID: d.UID, Title: d.Title, Version: d.Version, FolderUID: resp.Meta.FolderUID}, nil
}

// PutDashboard saves dashboard, a dashboard model with a uid, into folder
// folderUID and returns Grafana's new version of it. replaces is the version
// the write expects to replace, or 0 to create the dashboard; if the stored
// dashboard differs, nothing is written and the error is [ErrConflict].
// Writing the stored content again keeps its version.
func (c *Client) PutDashboard(ctx context.Context, folderUID string, dashboard json.RawMessage, replaces int) (int, error) {
	var model map[string]json.RawMessage
	if err := json.Unmarshal(dashboard, &model); err != nil {
		return 0, fmt.Errorf("grafana: put dashboard: %w", err)
	}
	var uid string
	_ = json.Unmarshal(model["uid"], &uid)
	delete(model, "id")
	delete(model, "version")
	if replaces > 0 {
		model["version"] = json.RawMessage(fmt.Sprint(replaces))
	}
	body := map[string]any{"dashboard": model, "folderUid": folderUID, "overwrite": false, "message": "shed"}
	var resp struct {
		Version int `json:"version"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/dashboards/db", body, &resp); err != nil {
		return 0, fmt.Errorf("grafana: put dashboard %s: %w", uid, err)
	}
	return resp.Version, nil
}

// SearchQuery filters [Client.SearchDashboards]. Empty fields match all.
type SearchQuery struct {
	Query      string
	Tags       []string
	FolderUIDs []string
}

// DashboardHit is one search result.
type DashboardHit struct {
	UID       string   `json:"uid"`
	Title     string   `json:"title"`
	URL       string   `json:"url"`
	FolderUID string   `json:"folderUid"`
	Tags      []string `json:"tags"`
}

// SearchDashboards returns the dashboards matching q.
func (c *Client) SearchDashboards(ctx context.Context, q SearchQuery) ([]DashboardHit, error) {
	v := url.Values{"type": {"dash-db"}}
	if q.Query != "" {
		v.Set("query", q.Query)
	}
	for _, t := range q.Tags {
		v.Add("tag", t)
	}
	for _, f := range q.FolderUIDs {
		v.Add("folderUIDs", f)
	}
	var hits []DashboardHit
	if err := c.do(ctx, http.MethodGet, "/api/search?"+v.Encode(), nil, &hits); err != nil {
		return nil, fmt.Errorf("grafana: search dashboards: %w", err)
	}
	return hits, nil
}

// Annotation is an event shown on dashboards whose annotation queries match
// its tags. A zero End marks a point in time.
type Annotation struct {
	Time time.Time
	End  time.Time
	Tags []string
	Text string
}

// CreateAnnotation stores a and returns its ID.
func (c *Client) CreateAnnotation(ctx context.Context, a Annotation) (int64, error) {
	body := map[string]any{"time": a.Time.UnixMilli(), "tags": a.Tags, "text": a.Text}
	if !a.End.IsZero() {
		body["timeEnd"] = a.End.UnixMilli()
	}
	var resp struct {
		ID int64 `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/annotations", body, &resp); err != nil {
		return 0, fmt.Errorf("grafana: create annotation: %w", err)
	}
	return resp.ID, nil
}

// Alert is a firing instance of a Grafana-managed alert rule.
type Alert struct {
	Labels      map[string]string
	Annotations map[string]string
	ActiveAt    time.Time
	Value       string
}

// FiringAlerts returns the alert instances of Grafana-managed rules that
// are firing.
func (c *Client) FiringAlerts(ctx context.Context) ([]Alert, error) {
	var resp struct {
		Data struct {
			Alerts []struct {
				Labels      map[string]string `json:"labels"`
				Annotations map[string]string `json:"annotations"`
				State       string            `json:"state"`
				ActiveAt    *time.Time        `json:"activeAt"`
				Value       string            `json:"value"`
			} `json:"alerts"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/prometheus/grafana/api/v1/alerts", nil, &resp); err != nil {
		return nil, fmt.Errorf("grafana: list alerts: %w", err)
	}
	var alerts []Alert
	for _, a := range resp.Data.Alerts {
		// States are "Alerting", or "Alerting (Error)" and the like.
		if !strings.HasPrefix(a.State, "Alerting") {
			continue
		}
		alert := Alert{Labels: a.Labels, Annotations: a.Annotations, Value: a.Value}
		if a.ActiveAt != nil {
			alert.ActiveAt = *a.ActiveAt
		}
		alerts = append(alerts, alert)
	}
	return alerts, nil
}

// ReloadAlertRules makes Grafana read its alert rule provisioning files
// again.
func (c *Client) ReloadAlertRules(ctx context.Context) error {
	if err := c.do(ctx, http.MethodPost, "/api/admin/provisioning/alerting/reload", nil, nil); err != nil {
		return fmt.Errorf("grafana: reload alert rules: %w", err)
	}
	return nil
}

// do sends a JSON request and decodes a JSON response into out, if non-nil.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(AdminUser, c.password)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		switch resp.StatusCode {
		case http.StatusNotFound:
			return ErrNotFound
		case http.StatusPreconditionFailed, http.StatusConflict:
			return ErrConflict
		}
		return fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(msg))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
