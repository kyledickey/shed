-- Overrides of the notification rule catalog. A rule without a row has its
-- defaults; a NULL threshold or for_seconds means the default.
CREATE TABLE notification_rules (
  id TEXT PRIMARY KEY,
  enabled INTEGER NOT NULL,
  threshold REAL,
  for_seconds INTEGER
);
