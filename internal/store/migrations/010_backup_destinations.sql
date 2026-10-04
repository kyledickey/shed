-- S3 destinations that backups were uploaded to. A destination's location
-- never changes, so a backup's object stays reachable after the settings
-- move to another bucket or prefix. The current one is named by the
-- backup.destination setting, which replaces backup.s3.
CREATE TABLE backup_destinations (
  id TEXT PRIMARY KEY,
  endpoint TEXT NOT NULL,
  region TEXT NOT NULL,
  bucket TEXT NOT NULL,
  prefix TEXT NOT NULL,                  -- without surrounding slashes
  path_style INTEGER NOT NULL,
  access_key_id TEXT NOT NULL,           -- credentials may be replaced
  secret_access_key TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (endpoint, region, bucket, prefix, path_style)
);

ALTER TABLE backups ADD COLUMN destination_id TEXT REFERENCES backup_destinations(id); -- NULL = not uploaded

INSERT INTO backup_destinations (id, endpoint, region, bucket, prefix, path_style,
  access_key_id, secret_access_key, created_at)
SELECT lower(hex(randomblob(6))),
  coalesce(json_extract(value, '$.endpoint'), ''),
  coalesce(json_extract(value, '$.region'), ''),
  coalesce(json_extract(value, '$.bucket'), ''),
  coalesce(json_extract(value, '$.prefix'), ''),
  coalesce(json_extract(value, '$.pathStyle'), 0),
  coalesce(json_extract(value, '$.accessKeyId'), ''),
  coalesce(json_extract(value, '$.secretAccessKey'), ''),
  strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM settings WHERE key = 'backup.s3' AND trim(value) <> '';

-- Uploaded backups can only have gone to the destination configured now.
UPDATE backups SET destination_id = (SELECT id FROM backup_destinations)
WHERE remote_key <> '';

INSERT INTO settings (key, value) SELECT 'backup.destination', id FROM backup_destinations;
DELETE FROM settings WHERE key = 'backup.s3';
