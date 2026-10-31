-- Variables move from per-service 1Password Environment links to one item
-- per service in a vault. Links and their tokens are dropped, not converted.
DROP TABLE onepassword_links;

CREATE TABLE onepassword_items (
  service_id TEXT PRIMARY KEY REFERENCES services(id) ON DELETE CASCADE,
  item_id    TEXT NOT NULL
);

DELETE FROM settings WHERE key IN ('onepassword.token', 'onepassword.vault');
