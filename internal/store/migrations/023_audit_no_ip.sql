-- shed never writes a client IP to disk.
ALTER TABLE audit_events DROP COLUMN actor_ip;
