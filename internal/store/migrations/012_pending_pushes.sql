-- The newest push to the branch an app service tracks, kept until it is
-- deployed so that a push received while the service is held, fenced, or
-- shed is restarting still deploys once the service can take it.
CREATE TABLE pending_pushes (
  service_id TEXT PRIMARY KEY REFERENCES services(id) ON DELETE CASCADE,
  id TEXT NOT NULL,                      -- changes with every newer push
  repo TEXT NOT NULL,
  branch TEXT NOT NULL,
  commit_sha TEXT NOT NULL,
  commit_message TEXT NOT NULL DEFAULT '',
  commit_author TEXT NOT NULL DEFAULT '',
  received_at TEXT NOT NULL,
  -- the service's latest deployment when the push arrived, or '' if none: a
  -- different latest deployment means the push was superseded
  prior_deployment_id TEXT NOT NULL DEFAULT ''
);
