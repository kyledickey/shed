-- Audit log. No foreign keys: events outlive what they name.
CREATE TABLE audit_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts TEXT NOT NULL,
  actor_kind TEXT NOT NULL,
  actor TEXT NOT NULL DEFAULT '',
  actor_ip TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL,
  outcome TEXT NOT NULL,
  project_id TEXT NOT NULL DEFAULT '',
  service_id TEXT NOT NULL DEFAULT '',
  target TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_events_ts ON audit_events(ts);
CREATE INDEX audit_events_service ON audit_events(service_id, id);
