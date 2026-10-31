-- A service's link to the 1Password Environment it reads variables from,
-- with the service account token that reads it. The global token is not
-- copied: links made before keep their Environment and have no token.
CREATE TABLE onepassword_links (
  service_id  TEXT PRIMARY KEY REFERENCES services(id) ON DELETE CASCADE,
  environment TEXT NOT NULL,
  token       TEXT NOT NULL DEFAULT '', -- encrypted; '' = none
  created_at  TEXT NOT NULL
);

INSERT INTO onepassword_links (service_id, environment, created_at)
  SELECT id, onepassword_environment, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  FROM services WHERE onepassword_environment <> '';

ALTER TABLE services DROP COLUMN onepassword_environment;

DELETE FROM settings WHERE key = 'onepassword.token';
