-- OAuth 2.1 for agents (MCP clients). shed is the authorization server;
-- GitHub sign-in only identifies the user. Codes and tokens are stored as the
-- SHA-256 of their value, like sessions.

-- Clients registered through dynamic client registration. All are public
-- clients (no secret).
CREATE TABLE oauth_clients (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL DEFAULT '',
  uri TEXT NOT NULL DEFAULT '',
  redirect_uris TEXT NOT NULL,           -- JSON array
  created_at TEXT NOT NULL
);

-- One approved authorization: a user let a client act for them. Revoking a
-- grant deletes its tokens.
CREATE TABLE oauth_grants (
  id TEXT PRIMARY KEY,
  client_id TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
  github_id INTEGER NOT NULL REFERENCES users(github_id) ON DELETE CASCADE,
  redirect_uri TEXT NOT NULL,
  scope TEXT NOT NULL,                   -- space-separated
  resource TEXT NOT NULL,                -- audience of its access tokens
  created_at TEXT NOT NULL,
  last_used_at TEXT
);
CREATE INDEX oauth_grants_user ON oauth_grants(github_id, created_at DESC);
CREATE INDEX oauth_grants_client ON oauth_grants(client_id);

-- Authorization codes. A redeemed code is kept until it expires so that a
-- second redemption can revoke the grant it produced.
CREATE TABLE oauth_codes (
  code_hash TEXT PRIMARY KEY,
  client_id TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
  github_id INTEGER NOT NULL REFERENCES users(github_id) ON DELETE CASCADE,
  redirect_uri TEXT NOT NULL,
  code_challenge TEXT NOT NULL,          -- PKCE S256
  scope TEXT NOT NULL,
  resource TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  grant_id TEXT NOT NULL DEFAULT ''      -- set once redeemed
);

-- Access and refresh tokens. A rotated refresh token is kept until it expires
-- so that presenting it again revokes the grant.
CREATE TABLE oauth_tokens (
  token_hash TEXT PRIMARY KEY,
  grant_id TEXT NOT NULL REFERENCES oauth_grants(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,                    -- access | refresh
  expires_at TEXT NOT NULL,
  rotated INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX oauth_tokens_grant ON oauth_tokens(grant_id);
