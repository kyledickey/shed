package store

import (
	"context"
	"fmt"
)

// GrafanaDashboard is shed's record of a dashboard it created in Grafana.
type GrafanaDashboard struct {
	UID      string
	Template string
	// Folder is the UID of the folder shed created the dashboard in.
	Folder string
	// TemplateVersion is the template version shed last wrote.
	TemplateVersion int
	// GrafanaVersion is Grafana's dashboard version after that write.
	GrafanaVersion int
	// Customized is set once a user saved the dashboard; shed no longer
	// overwrites it.
	Customized bool
}

// GrafanaDashboards returns every recorded dashboard.
func (s *Store) GrafanaDashboards(ctx context.Context) ([]GrafanaDashboard, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT uid, template, folder, template_version, grafana_version, customized
		FROM grafana_dashboards ORDER BY uid`)
	if err != nil {
		return nil, fmt.Errorf("store: list grafana dashboards: %w", err)
	}
	defer rows.Close()
	var out []GrafanaDashboard
	for rows.Next() {
		var d GrafanaDashboard
		if err := rows.Scan(&d.UID, &d.Template, &d.Folder, &d.TemplateVersion, &d.GrafanaVersion, &d.Customized); err != nil {
			return nil, fmt.Errorf("store: list grafana dashboards: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list grafana dashboards: %w", err)
	}
	return out, nil
}

// PutGrafanaDashboard creates or replaces the record of d.UID.
func (s *Store) PutGrafanaDashboard(ctx context.Context, d GrafanaDashboard) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO grafana_dashboards
		(uid, template, folder, template_version, grafana_version, customized) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (uid) DO UPDATE SET template = excluded.template, folder = excluded.folder,
		template_version = excluded.template_version, grafana_version = excluded.grafana_version,
		customized = excluded.customized`,
		d.UID, d.Template, d.Folder, d.TemplateVersion, d.GrafanaVersion, d.Customized)
	if err != nil {
		return fmt.Errorf("store: put grafana dashboard %s: %w", d.UID, err)
	}
	return nil
}

// ForgetGrafanaFolder deletes the records of the dashboards in folder.
func (s *Store) ForgetGrafanaFolder(ctx context.Context, folder string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM grafana_dashboards WHERE folder = ?`, folder); err != nil {
		return fmt.Errorf("store: forget grafana folder %s: %w", folder, err)
	}
	return nil
}

// ForgetGrafanaDashboard deletes the record of dashboard uid.
func (s *Store) ForgetGrafanaDashboard(ctx context.Context, uid string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM grafana_dashboards WHERE uid = ?`, uid); err != nil {
		return fmt.Errorf("store: forget grafana dashboard %s: %w", uid, err)
	}
	return nil
}

// DeleteGrafanaDashboards deletes every dashboard record.
func (s *Store) DeleteGrafanaDashboards(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM grafana_dashboards`); err != nil {
		return fmt.Errorf("store: delete grafana dashboards: %w", err)
	}
	return nil
}
