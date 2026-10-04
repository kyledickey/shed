-- Per-service container limits: CPU in cores, memory in bytes; 0 = unlimited.
ALTER TABLE services ADD COLUMN cpu_limit REAL NOT NULL DEFAULT 1;
ALTER TABLE services ADD COLUMN memory_limit INTEGER NOT NULL DEFAULT 1073741824;
