-- A stopped service keeps its active deployment but its container is not run.
ALTER TABLE services ADD COLUMN stopped INTEGER NOT NULL DEFAULT 0;
