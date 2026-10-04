CREATE TABLE metric_samples (
  service_id TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
  ts INTEGER NOT NULL, -- unix seconds
  cpu REAL NOT NULL,
  memory INTEGER NOT NULL,
  net_rx REAL NOT NULL,
  net_tx REAL NOT NULL,
  disk_read REAL NOT NULL,
  disk_write REAL NOT NULL,
  PRIMARY KEY (service_id, ts)
) WITHOUT ROWID;
