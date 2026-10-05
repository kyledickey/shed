import type { DeploymentStatus, ServiceKind, ServiceStatus } from "../api/types";

/** Tone names a crayon from the design tokens; "neutral" is uncolored. */
export type Tone =
  | "neutral"
  | "accent"
  | "tangerine"
  | "sunflower"
  | "grass"
  | "teal"
  | "sky"
  | "grape"
  | "bubblegum"
  | "tomato";

export const crayons = [
  "tangerine",
  "sunflower",
  "grass",
  "teal",
  "sky",
  "grape",
  "bubblegum",
  "tomato",
] as const satisfies Tone[];

type StatusLook = { tone: Tone; label: string; live?: boolean };

const deploymentLooks: Record<DeploymentStatus, StatusLook> = {
  queued: { tone: "neutral", label: "Queued", live: true },
  waiting: { tone: "neutral", label: "Waiting for CI", live: true },
  building: { tone: "sunflower", label: "Building", live: true },
  deploying: { tone: "sky", label: "Deploying", live: true },
  active: { tone: "grass", label: "Active" },
  failed: { tone: "tomato", label: "Failed" },
  crashed: { tone: "tomato", label: "Crashed" },
  removed: { tone: "neutral", label: "Removed" },
  canceled: { tone: "neutral", label: "Canceled" },
  skipped: { tone: "neutral", label: "Skipped" },
};

const serviceLooks: Record<ServiceStatus, StatusLook> = {
  offline: { tone: "neutral", label: "Offline" },
  deploying: { tone: "sky", label: "Deploying", live: true },
  active: { tone: "grass", label: "Online" },
  failed: { tone: "tomato", label: "Failed" },
  crashed: { tone: "tomato", label: "Crashed" },
  stopped: { tone: "neutral", label: "Stopped" },
};

export function deploymentLook(status: DeploymentStatus): StatusLook {
  return deploymentLooks[status];
}

export function serviceLook(status: ServiceStatus): StatusLook {
  return serviceLooks[status];
}

export const kindTones: Record<ServiceKind, Tone> = {
  app: "grape",
  postgres: "sky",
  mysql: "tangerine",
  mongo: "grass",
  redis: "tomato",
};
