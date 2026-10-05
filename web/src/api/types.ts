export type User = { login: string; name: string; avatarUrl: string };
export type Setup = { githubConfigured: boolean; appSlug: string; installUrl: string };
export type ImportApp = {
  token: string;
  appId: number;
  clientId: string;
  clientSecret: string;
  webhookSecret: string;
  privateKey: string;
};

export type ServiceKind = "app" | "postgres" | "mysql" | "mongo" | "redis";
export type ServiceStatus = "offline" | "deploying" | "active" | "failed" | "crashed" | "stopped";
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
  /** Cores; 0 = unlimited. */
  cpuLimit: number;
  /** Bytes; 0 = unlimited. */
  memoryLimit: number;
  autoDeploy: boolean;
  waitForCi: boolean;
  status: ServiceStatus;
  privateHost: string;
  domains: Domain[];
  volumes: Volume[];
  latestDeployment: Deployment | null;
  /** Set while a restore runs, or after one failed and left the data possibly partial. */
  restoreFence: RestoreFence | null;
  createdAt: string;
};

/**
 * A fenced service cannot be deployed, started, or restarted until the
 * restore finishes or the fence is cleared.
 */
export type RestoreFence = {
  restoreId: string;
  phase: "retaining" | "replacing" | "loading";
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
    | "cpuLimit"
    | "memoryLimit"
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

export type MetricsRange = "1h" | "6h" | "24h" | "7d";

/** Resource usage samples for a service's running container, evenly spaced from start. */
export type Metrics = {
  range: MetricsRange;
  start: string;
  step: number;
  cpuLimit: number;
  memoryLimit: number;
  cpu: (number | null)[];
  memory: (number | null)[];
  netRx: (number | null)[];
  netTx: (number | null)[];
  diskRead: (number | null)[];
  diskWrite: (number | null)[];
};

/** Resource usage of the whole host, evenly spaced from start. */
export type HostMetrics = {
  range: MetricsRange;
  start: string;
  step: number;
  cpus: number;
  memoryTotal: number;
  diskTotal: number;
  cpu: (number | null)[];
  memory: (number | null)[];
  diskUsed: (number | null)[];
  netRx: (number | null)[];
  netTx: (number | null)[];
  diskRead: (number | null)[];
  diskWrite: (number | null)[];
};

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

export type BackupCompression = "fastest" | "default" | "better" | "best";

/** A backup schedule and retention policy; nextRunAt is null while disabled. */
export type BackupPolicy = {
  enabled: boolean;
  /** Cron, UTC unless it starts with "CRON_TZ=<zone> ". */
  schedule: string;
  compression: BackupCompression;
  /** Local archives to keep; 0 only together with upload. */
  keepLocal: number;
  /** Ignored while S3 is not configured. */
  upload: boolean;
  keepRemote: number;
  nextRunAt: string | null;
};
export type BackupPolicyInput = Omit<BackupPolicy, "nextRunAt">;

/** "uploading": the archive is written and its S3 upload is in progress. */
export type BackupStatus = "queued" | "running" | "uploading" | "succeeded" | "failed";
export type BackupTrigger = "schedule" | "manual" | "pre-restore";
export type BackupMethod = "dump" | "volume" | "sqlite";

export type Backup = {
  id: string;
  /** Null for shed's own database. */
  serviceId: string | null;
  trigger: BackupTrigger;
  method: BackupMethod;
  status: BackupStatus;
  /** Download name, e.g. "postgres-20261004-030000.sql.zst". */
  fileName: string;
  /** Archive size in bytes. */
  size: number;
  encrypted: boolean;
  local: boolean;
  remote: boolean;
  remoteError: string;
  error: string;
  createdAt: string;
  finishedAt: string | null;
};

export type Restore = {
  id: string;
  serviceId: string;
  backupId: string;
  status: "running" | "succeeded" | "failed";
  error: string;
  createdAt: string;
  finishedAt: string | null;
};

/** The latest restore is null when the service never had one. */
export type ServiceBackups = { policy: BackupPolicy; backups: Backup[]; restore: Restore | null };
export type SystemBackups = { policy: BackupPolicy; backups: Backup[] };

export type S3Settings = {
  /** URL, e.g. "https://s3.us-east-1.amazonaws.com". */
  endpoint: string;
  region: string;
  bucket: string;
  prefix: string;
  accessKeyId: string;
  pathStyle: boolean;
  hasSecret: boolean;
};

export type BackupSettings = {
  s3: S3Settings | null;
  /** The recipient is "" until a key exists. */
  encryption: { enabled: boolean; recipient: string };
};

/** An omitted or empty secretAccessKey keeps the stored secret. A null s3 removes the destination. */
export type BackupSettingsInput = {
  s3: (Omit<S3Settings, "hasSecret"> & { secretAccessKey?: string }) | null;
  encryption: { enabled: boolean };
};

export const isBackupActive = (b: Pick<Backup, "status">): boolean =>
  b.status === "queued" || b.status === "running" || b.status === "uploading";
