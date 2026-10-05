-- A service has at most one active deployment. Earlier versions could leave
-- two after a crash during a switchover; keep the newest and mark the others
-- removed.
UPDATE deployments SET status = 'removed'
WHERE status = 'active' AND EXISTS (
  SELECT 1 FROM deployments AS newer
  WHERE newer.service_id = deployments.service_id AND newer.status = 'active'
    AND (newer.created_at > deployments.created_at
      OR (newer.created_at = deployments.created_at AND newer.rowid > deployments.rowid))
);
CREATE UNIQUE INDEX deployments_one_active ON deployments(service_id) WHERE status = 'active';
