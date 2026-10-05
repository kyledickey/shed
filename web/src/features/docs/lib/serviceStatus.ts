import type { DeploymentStatus, ServiceStatus } from "../../../api/types";

/** StatusRuleId names the rule that produced a service status. */
export type StatusRuleId =
  | "in-progress"
  | "stopped"
  | "running"
  | "not-running"
  | "failed"
  | "offline";

/** The inputs shed derives a service's status from. */
export type StatusInputs = {
  /** Status of the service's newest deployment, or null if it never had one. */
  latest: DeploymentStatus | null;
  /** The user stopped the service. */
  stopped: boolean;
  /** The service has an active deployment; running says whether its container is. */
  active: { running: boolean } | null;
};

/** statusRules lists the rules in the order shed checks them. */
export const statusRules: {
  id: StatusRuleId;
  status: ServiceStatus;
  title: string;
  detail: string;
}[] = [
  {
    id: "in-progress",
    status: "deploying",
    title: "Latest deployment is in progress",
    detail: "queued, waiting, building or deploying",
  },
  {
    id: "stopped",
    status: "stopped",
    title: "The service is stopped",
    detail: "you pressed Stop and nothing has started it since",
  },
  {
    id: "running",
    status: "active",
    title: "Active deployment, container running",
    detail: "Docker says the container is up",
  },
  {
    id: "not-running",
    status: "crashed",
    title: "Active deployment, container not running",
    detail: "it exited, or was removed behind shed's back",
  },
  {
    id: "failed",
    status: "failed",
    title: "No active deployment, latest one failed",
    detail: "the first deploy never went live",
  },
  {
    id: "offline",
    status: "offline",
    title: "Anything else",
    detail: "no deployments yet, or the rest were canceled or skipped",
  },
];

const inProgress: ReadonlySet<DeploymentStatus> = new Set([
  "queued",
  "waiting",
  "building",
  "deploying",
]);

/** deriveServiceStatus derives a service status from its deployments and container. */
export function deriveServiceStatus(i: StatusInputs): {
  status: ServiceStatus;
  rule: StatusRuleId;
} {
  const rule = pickRule(i);
  const status = statusRules.find((r) => r.id === rule)!.status;
  return { status, rule };
}

function pickRule(i: StatusInputs): StatusRuleId {
  if (i.latest !== null && inProgress.has(i.latest)) return "in-progress";
  if (i.stopped) return "stopped";
  if (i.active) return i.active.running ? "running" : "not-running";
  if (i.latest === "failed") return "failed";
  return "offline";
}
