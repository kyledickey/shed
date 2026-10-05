-- A restore fence keeps a service stopped, across restarts, while a restore
-- changes its data, until the restore completes or is undone.
CREATE TABLE restore_fences (
  service_id TEXT PRIMARY KEY REFERENCES services(id) ON DELETE CASCADE,
  restore_id TEXT NOT NULL,
  phase TEXT NOT NULL,                   -- retaining | replacing | loading
  image TEXT NOT NULL,                   -- image of the helper containers
  volume_ids TEXT NOT NULL,              -- space-separated IDs of the volumes being replaced
  was_stopped INTEGER NOT NULL,          -- services.stopped before the restore
  created_at TEXT NOT NULL
);
