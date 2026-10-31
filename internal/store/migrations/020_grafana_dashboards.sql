-- Dashboards shed created in Grafana, to tell its own writes from a user's.
CREATE TABLE grafana_dashboards (
  uid TEXT PRIMARY KEY,
  template TEXT NOT NULL,
  folder TEXT NOT NULL,
  template_version INTEGER NOT NULL,
  grafana_version INTEGER NOT NULL,
  customized INTEGER NOT NULL DEFAULT 0
);
