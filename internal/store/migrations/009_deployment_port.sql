-- The container port a deployment was started with, so routes follow the
-- running container rather than a port edited since. Existing deployments
-- take their service's current port.
ALTER TABLE deployments ADD COLUMN port INTEGER NOT NULL DEFAULT 0;
UPDATE deployments SET port = (SELECT port FROM services WHERE services.id = deployments.service_id);
