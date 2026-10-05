-- The container configuration the active deployment was started with, as
-- JSON, so a missing container is recreated as it was deployed rather than
-- with settings saved since. Empty for deployments from earlier versions and
-- for deployments that are not active.
ALTER TABLE deployments ADD COLUMN runtime TEXT NOT NULL DEFAULT '';
