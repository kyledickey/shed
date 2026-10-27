-- Per-service overrides of the default telemetry caps.
CREATE TABLE telemetry_caps (
  service_id TEXT PRIMARY KEY REFERENCES services(id) ON DELETE CASCADE,
  spans_per_second INTEGER NOT NULL,
  logs_per_second INTEGER NOT NULL,
  series INTEGER NOT NULL
);

-- Docker timestamp of the last line of each container's output that
-- VictoriaLogs acknowledged.
CREATE TABLE log_cursors (
  container_id TEXT PRIMARY KEY,
  ts TEXT NOT NULL
);
