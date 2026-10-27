package observability

import (
	"context"
	"strings"

	"github.com/kyledickey/shed/internal/grafana"
)

// Dashboard is a Grafana dashboard.
type Dashboard struct {
	UID   string
	Title string
	// Folder is the title of the folder holding the dashboard.
	Folder string
	// Path is where the dashboard is served, "/grafana/d/<uid>".
	Path string
}

// Dashboards lists the dashboards in Grafana, or only those in folder
// folderUID when it is not empty. It fails with ErrDisabled while telemetry
// is off.
func (m *Manager) Dashboards(ctx context.Context, folderUID string) ([]Dashboard, error) {
	s, err := m.Settings(ctx)
	if err != nil {
		return nil, err
	}
	if !s.Enabled {
		return nil, ErrDisabled
	}
	pw, err := m.adminPassword(ctx)
	if err != nil {
		return nil, err
	}
	q := grafana.SearchQuery{}
	if folderUID != "" {
		q.FolderUIDs = []string{folderUID}
	}
	hits, err := m.cfg.Grafana(pw).SearchDashboards(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]Dashboard, 0, len(hits))
	for _, h := range hits {
		out = append(out, Dashboard{
			UID: h.UID, Title: h.Title, Folder: h.FolderTitle,
			Path: strings.TrimRight(grafanaPath, "/") + "/d/" + h.UID,
		})
	}
	return out, nil
}

// grafanaPath is where shed's proxy serves Grafana.
const grafanaPath = "/grafana/"
