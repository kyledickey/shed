-- Trace ID of a deployment's deploy span, empty while telemetry was off.
ALTER TABLE deployments ADD COLUMN trace_id TEXT NOT NULL DEFAULT '';
