-- Backup policies, backup archives, and restore runs.
CREATE TABLE backup_policies (           -- absent row = default policy
  service_id TEXT PRIMARY KEY REFERENCES services(id) ON DELETE CASCADE,
  enabled INTEGER NOT NULL,
  schedule TEXT NOT NULL,                -- cron expression, UTC unless CRON_TZ=
  compression TEXT NOT NULL,             -- fastest | default | better | best
  keep_local INTEGER NOT NULL,           -- scheduled backups kept on disk
  upload INTEGER NOT NULL,               -- also store in S3 (when configured)
  keep_remote INTEGER NOT NULL           -- scheduled backups kept in S3
);

CREATE TABLE backups (
  id TEXT PRIMARY KEY,
  service_id TEXT REFERENCES services(id) ON DELETE CASCADE, -- NULL = shed.db
  trigger TEXT NOT NULL,                 -- schedule | manual | pre-restore
  method TEXT NOT NULL,                  -- dump | volume | sqlite
  status TEXT NOT NULL,                  -- queued | running | succeeded | failed
  file TEXT NOT NULL DEFAULT '',         -- archive file name, e.g. <id>.sql.zst.age
  size INTEGER NOT NULL DEFAULT 0,       -- archive bytes
  encrypted INTEGER NOT NULL DEFAULT 0,
  local INTEGER NOT NULL DEFAULT 0,      -- archive present under <data>/backups
  remote_key TEXT NOT NULL DEFAULT '',   -- S3 object key; '' = not uploaded
  remote_error TEXT NOT NULL DEFAULT '', -- last upload failure
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT
);
CREATE INDEX backups_service ON backups(service_id, created_at DESC);

CREATE TABLE restores (
  id TEXT PRIMARY KEY,
  service_id TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
  backup_id TEXT NOT NULL,               -- not a foreign key: the backup may be pruned later
  status TEXT NOT NULL,                  -- running | succeeded | failed
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  finished_at TEXT
);
CREATE INDEX restores_service ON restores(service_id, created_at DESC);
