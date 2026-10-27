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

export type DeploymentTrigger = "push" | "manual" | "redeploy" | "create" | "telemetry";

export type Deployment = {
  id: string;
  serviceId: string;
  status: DeploymentStatus;
  trigger: DeploymentTrigger;
  commitSha: string;
  commitMessage: string;
  commitAuthor: string;
  image: string;
  /** Whether the image is on the server; only set in deployment lists. */
  imageAvailable?: boolean;
  error: string;
  /** The deploy trace, "" if telemetry was off. */
  traceId: string;
  createdAt: string;
  startedAt: string | null;
  finishedAt: string | null;
};

export type Domain = { id: string; host: string; generated: boolean; url: string };
export type Volume = { id: string; mountPath: string; createdAt: string };
export type Repo = { fullName: string; defaultBranch: string; private: boolean };
/**
 * Whether variables are kept in 1Password, the vault's name, and how many
 * services have an item in it. The token is write-only: PUT /api/onepassword
 * takes it.
 */
export type OnePassword = { connected: boolean; vault: string; items: number };
/** A vault a service account token can access: POST /api/onepassword/vaults. */
export type OnePasswordVault = { id: string; name: string };

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

export type Release = {
  /** The tag, e.g. "v1.4.0". */
  version: string;
  /** The GitHub release page. */
  url: string;
  /** The release body, Markdown. */
  notes: string;
  publishedAt: string;
};

export type UpdateState = "idle" | "checking" | "downloading" | "restarting";

export type UpdateStatus = {
  /** The running version, e.g. "v1.3.2" or "dev". */
  current: string;
  /** Null until a check succeeds. */
  latest: Release | null;
  /** Whether latest is newer than current. */
  available: boolean;
  /** The last successful check. */
  checkedAt: string | null;
  state: UpdateState;
  /** The verified version ready to install, "" if none. */
  staged: string;
  /** The last check or download failure, "" if none. */
  error: string;
  autoDownload: boolean;
  /** Why this build cannot update itself, "" if it can. */
  unsupported: string;
};

/** A pending MCP client authorization, shown on the /authorize consent page. */
export type OAuthRequest = {
  id: string;
  /** Self-reported by the client at registration; not verified. */
  clientName: string;
  /** "" when the client registered none. */
  clientUri: string;
  /** The host of the redirect_uri the code will be sent to. */
  redirectHost: string;
  scopes: string[];
};

/** An MCP client the user has authorized, covering all the tokens issued to it. */
export type OAuthGrant = {
  id: string;
  clientName: string;
  redirectHost: string;
  scopes: string[];
  createdAt: string;
  /** Null until the client first uses an access token. */
  lastUsedAt: string | null;
};

export type AuditActorKind = "user" | "agent" | "github" | "system" | "docker" | "anonymous";
export type AuditOutcome = "ok" | "failed" | "denied";

/** One audit log event. detail holds names and changed fields or keys, never values. */
export type AuditEvent = {
  id: number;
  ts: string;
  actorKind: AuditActorKind;
  /** GitHub login of a user or agent, "" otherwise. */
  actor: string;
  actorIp: string;
  /** Dotted, e.g. service.variables.update. */
  action: string;
  outcome: AuditOutcome;
  projectId: string;
  serviceId: string;
  /** ID of what was acted on. */
  target: string;
  detail: Record<string, unknown>;
};

/** A page of audit events, newest first. Pass next as before for the next page; null at the end. */
export type AuditPage = { events: AuditEvent[]; next: number | null };

export type AuditSettings = { retentionDays: number };

export type TelemetryCaps = { spansPerSecond: number; logsPerSecond: number; series: number };

/** A service's telemetry: its caps and whether records are being dropped. */
export type TelemetrySettings = {
  enabled: boolean;
  metricsRetentionDays: number;
  logsRetentionDays: number;
  tracesRetentionDays: number;
  /** Disk for logs and traces together, 60/40. */
  diskLimitGB: number;
  defaultCaps: TelemetryCaps;
};

export type TelemetryComponentName = "metrics" | "logs" | "traces" | "grafana";

export type TelemetryComponent = {
  name: TelemetryComponentName;
  state: "running" | "starting" | "stopped" | "failed";
  version: string;
  /** The last failure starting it, "" if none. */
  error: string;
  cpuPercent: number | null;
  memoryBytes: number | null;
  memoryLimit: number;
  /** From the store's own metrics; null for grafana. */
  diskBytes: number | null;
};

export type DashboardLink = {
  uid: string;
  title: string;
  url: string;
  /** Saved by a user since shed wrote it; shed no longer overwrites it. */
  customized: boolean;
  /** Customized, and its default has changed since. */
  outdated: boolean;
};
export type ServiceTelemetry = {
  /** Effective: the service's own caps, or the defaults. */
  caps: TelemetryCaps;
  overridden: boolean;
  /** Records were dropped in the last 5 minutes. */
  dropping: boolean;
  /** "" while telemetry is off. */
  folderUrl: string;
  dashboards: DashboardLink[];
  firingAlerts: { name: string; since: string }[];
};

export type Telemetry = {
  settings: TelemetrySettings;
  /** Empty while never enabled. */
  components: TelemetryComponent[];
  /** Per second, over the last minute. */
  ingest: { spans: number; logs: number; points: number };
  droppedLastHour: { reason: string; signal: string; count: number }[];
  firingAlerts: { name: string; serviceId: string; since: string }[];
  /** "<server.url>/grafana/" */
  grafanaUrl: string;
  /** "http://shed.internal:4318" */
  otlpEndpoint: string;
};

/** One stored log record of a service: GET /api/services/{id}/logs/history. */
export type LogRecord = {
  time: string;
  source: "container" | "otlp";
  stream: "stdout" | "stderr" | "";
  level: string;
  message: string;
  deploymentId: string;
  traceId: string;
  fields: Record<string, string>;
};

/** A page of log records, newest first; next is the before cursor, "" at the end. */
export type LogPage = { records: LogRecord[]; next: string };
