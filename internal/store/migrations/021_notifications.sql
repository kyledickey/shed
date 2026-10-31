-- Where shed sends notifications, and the outbox of messages waiting for them.
CREATE TABLE notification_channels (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  provider TEXT NOT NULL,
  target TEXT NOT NULL,                  -- encrypted (see Encryption at rest)
  enabled INTEGER NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE notification_deliveries (
  id TEXT PRIMARY KEY,
  channel_id TEXT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
  key TEXT NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  url TEXT NOT NULL,
  project TEXT NOT NULL,
  status TEXT NOT NULL,                  -- pending | sent | failed
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT NOT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  sent_at TEXT,
  UNIQUE (channel_id, key)
);
CREATE INDEX notification_deliveries_due ON notification_deliveries(status, next_attempt_at);
