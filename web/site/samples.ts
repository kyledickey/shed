/**
 * Sample data for the product pieces the public site shows. Never live data:
 * a made-up "acme" project on one server.
 */
import type { DeploymentStatus } from "../src/api/types";
import type { Stat } from "../src/features/metrics/StatStrip";
import type { Edge } from "../src/features/projects/graph";
import type { DiagramService } from "../src/features/projects/ProjectDiagram";

const sample = (
  name: string,
  kind: DiagramService["kind"],
  extra: Partial<DiagramService> = {},
): DiagramService => ({
  id: name,
  projectId: "acme",
  name,
  kind,
  status: "active",
  repo: "",
  branch: "main",
  image: "",
  privateHost: name,
  port: 0,
  domains: [],
  volumes: kind === "app" ? [] : [{}],
  latestDeployment: null,
  ...extra,
});

const minutesAgo = (m: number) => new Date(Date.now() - m * 60_000).toISOString();

/** services is a small production setup: a site, an API, a worker, and two databases. */
export const services: DiagramService[] = [
  sample("web", "app", {
    repo: "acme/web",
    port: 3000,
    domains: [{ id: "d1", host: "acme.dev" }],
    latestDeployment: { status: "active", createdAt: minutesAgo(60) },
  }),
  sample("api", "app", {
    repo: "acme/api",
    port: 8080,
    domains: [{ id: "d2", host: "api.acme.dev" }],
    latestDeployment: { status: "active", createdAt: minutesAgo(60) },
  }),
  sample("worker", "app", {
    repo: "acme/api",
    status: "deploying",
    latestDeployment: { status: "building", createdAt: minutesAgo(1) },
  }),
  sample("postgres", "postgres", { port: 5432 }),
  sample("redis", "redis", { port: 6379 }),
];

/** edges are which services use which. */
export const edges: Edge[] = [
  { from: "api", to: "postgres" },
  { from: "api", to: "redis" },
  { from: "worker", to: "postgres" },
  { from: "worker", to: "redis" },
];

/** SampleDeployment is one row of a service's deployment history. */
export type SampleDeployment = {
  sha: string;
  message: string;
  status: DeploymentStatus;
  trigger: string;
  author: string;
  when: string;
};

/** deployments is the recent history of the web service, newest first. */
export const deployments: SampleDeployment[] = [
  {
    sha: "8f3a2c1",
    message: "Add the pricing page",
    status: "active",
    trigger: "Push",
    author: "sam",
    when: "4 min ago",
  },
  {
    sha: "c07e9b4",
    message: "Upgrade to the new router",
    status: "failed",
    trigger: "Push",
    author: "sam",
    when: "26 min ago",
  },
  {
    sha: "e41b9d0",
    message: "Fix the signup redirect",
    status: "removed",
    trigger: "Push",
    author: "sam",
    when: "2 h ago",
  },
  {
    sha: "5d2f810",
    message: "First version",
    status: "removed",
    trigger: "Initial deploy",
    author: "sam",
    when: "3 days ago",
  },
];

/** variables are what the web service stores: references to its neighbours. */
export const variables: { key: string; value: string }[] = [
  { key: "DATABASE_URL", value: "${{ postgres.DATABASE_URL }}" },
  { key: "REDIS_URL", value: "${{ redis.REDIS_URL }}" },
  { key: "PUBLIC_URL", value: "https://${{ SHED_PUBLIC_DOMAIN }}" },
];

/** resolved is DATABASE_URL as the next deployment of web receives it, password masked. */
export const resolved = {
  key: "DATABASE_URL",
  before: "postgresql://postgres:",
  secret: "••••••••••••",
  after: "@postgres:5432/app",
};

/** wave is a deterministic, gently noisy series of n points around base. */
function wave(n: number, base: number, swing: number, seed: number): number[] {
  return Array.from({ length: n }, (_, i) => {
    const v =
      base +
      swing * Math.sin((i + seed) / 7) +
      (swing / 3) * Math.sin((i * 3 + seed) / 2.3) +
      (i > 40 && i < 46 ? swing * 1.6 : 0);
    return Math.max(0, Math.round(v * 10) / 10);
  });
}

const POINTS = 60;

/** stats are an hour of the web service's resource usage. */
export const stats: Stat[] = [
  { label: "CPU", value: "14%", of: "of 1 core", values: wave(POINTS, 12, 4, 1) },
  { label: "Memory", value: "182 MiB", of: "of 1 GiB", values: wave(POINTS, 180, 12, 5) },
  { label: "Network", value: "41 KB/s", of: "in + out", values: wave(POINTS, 38, 14, 9) },
  { label: "Disk", value: "3 KB/s", values: wave(POINTS, 4, 3, 13) },
];

/** statsStart is when the sample hour begins; one point a minute after it. */
export const statsStart = "2026-10-04T11:00:00Z";

/** statsStep is the seconds between sample points. */
export const statsStep = 60;
