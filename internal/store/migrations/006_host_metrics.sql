CREATE TABLE host_samples (
  ts INTEGER PRIMARY KEY, -- unix seconds
  cpu REAL NOT NULL,
  memory INTEGER NOT NULL,
  disk_used INTEGER NOT NULL,
  net_rx REAL NOT NULL,
  net_tx REAL NOT NULL,
  disk_read REAL NOT NULL,
  disk_write REAL NOT NULL
) WITHOUT ROWID;
