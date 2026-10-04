export type User = { login: string; name: string; avatarUrl: string };
export type Setup = { githubConfigured: boolean; appSlug: string; installUrl: string };

export type ServiceKind = "app" | "postgres" | "mysql" | "mongo" | "redis";
export type ServiceStatus = "offline" | "deploying" | "active" | "failed" | "crashed";
export type DeploymentStatus =
  | "queued"
  | "waiting"
  | "building"
  | "deploying"
  | "active"
  | "failed"
  | "crashed"
  | "removed"
  | "canceled"
  | "skipped";

export type ServiceSummary = { id: string; name: string; kind: ServiceKind; status: ServiceStatus };

export type Project = {
  id: string;
  name: string;
  createdAt: string;
  services: ServiceSummary[];
};

/** GET /api/projects/{id} returns full services instead of summaries. */
export type ProjectDetail = Omit<Project, "services"> & { services: Service[] };

export type Service = {
  id: string;
  projectId: string;
  name: string;
  kind: ServiceKind;
  repo: string;
  branch: string;
  rootDir: string;
  image: string;
  dockerfilePath: string;
  startCommand: string;
  port: number;
  healthcheckPath: string;
  publicPort: number;
  autoDeploy: boolean;
  waitForCi: boolean;
  status: ServiceStatus;
  privateHost: string;
  domains: Domain[];
  volumes: Volume[];
  latestDeployment: Deployment | null;
  createdAt: string;
};

export type NewService = {
  name: string;
  kind: ServiceKind;
  repo?: string;
  branch?: string;
  image?: string;
};

export type ServicePatch = Partial<
  Pick<
    Service,
    | "repo"
    | "branch"
    | "rootDir"
    | "image"
    | "dockerfilePath"
    | "startCommand"
    | "port"
    | "healthcheckPath"
    | "publicPort"
    | "autoDeploy"
    | "waitForCi"
  >
>;

export type DeploymentTrigger = "push" | "manual" | "redeploy" | "create";

export type Deployment = {
  id: string;
  serviceId: string;
  status: DeploymentStatus;
  trigger: DeploymentTrigger;
  commitSha: string;
  commitMessage: string;
  commitAuthor: string;
  image: string;
  error: string;
  createdAt: string;
  startedAt: string | null;
  finishedAt: string | null;
};

export type Domain = { id: string; host: string; generated: boolean; url: string };
export type Volume = { id: string; mountPath: string; createdAt: string };
export type Repo = { fullName: string; defaultBranch: string; private: boolean };

export type Variables = Record<string, string>;

const pendingStatuses: ReadonlySet<DeploymentStatus> = new Set([
  "queued",
  "waiting",
  "building",
  "deploying",
]);

export function isPending(status: DeploymentStatus): boolean {
  return pendingStatuses.has(status);
}

export const databaseKinds = ["postgres", "mysql", "mongo", "redis"] as const;
