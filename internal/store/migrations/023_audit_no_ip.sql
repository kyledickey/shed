-- shed never writes a client IP to disk.
ALTER TABLE audit_events DROP COLUMN actor_ip;
INSERT INTO settings (key, value) VALUES ('store.purge_pending', '');
