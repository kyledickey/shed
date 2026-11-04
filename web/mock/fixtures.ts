// Seed data for the dev mock API. Everything is typed against the real
// contract in ../src/api/types.ts. Timestamps are relative to server start;
// randomness is hash-based so the data is the same on every start.
import type {
  AuditEvent,
  AuditActorKind,
  AuditOutcome,
  Backup,
  BackupPolicy,
  BackupSettings,
  Deployment,
  DeploymentStatus,
  DeploymentTrigger,
  LogRecord,
  NotificationChannel,
  NotificationDelivery,
  NotificationRule,
  OAuthGrant,
  OnePassword,
  Repo,
  Restore,
  Service,
  ServiceKind,
  ServiceStatus,
  Telemetry,
  TelemetryCaps,
  UpdateStatus,
  Variables,
} from "../src/api/types";

export const MIN = 60_000;
export const HOUR = 60 * MIN;
export const DAY = 24 * HOUR;
export const MiB = 1024 * 1024;
export const GiB = 1024 * MiB;

export const BOOT = Date.now();
export const iso = (ms: number) => new Date(ms).toISOString();

// ---------------------------------------------------------------- randomness

function fnv(s: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h >>> 0;
}

/** rand returns a deterministic number in [0, 1) for the given parts. */
export function rand(...parts: (string | number)[]): number {
  let h = fnv(parts.join("|"));
  h ^= h >>> 16;
  h = Math.imul(h, 0x85ebca6b);
  h ^= h >>> 13;
  h = Math.imul(h, 0xc2b2ae35);
  h ^= h >>> 16;
  return (h >>> 0) / 4294967296;
}

export function hex(seed: string, n: number): string {
  let out = "";
  for (let i = 0; out.length < n; i++) {
    out += Math.floor(rand(seed, i) * 0xffffffff)
      .toString(16)
      .padStart(8, "0");
  }
  return out.slice(0, n);
}

/** pick returns a deterministic element of a non-empty list. */
export function pick<T>(list: readonly T[], ...seed: (string | number)[]): T {
  return list[Math.floor(rand(...seed) * list.length) % list.length] as T;
}

export const newId = (prefix: string) =>
  `${prefix}_${Math.floor(Math.random() * 0xffffffff)
    .toString(36)
    .padStart(7, "0")}`;

// ------------------------------------------------------------------- session

export const me = {
  login: "kyledickey",
  name: "Kyle Dickey",
  avatarUrl: "https://github.com/kyledickey.png",
};

export const setup = {
  githubConfigured: true,
  appSlug: "shed-kyledickey",
  installUrl: "https://github.com/apps/shed-kyledickey/installations/new",
};

// ------------------------------------------------------------- services/data

type ServiceSeed = {
  id: string;
  name: string;
  kind: ServiceKind;
  status: ServiceStatus;
  ageDays: number;
  repo?: string;
  branch?: string;
  rootDir?: string;
  image?: string;
  dockerfilePath?: string;
  startCommand?: string;
  port: number;
  healthcheckPath?: string;
  cpuLimit?: number;
  memoryLimit?: number;
  autoDeploy?: boolean;
  waitForCi?: boolean;
  domains?: { host: string; generated?: boolean }[];
  volumes?: string[];
  variables: Variables;
};

type ProjectSeed = { id: string; name: string; ageDays: number; services: ServiceSeed[] };

const pgVars = (db: string): Variables => ({
  POSTGRES_USER: "postgres",
  POSTGRES_PASSWORD: hex(`pg-${db}`, 24),
  POSTGRES_DB: db,
  DATABASE_URL:
    "postgresql://${{POSTGRES_USER}}:${{POSTGRES_PASSWORD}}@${{SHED_PRIVATE_DOMAIN}}:5432/${{POSTGRES_DB}}",
});

const projectSeeds: ProjectSeed[] = [
  {
    id: "prj_k7x2m9a",
    name: "kyle.so",
    ageDays: 118,
    services: [
      {
        id: "svc_web4kq1",
        name: "web",
        kind: "app",
        status: "active",
        ageDays: 118,
        repo: "kyledickey/kyle.so",
        branch: "main",
        port: 3000,
        healthcheckPath: "/api/health",
        cpuLimit: 1,
        memoryLimit: 512 * MiB,
        autoDeploy: true,
        waitForCi: true,
        domains: [{ host: "kyle.so" }, { host: "web-production-4f2a.apps.kyle.so", generated: true }],
        variables: {
          NODE_ENV: "production",
          DATABASE_URL: "${{ postgres.DATABASE_URL }}",
          REDIS_URL: "${{ redis.REDIS_URL }}",
          SITE_URL: "https://${{ SHED_PUBLIC_DOMAIN }}",
          SESSION_SECRET: hex("session-kyleso", 48),
          GITHUB_TOKEN: "ghp_" + hex("ghp-kyleso", 36),
          PLAUSIBLE_DOMAIN: "kyle.so",
        },
      },
      {
        id: "svc_pg9d3wf",
        name: "postgres",
        kind: "postgres",
        status: "active",
        ageDays: 118,
        image: "postgres:17",
        port: 5432,
        memoryLimit: GiB,
        volumes: ["/var/lib/postgresql/data"],
        variables: pgVars("kyleso"),
      },
      {
        id: "svc_rd2h8nz",
        name: "redis",
        kind: "redis",
        status: "active",
        ageDays: 118,
        image: "redis:7-alpine",
        port: 6379,
        memoryLimit: 256 * MiB,
        volumes: ["/data"],
        variables: {
          REDIS_PASSWORD: hex("redis-kyleso", 24),
          REDIS_URL: "redis://default:${{REDIS_PASSWORD}}@${{SHED_PRIVATE_DOMAIN}}:6379",
        },
      },
    ],
  },
  {
    id: "prj_au5t1c6",
    name: "aretheyup",
    ageDays: 74,
    services: [
      {
        id: "svc_api7mz3",
        name: "api",
        kind: "app",
        status: "deploying",
        ageDays: 74,
        repo: "kyledickey/aretheyup",
        branch: "main",
        rootDir: "apps/api",
        dockerfilePath: "Dockerfile",
        port: 8080,
        healthcheckPath: "/healthz",
        cpuLimit: 2,
        memoryLimit: 768 * MiB,
        autoDeploy: true,
        waitForCi: false,
        domains: [{ host: "api.aretheyup.com" }],
        variables: {
          DATABASE_URL: "${{ postgres.DATABASE_URL }}",
          PROBE_REGIONS: "fra,sin,iad",
          INCIDENT_DEDUPE_SECONDS: "60",
          RESEND_API_KEY: "re_" + hex("resend", 32),
          SENTRY_DSN: "https://" + hex("sentry", 32) + "@o412345.ingest.sentry.io/6021184",
          LOG_LEVEL: "info",
        },
      },
      {
        id: "svc_wk1p6ra",
        name: "worker",
        kind: "app",
        status: "crashed",
        ageDays: 74,
        repo: "kyledickey/aretheyup",
        branch: "main",
        rootDir: "apps/worker",
        startCommand: "node dist/worker.js",
        port: 0,
        cpuLimit: 1,
        memoryLimit: 384 * MiB,
        autoDeploy: true,
        waitForCi: false,
        variables: {
          DATABASE_URL: "${{ postgres.DATABASE_URL }}",
          PROBE_CONCURRENCY: "64",
          PROBE_TIMEOUT_MS: "8000",
          LOG_LEVEL: "debug",
        },
      },
      {
        id: "svc_pg3k8vb",
        name: "postgres",
        kind: "postgres",
        status: "active",
        ageDays: 74,
        image: "postgres:17",
        port: 5432,
        memoryLimit: 2 * GiB,
        volumes: ["/var/lib/postgresql/data"],
        variables: pgVars("aretheyup"),
      },
      {
        id: "svc_sc5r2yd",
        name: "scheduler",
        kind: "app",
        status: "stopped",
        ageDays: 51,
        repo: "kyledickey/aretheyup",
        branch: "main",
        rootDir: "apps/scheduler",
        startCommand: "bun run cron.ts",
        port: 0,
        memoryLimit: 256 * MiB,
        autoDeploy: false,
        waitForCi: false,
        variables: {
          DATABASE_URL: "${{ postgres.DATABASE_URL }}",
          ROLLUP_CRON: "0 2 * * *",
          RETENTION_DAYS: "90",
        },
      },
    ],
  },
  {
    id: "prj_al8y4p2",
    name: "alias.party",
    ageDays: 203,
    services: [
      {
        id: "svc_al6n9qe",
        name: "alias",
        kind: "app",
        status: "active",
        ageDays: 203,
        image: "ghcr.io/kyledickey/alias-party:1.8.2",
        port: 8080,
        healthcheckPath: "/up",
        cpuLimit: 1,
        memoryLimit: 384 * MiB,
        domains: [
          { host: "alias.party" },
          { host: "www.alias.party" },
          { host: "alias-production-91c3.apps.kyle.so", generated: true },
        ],
        variables: {
          DATABASE_URL: "mysql://${{ mysql.MYSQL_USER }}:${{ mysql.MYSQL_PASSWORD }}@${{ mysql.SHED_PRIVATE_DOMAIN }}:3306/${{ mysql.MYSQL_DATABASE }}",
          MAILGUN_DOMAIN: "mg.alias.party",
          MAILGUN_API_KEY: "key-" + hex("mailgun", 32),
          RAILS_ENV: "production",
          SECRET_KEY_BASE: hex("skb", 64),
        },
      },
      {
        id: "svc_my4e7ts",
        name: "mysql",
        kind: "mysql",
        status: "active",
        ageDays: 203,
        image: "mysql:8.4",
        port: 3306,
        memoryLimit: GiB,
        volumes: ["/var/lib/mysql"],
        variables: {
          MYSQL_ROOT_PASSWORD: hex("mysql-root", 24),
          MYSQL_USER: "alias",
          MYSQL_PASSWORD: hex("mysql-pw", 24),
          MYSQL_DATABASE: "alias_production",
          DATABASE_URL:
            "mysql://${{MYSQL_USER}}:${{MYSQL_PASSWORD}}@${{SHED_PRIVATE_DOMAIN}}:3306/${{MYSQL_DATABASE}}",
        },
      },
    ],
  },
  {
    id: "prj_dm3w6k1",
    name: "dismegle",
    ageDays: 27,
    services: [
      {
        id: "svc_dm8b2ux",
        name: "dismegle",
        kind: "app",
        status: "active",
        ageDays: 27,
        repo: "kyledickey/dismegle",
        branch: "main",
        port: 3000,
        healthcheckPath: "/healthz",
        cpuLimit: 2,
        memoryLimit: 640 * MiB,
        autoDeploy: true,
        waitForCi: true,
        domains: [{ host: "dismegle.com" }, { host: "dismegle-production-27d1.apps.kyle.so", generated: true }],
        volumes: ["/app/uploads"],
        variables: {
          MONGODB_URL: "${{ mongo.MONGODB_URL }}",
          UPLOAD_DIR: "/app/uploads",
          MAX_ROOM_SIZE: "8",
          TURN_SECRET: hex("turn", 32),
          NODE_ENV: "production",
        },
      },
      {
        id: "svc_mg1c5jh",
        name: "mongo",
        kind: "mongo",
        status: "active",
        ageDays: 27,
        image: "mongo:8",
        port: 27017,
        memoryLimit: GiB,
        volumes: ["/data/db"],
        variables: {
          MONGO_INITDB_ROOT_USERNAME: "root",
          MONGO_INITDB_ROOT_PASSWORD: hex("mongo-pw", 24),
          MONGODB_URL:
            "mongodb://${{MONGO_INITDB_ROOT_USERNAME}}:${{MONGO_INITDB_ROOT_PASSWORD}}@${{SHED_PRIVATE_DOMAIN}}:27017",
        },
      },
    ],
  },
];

// ---------------------------------------------------------------- deployments

const commitMessages: Record<string, string[]> = {
  web: [
    "feat: add RSS feed for blog posts",
    "fix: correct og:image dimensions on post pages",
    "chore(deps): bump next from 15.4.2 to 15.4.5",
    "docs: update the uses page",
    "refactor: move markdown pipeline to rehype-pretty-code",
    "fix: flash of unstyled theme on first load",
    "perf: lazy-load the photo gallery",
    "feat: add /now page",
    "style: tighten prose spacing on mobile",
    "fix: 404 for trailing-slash post URLs",
    "feat: reading time on post cards",
    "chore: remove unused tailwind plugins",
    "fix: sitemap lastmod uses updatedAt",
    "feat: webmentions on posts",
  ],
  api: [
    "feat: add public status page endpoint",
    "fix: retry probe on ECONNRESET",
    "perf: batch probe result inserts",
    "feat: regional probes (fra, sin, iad)",
    "chore(deps): bump hono from 4.8.9 to 4.9.2",
    "fix: dedupe incidents opened within 60s",
    "refactor: extract probe scheduler",
    "test: cover the incident state machine",
    "feat: webhook delivery for incident updates",
    "fix: rate limit key ignored X-Forwarded-For",
    "feat: monitor groups",
    "fix: p95 latency rollup off by one bucket",
  ],
  worker: [
    "fix: handle null response bodies in probe worker",
    "feat: concurrent probes with p-limit",
    "perf: reuse undici agents per region",
    "fix: abort probes that exceed PROBE_TIMEOUT_MS",
    "chore(deps): bump undici from 7.2.0 to 7.3.0",
    "feat: TLS expiry checks",
    "fix: leak of sockets on redirect loops",
    "refactor: split probe result mapper",
    "feat: DNS resolution timing",
  ],
  scheduler: [
    "feat: nightly rollup of probe results",
    "fix: skip rollup when previous run is still going",
    "chore: pin bun to 1.2.19",
    "feat: prune results older than RETENTION_DAYS",
    "fix: timezone handling for ROLLUP_CRON",
    "refactor: share db client with worker",
    "feat: weekly uptime digest",
    "fix: log rollup duration",
  ],
  dismegle: [
    "feat: reaction emoji picker",
    "fix: websocket reconnect backoff",
    "feat: room size limit",
    "perf: stream uploads to disk instead of buffering",
    "chore(deps): bump socket.io from 4.8.0 to 4.8.1",
    "fix: duplicate join events after reconnect",
    "feat: moderation shortcuts",
    "fix: avatar upload rejects HEIC",
    "refactor: move presence to redis-less in-memory map",
    "feat: typing indicators",
    "fix: crash on empty room name",
  ],
};

const authorsFor = (msg: string, seed: string) =>
  msg.startsWith("chore(deps)")
    ? "dependabot[bot]"
    : rand(seed, "author") < 0.12
      ? "Maya Okafor"
      : "Kyle Dickey";

const failures = [
  'build: process "/bin/sh -c npm run build" did not complete successfully: exit code: 1',
  "deploy: health check GET /healthz did not return 2xx within 60s",
  "build: railpack: no start command could be found",
  "deploy: container exited with code 1 during startup",
  "waiting for CI: check run \"test\" failed",
];

/** LONG_HISTORY are the services with enough deployments or backups to page through. */
const LONG_HISTORY = new Set(["svc_api7mz3", "svc_web4kq1", "svc_pg9d3wf"]);

function genDeployments(svc: ServiceSeed, head: DeploymentStatus[], count: number): Deployment[] {
  const isImage = !svc.repo;
  const pool = commitMessages[svc.name] ?? commitMessages["web"] ?? [];
  const out: Deployment[] = [];
  const firstAge = head[0] === "building" ? 4 * MIN : 3 * HOUR + Math.floor(rand(svc.id, "first") * 5 * HOUR);
  const span = 13.4 * DAY - firstAge;
  for (let i = 0; i < count; i++) {
    const age = firstAge + span * Math.pow(i / Math.max(count - 1, 1), 1.35) + (i === 0 || i === count - 1 ? 0 : (rand(svc.id, i, "jit") - 0.5) * 5 * HOUR);
    const seed = `${svc.id}:${i}`;
    const created = BOOT - age;
    let status: DeploymentStatus;
    if (i < head.length) {
      status = head[i] as DeploymentStatus;
    } else {
      const r = rand(seed, "status");
      status = r < 0.68 ? "removed" : r < 0.8 ? "failed" : r < 0.88 ? "canceled" : "skipped";
      if (i === count - 1) status = "removed";
    }
    const msg = pool[(i * 5 + Math.floor(rand(svc.id, "m") * pool.length)) % pool.length] as string;
    const sha = hex(seed + "sha", 40);
    const trigger: DeploymentTrigger =
      i === count - 1
        ? "create"
        : isImage
          ? rand(seed, "t") < 0.3
            ? "redeploy"
            : "manual"
          : rand(seed, "t") < 0.08
            ? "redeploy"
            : rand(seed, "t") < 0.2
              ? "manual"
              : "push";
    const buildMs = isImage
      ? 6_000 + Math.floor(rand(seed, "d") * 12_000)
      : 45_000 + Math.floor(rand(seed, "d") * 140_000);
    const pending = status === "building" || status === "deploying" || status === "queued";
    const startedAt = status === "skipped" || status === "queued" ? null : created + 1500;
    const finishedAt = pending || status === "skipped" ? null : created + buildMs;
    const image = isImage
      ? (svc.image as string)
      : status === "failed" && rand(seed, "imgfail") < 0.6
        ? ""
        : pending && status !== "deploying"
          ? ""
          : `shed/${svc.name}:${sha.slice(0, 7)}`;
    out.push({
      id: `dep_${hex(seed + "id", 9)}`,
      serviceId: svc.id,
      status,
      trigger,
      commitSha: isImage ? "" : sha,
      commitMessage: isImage ? "" : msg,
      commitAuthor: isImage ? "" : authorsFor(msg, seed),
      image,
      imageAvailable: i < 4 && image !== "" && status !== "failed",
      error:
        status === "failed"
          ? (pick(failures, seed, "err") as string)
          : status === "crashed"
            ? "container exited with code 137 (out of memory)"
            : status === "canceled"
              ? "canceled by kyledickey"
              : status === "skipped"
                ? "superseded by a newer commit"
                : "",
      traceId: hex(seed + "trace", 32),
      createdAt: iso(created),
      startedAt: startedAt === null ? null : iso(startedAt),
      finishedAt: finishedAt === null ? null : iso(finishedAt),
    });
  }
  return out;
}

function headFor(svc: ServiceSeed): DeploymentStatus[] {
  switch (svc.status) {
    case "deploying":
      return ["building", "active"];
    case "crashed":
      return ["crashed"];
    case "stopped":
      return ["removed"];
    default:
      return ["active"];
  }
}

// --------------------------------------------------------------------- state

export type State = {
  projects: { id: string; name: string; createdAt: string }[];
  services: Map<string, Service>;
  deployments: Map<string, Deployment[]>;
  variables: Map<string, Variables>;
  /** Deployments whose build never finishes (the seeded in-progress one). */
  stuck: Set<string>;
  backups: Backup[];
  servicePolicies: Map<string, BackupPolicy>;
  systemPolicy: BackupPolicy;
  backupSettings: BackupSettings;
  restores: Map<string, Restore>;
  audit: AuditEvent[];
  auditRetentionDays: number;
  channels: (NotificationChannel & { target: string })[];
  rules: NotificationRule[];
  deliveries: NotificationDelivery[];
  telemetry: Telemetry;
  serviceCaps: Map<string, TelemetryCaps>;
  logHistory: Map<string, LogRecord[]>;
  update: UpdateStatus;
  grants: OAuthGrant[];
  onePassword: OnePassword;
};

function toService(p: ProjectSeed, s: ServiceSeed, deps: Deployment[]): Service {
  const created = BOOT - s.ageDays * DAY;
  return {
    id: s.id,
    projectId: p.id,
    name: s.name,
    kind: s.kind,
    repo: s.repo ?? "",
    branch: s.branch ?? "",
    rootDir: s.rootDir ?? "",
    image: s.image ?? "",
    dockerfilePath: s.dockerfilePath ?? "",
    startCommand: s.startCommand ?? "",
    port: s.port,
    healthcheckPath: s.healthcheckPath ?? "",
    publicPort: 0,
    cpuLimit: s.cpuLimit ?? 0,
    memoryLimit: s.memoryLimit ?? 0,
    autoDeploy: s.autoDeploy ?? false,
    waitForCi: s.waitForCi ?? false,
    status: s.status,
    privateHost: s.name,
    domains: (s.domains ?? []).map((d, i) => ({
      id: `dom_${hex(`${s.id}:dom:${i}`, 8)}`,
      host: d.host,
      generated: d.generated ?? false,
      url: `https://${d.host}`,
    })),
    volumes: (s.volumes ?? []).map((mountPath, i) => ({
      id: `vol_${hex(`${s.id}:vol:${i}`, 8)}`,
      mountPath,
      createdAt: iso(created + MIN),
    })),
    latestDeployment: deps[0] ?? null,
    restoreFence: null,
    createdAt: iso(created),
  };
}

const defaultPolicy = (enabled: boolean, schedule: string): BackupPolicy => ({
  enabled,
  schedule,
  compression: "default",
  keepLocal: 7,
  upload: true,
  keepRemote: 30,
  nextRunAt: enabled ? nextCron(schedule) : null,
});

/** nextCron approximates the next run of a daily "m h * * *" schedule. */
export function nextCron(schedule: string): string {
  const [m, h] = schedule.replace(/^CRON_TZ=\S+\s+/, "").split(/\s+/);
  const d = new Date();
  d.setUTCHours(Number(h) || 0, Number(m) || 0, 0, 0);
  if (d.getTime() <= Date.now()) d.setUTCDate(d.getUTCDate() + 1);
  return d.toISOString();
}

export function seed(): State {
  const services = new Map<string, Service>();
  const deployments = new Map<string, Deployment[]>();
  const variables = new Map<string, Variables>();
  const stuck = new Set<string>();
  const servicePolicies = new Map<string, BackupPolicy>();
  const backups: Backup[] = [];

  for (const p of projectSeeds) {
    for (const s of p.services) {
      let deps: Deployment[];
      if (s.kind === "app") {
        // Two apps have enough history to page through.
        const count = LONG_HISTORY.has(s.id) ? 35 : 8 + Math.floor(rand(s.id, "count") * 8);
        deps = genDeployments(s, headFor(s), count);
        if (s.status === "deploying") {
          const building = deps[0] as Deployment;
          building.status = "building";
          building.image = "";
          building.startedAt = building.createdAt;
          building.finishedAt = null;
          building.error = "";
          stuck.add(building.id);
        }
      } else {
        const created = BOOT - s.ageDays * DAY;
        deps = genDeployments(s, ["active"], 1).map((d) => ({
          ...d,
          trigger: "create" as const,
          createdAt: iso(created),
          startedAt: iso(created + 1500),
          finishedAt: iso(created + 9000),
        }));
        (deps[0] as Deployment).image = s.image ?? "";
      }
      deployments.set(s.id, deps);
      services.set(s.id, toService(p, s, deps));
      variables.set(s.id, { ...s.variables });
      if ((s.volumes ?? []).length > 0) {
        servicePolicies.set(s.id, defaultPolicy(true, s.kind === "app" ? "30 4 * * *" : "0 3 * * *"));
        backups.push(...genBackups(s));
      }
    }
  }

  const audit = genAudit();
  const grants: OAuthGrant[] = [
    {
      id: "gnt_c1a7e90",
      clientName: "Claude Code",
      redirectHost: "localhost",
      scopes: ["read"],
      createdAt: iso(BOOT - 9 * DAY),
      lastUsedAt: iso(BOOT - 35 * MIN),
    },
    {
      id: "gnt_d4b2f31",
      clientName: "Cursor",
      redirectHost: "127.0.0.1",
      scopes: ["read"],
      createdAt: iso(BOOT - 21 * DAY),
      lastUsedAt: iso(BOOT - 4 * DAY),
    },
  ];

  const serviceCaps = new Map<string, TelemetryCaps>();
  const logHistory = new Map<string, LogRecord[]>();
  for (const svc of services.values()) {
    logHistory.set(svc.id, genLogHistory(svc, deployments.get(svc.id) ?? []));
  }

  return {
    projects: projectSeeds.map((p) => ({
      id: p.id,
      name: p.name,
      createdAt: iso(BOOT - p.ageDays * DAY),
    })),
    services,
    deployments,
    variables,
    stuck,
    backups: [...backups, ...genSystemBackups()].sort((a, b) => b.createdAt.localeCompare(a.createdAt)),
    servicePolicies,
    systemPolicy: defaultPolicy(true, "15 3 * * *"),
    backupSettings: {
      s3: {
        endpoint: "https://s3.us-east-1.amazonaws.com",
        region: "us-east-1",
        bucket: "kyle-shed-backups",
        prefix: "shed/",
        accessKeyId: "AKIAIOSFODNN7EXAMPLE",
        pathStyle: false,
        hasSecret: true,
      },
      encryption: {
        enabled: true,
        recipient: "age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p",
      },
    },
    restores: new Map(),
    audit,
    auditRetentionDays: 90,
    channels: [
      {
        id: "chn_5a1f0c2",
        name: "Kyle's phone",
        provider: "hark",
        enabled: true,
        createdAt: iso(BOOT - 40 * DAY),
        target: "https://hark.ryan.ceo/hooks/" + hex("hark1", 24),
      },
      {
        id: "chn_8e2b7d4",
        name: "Weekend on-call",
        provider: "hark",
        enabled: false,
        createdAt: iso(BOOT - 12 * DAY),
        target: "https://hark.ryan.ceo/hooks/" + hex("hark2", 24),
      },
    ],
    rules: genRules(),
    deliveries: genDeliveries(),
    telemetry: genTelemetry(),
    serviceCaps,
    logHistory,
    update: {
      current: "v0.14.2",
      latest: {
        version: "v0.15.0",
        url: "https://github.com/kyledickey/shed/releases/tag/v0.15.0",
        notes: releaseNotes,
        publishedAt: iso(BOOT - 2 * DAY - 3 * HOUR),
      },
      available: true,
      checkedAt: iso(BOOT - 25 * MIN),
      state: "idle",
      staged: "",
      error: "",
      autoDownload: false,
      channel: "stable",
      unsupported: "",
    },
    grants,
    onePassword: { connected: true, vault: "shed", items: 9 },
  };
}

const releaseNotes = `## v0.15.0

### Highlights

- **Notifications.** Send deploy failures, crash loops, and host alerts to your phone through Hark. Rules and channels live under *Server > Notifications*.
- **Log history.** With telemetry on, the service *Logs* tab gains a *History* mode with search, level filters, and ranges up to 7 days.
- **Backups to S3.** Per-service and system backups can be uploaded to any S3-compatible bucket, encrypted with age.

### Improvements

- Deployments wait for GitHub CI checks when *Wait for CI* is on, and show the check that blocked them.
- The host page shows conntrack flows and per-interface network rates.
- \`shed update\` verifies the release checksum and signature before staging a binary.

### Fixes

- A restore that failed halfway now keeps its fence so the service cannot start with partial data.
- Generated domains no longer collide when two services share a name across projects.
- Build logs no longer drop the last BuildKit step when a build is canceled.
`;

// ------------------------------------------------------------------- backups

function genBackups(s: ServiceSeed): Backup[] {
  const out: Backup[] = [];
  const isDb = s.kind !== "app";
  const method = isDb ? "dump" : "volume";
  const ext = s.kind === "postgres" || s.kind === "mysql" ? "sql.zst" : s.kind === "redis" ? "rdb.zst" : s.kind === "mongo" ? "archive.zst" : "tar.zst";
  const hour = s.kind === "app" ? 4 : 3;
  const baseSize =
    s.kind === "postgres" ? 18 * MiB : s.kind === "mysql" ? 9 * MiB : s.kind === "mongo" ? 64 * MiB : s.kind === "redis" ? 2 * MiB : 140 * MiB;
  const count = LONG_HISTORY.has(s.id) ? 25 : 8;
  for (let i = 0; i < count; i++) {
    const d = new Date(BOOT);
    d.setUTCDate(d.getUTCDate() - i - (d.getUTCHours() < hour ? 1 : 0));
    d.setUTCHours(hour, s.kind === "app" ? 30 : 0, 0, 0);
    const created = d.getTime();
    const stamp = d.toISOString().replace(/[-:]/g, "").replace("T", "-").slice(0, 15);
    const seed = `${s.id}:bk:${i}`;
    const failed = i === 3 && s.kind === "postgres" && s.id === "svc_pg3k8vb";
    const remoteFailed = i === 1 && s.id === "svc_pg9d3wf";
    const manual = i === 2;
    out.push({
      id: `bak_${hex(seed, 9)}`,
      serviceId: s.id,
      trigger: manual ? "manual" : "schedule",
      method,
      status: failed ? "failed" : "succeeded",
      fileName: `${s.name}-${stamp.slice(0, 8)}-${stamp.slice(9)}.${ext}`,
      size: failed ? 0 : Math.round(baseSize * (1 + (count - i) * 0.012 + rand(seed, "sz") * 0.03)),
      encrypted: true,
      local: i < 5 && !failed,
      remote: !failed && !remoteFailed,
      remoteError: remoteFailed ? "s3: PutObject: RequestTimeTooSkewed: request time too skewed" : "",
      error: failed ? "pg_dump: error: connection to server failed: Connection refused" : "",
      createdAt: iso(created),
      finishedAt: iso(created + 20_000 + Math.floor(rand(seed, "fin") * 60_000)),
    });
  }
  return out;
}

export function genSystemBackups(): Backup[] {
  const out: Backup[] = [];
  for (let i = 0; i < 25; i++) {
    const d = new Date(BOOT);
    d.setUTCDate(d.getUTCDate() - i - (d.getUTCHours() < 3 ? 1 : 0));
    d.setUTCHours(3, 15, 0, 0);
    const stamp = d.toISOString().replace(/[-:]/g, "").replace("T", "-").slice(0, 15);
    const seed = `sys:bk:${i}`;
    out.push({
      id: `bak_${hex(seed, 9)}`,
      serviceId: null,
      trigger: "schedule",
      method: "sqlite",
      status: "succeeded",
      fileName: `shed-${stamp.slice(0, 8)}-${stamp.slice(9)}.db.zst`,
      size: Math.round(3.4 * MiB + i * 0.02 * MiB),
      encrypted: true,
      local: i < 7,
      remote: true,
      remoteError: "",
      error: "",
      createdAt: iso(d.getTime()),
      finishedAt: iso(d.getTime() + 4_000),
    });
  }
  return out;
}

// --------------------------------------------------------------------- audit

type AuditTemplate = {
  actorKind: AuditActorKind;
  actor: string;
  action: string;
  outcome?: AuditOutcome;
  w: number;
  scoped: boolean;
  detail?: (svc: string, proj: string) => Record<string, unknown>;
};

const auditTemplates: AuditTemplate[] = [
  { actorKind: "github", actor: "", action: "webhook.push", w: 14, scoped: true, detail: () => ({ ref: "refs/heads/main", commits: 1 }) },
  { actorKind: "system", actor: "", action: "deployment.succeeded", w: 12, scoped: true },
  { actorKind: "system", actor: "", action: "deployment.failed", outcome: "failed", w: 3, scoped: true },
  { actorKind: "user", actor: "kyledickey", action: "deployment.create", w: 6, scoped: true, detail: () => ({ trigger: "manual" }) },
  { actorKind: "user", actor: "kyledickey", action: "deployment.redeploy", w: 2, scoped: true },
  { actorKind: "user", actor: "kyledickey", action: "deployment.cancel", w: 1, scoped: true },
  { actorKind: "user", actor: "kyledickey", action: "service.variables.update", w: 6, scoped: true, detail: () => ({ keys: ["DATABASE_URL", "LOG_LEVEL"] }) },
  { actorKind: "user", actor: "kyledickey", action: "service.update", w: 4, scoped: true, detail: () => ({ fields: ["memoryLimit", "healthcheckPath"] }) },
  { actorKind: "user", actor: "kyledickey", action: "service.restart", w: 3, scoped: true },
  { actorKind: "user", actor: "kyledickey", action: "service.stop", w: 1, scoped: true },
  { actorKind: "user", actor: "kyledickey", action: "service.start", w: 1, scoped: true },
  { actorKind: "user", actor: "kyledickey", action: "service.domain.create", w: 1, scoped: true, detail: () => ({ host: "api.aretheyup.com" }) },
  { actorKind: "user", actor: "kyledickey", action: "project.rename", w: 1, scoped: false, detail: () => ({ from: "aretheyup-dev", to: "aretheyup" }) },
  { actorKind: "user", actor: "kyledickey", action: "backup.run", w: 3, scoped: true },
  { actorKind: "system", actor: "", action: "backup.succeeded", w: 10, scoped: true },
  { actorKind: "system", actor: "", action: "backup.failed", outcome: "failed", w: 1, scoped: true },
  { actorKind: "user", actor: "kyledickey", action: "backup.restore", w: 1, scoped: true },
  { actorKind: "agent", actor: "kyledickey", action: "mcp.call", w: 9, scoped: true, detail: () => ({ tool: "service_logs", limit: 200 }) },
  { actorKind: "agent", actor: "kyledickey", action: "mcp.call", w: 4, scoped: false, detail: () => ({ tool: "list_projects" }) },
  { actorKind: "docker", actor: "", action: "container.crash", outcome: "failed", w: 3, scoped: true, detail: () => ({ exitCode: 137, oom: true }) },
  { actorKind: "user", actor: "kyledickey", action: "auth.login", w: 4, scoped: false },
  { actorKind: "anonymous", actor: "", action: "auth.login", outcome: "denied", w: 1, scoped: false, detail: () => ({ reason: "not an allowed user" }) },
  { actorKind: "user", actor: "kyledickey", action: "notification.rule.update", w: 1, scoped: false, detail: () => ({ rule: "shed-host-disk", fields: ["threshold"] }) },
  { actorKind: "user", actor: "kyledickey", action: "telemetry.update", w: 1, scoped: false, detail: () => ({ fields: ["logsRetentionDays"] }) },
  { actorKind: "user", actor: "kyledickey", action: "oauth.grant", w: 1, scoped: false, detail: () => ({ client: "Cursor" }) },
  { actorKind: "user", actor: "kyledickey", action: "update.check", w: 2, scoped: false },
];

function genAudit(): AuditEvent[] {
  const total = auditTemplates.reduce((n, t) => n + t.w, 0);
  const svcs = [...projectSeeds.flatMap((p) => p.services.map((s) => ({ s, p })))];
  const out: AuditEvent[] = [];
  const count = 130;
  for (let i = 0; i < count; i++) {
    let r = rand("audit", i, "t") * total;
    let t = auditTemplates[0] as AuditTemplate;
    for (const cand of auditTemplates) {
      r -= cand.w;
      if (r <= 0) {
        t = cand;
        break;
      }
    }
    const target = pick(svcs.filter((x) => x.s.kind === "app" || t.action.startsWith("backup") || t.action.startsWith("mcp")), "audit", i, "svc");
    const ts = BOOT - Math.pow(i / count, 1.6) * 14 * DAY - 3 * MIN - Math.floor(rand("audit", i, "jit") * 20 * MIN);
    out.push({
      id: 0,
      ts: iso(ts),
      actorKind: t.actorKind,
      actor: t.actorKind === "github" ? "kyledickey" : t.actor,
      action: t.action,
      outcome: t.outcome ?? "ok",
      projectId: t.scoped ? target.p.id : "",
      serviceId: t.scoped ? target.s.id : "",
      target: t.scoped ? target.s.id : "",
      detail: t.detail ? t.detail(target.s.name, target.p.name) : {},
    });
  }
  out.sort((a, b) => b.ts.localeCompare(a.ts));
  out.forEach((e, i) => (e.id = count - i));
  return out;
}

// ------------------------------------------------------------ notifications

function genRules(): NotificationRule[] {
  const ev = (id: string, title: string, description: string, extra: Partial<NotificationRule> = {}): NotificationRule => ({
    id,
    title,
    description,
    enabled: true,
    threshold: null,
    unit: "",
    forSeconds: null,
    needsTelemetry: false,
    available: true,
    ...extra,
  });
  const metric = (id: string, title: string, description: string, threshold: number | null, unit: string, forSeconds: number | null, enabled = true): NotificationRule =>
    ev(id, title, description, { enabled, threshold, unit, forSeconds, needsTelemetry: true });
  return [
    ev("deploy-failed", "Deployment failed", "A build or deploy failed."),
    ev("backup-failed", "Backup failed", "A scheduled or manual backup failed."),
    ev("restore-failed", "Restore failed", "A restore failed and left its service fenced."),
    ev("service-oom", "Service out of memory", "A container was killed for exceeding its memory limit."),
    ev("service-crash-loop", "Service crash loop", "A service crashed repeatedly within 10 minutes.", { threshold: 3, unit: "crashes" }),
    metric("shed-host-disk", "Host disk almost full", "Host disk use is above the threshold.", 85, "%", null),
    metric("shed-host-memory", "Host memory high", "Host memory use is above the threshold.", 90, "%", 600),
    metric("shed-http-errors", "HTTP 5xx rate", "A service's 5xx ratio is above the threshold.", 5, "%", 300),
    metric("shed-http-latency", "HTTP latency", "A service's p95 latency is above the threshold.", 2, "s", 600, false),
    metric("shed-db-connections", "Database connections", "A database's connections are close to its maximum.", 80, "%", 300),
    metric("shed-telemetry-drops", "Telemetry dropped", "shed is dropping a service's telemetry.", null, "", 300),
  ];
}

function genDeliveries(): NotificationDelivery[] {
  const items: { title: string; body: string; status: "sent" | "failed" | "pending"; ch: number; ago: number }[] = [
    { title: "Deployment failed: aretheyup/api", body: "Deployment dep_3f9a1c2d7 of api failed: deploy: health check GET /healthz did not return 2xx within 60s", status: "sent", ch: 0, ago: 6 * HOUR },
    { title: "Service crash loop: aretheyup/worker", body: "worker crashed 3 times in 10 minutes (last exit: 137, out of memory).", status: "sent", ch: 0, ago: 52 * MIN },
    { title: "Service out of memory: aretheyup/worker", body: "worker was killed for exceeding its 384 MiB memory limit.", status: "sent", ch: 0, ago: 58 * MIN },
    { title: "Backup failed: aretheyup/postgres", body: "Backup bak_a81c4e03f failed: pg_dump: connection to server failed: Connection refused", status: "failed", ch: 0, ago: 4 * DAY },
    { title: "Host memory high", body: "Host memory use is 91% (7.3 GiB of 8 GiB) for 10 minutes.", status: "sent", ch: 0, ago: 3 * DAY + 2 * HOUR },
    { title: "Deployment failed: dismegle/dismegle", body: "Deployment dep_72be90a31 of dismegle failed: build: process \"/bin/sh -c npm run build\" did not complete successfully: exit code: 1", status: "failed", ch: 0, ago: 5 * DAY },
    { title: "Host disk almost full", body: "Host disk use is 86% (68.9 GB of 80 GB).", status: "sent", ch: 0, ago: 6 * DAY },
    { title: "HTTP 5xx rate: kyle.so/web", body: "web returned 5xx for 7.2% of requests over the last 5 minutes.", status: "sent", ch: 0, ago: 7 * DAY },
    { title: "Deployment failed: kyle.so/web", body: "Deployment dep_c0de5aa17 of web failed: waiting for CI: check run \"test\" failed", status: "sent", ch: 0, ago: 8 * DAY },
    { title: "Service crash loop: dismegle/dismegle", body: "dismegle crashed 3 times in 10 minutes (last exit: 1).", status: "sent", ch: 0, ago: 9 * DAY },
    { title: "Backup failed: kyle.so/postgres", body: "Backup bak_66d1e2b40 was written but its upload failed: s3: RequestTimeTooSkewed", status: "failed", ch: 0, ago: 10 * DAY },
    { title: "Deployment failed: aretheyup/worker", body: "Deployment dep_91ab33c0e of worker failed: build: railpack: no start command could be found", status: "sent", ch: 0, ago: 11 * DAY },
    { title: "Database connections: aretheyup/postgres", body: "postgres is using 86% of its connection limit.", status: "sent", ch: 0, ago: 12 * DAY },
    { title: "Test notification", body: "This is a test message from shed.", status: "sent", ch: 1, ago: 12 * DAY },
    { title: "Telemetry dropped: aretheyup/api", body: "shed dropped log records from api (rate cap of 500/s).", status: "pending", ch: 0, ago: 2 * MIN },
  ];
  const channelIds = ["chn_5a1f0c2", "chn_8e2b7d4"];
  const channelNames = ["Kyle's phone", "Weekend on-call"];
  return items
    .map((it, i) => {
      const created = BOOT - it.ago;
      return {
        id: `dlv_${hex(`dlv${i}`, 8)}`,
        channelId: channelIds[it.ch] as string,
        channelName: channelNames[it.ch] as string,
        title: it.title,
        body: it.body,
        status: it.status,
        attempts: it.status === "failed" ? 5 : it.status === "pending" ? 1 : 0,
        lastError:
          it.status === "pending"
            ? "hark: POST /hooks: 502 Bad Gateway"
            : it.status === "failed"
              ? "hark: POST /hooks: 502 Bad Gateway"
              : "",
        createdAt: iso(created),
        sentAt: it.status === "sent" ? iso(created + 1200) : null,
      } satisfies NotificationDelivery;
    })
    .sort((a, b) => b.createdAt.localeCompare(a.createdAt));
}

// ----------------------------------------------------------------- telemetry

function genTelemetry(): Telemetry {
  const comp = (
    name: "metrics" | "logs" | "traces" | "grafana",
    version: string,
    cpu: number | null,
    mem: number | null,
    limit: number,
    disk: number | null,
  ) => ({
    name,
    state: "running" as const,
    version,
    error: "",
    cpuPercent: cpu,
    memoryBytes: mem,
    memoryLimit: limit,
    diskBytes: disk,
  });
  return {
    settings: {
      enabled: true,
      metricsRetentionDays: 30,
      logsRetentionDays: 14,
      tracesRetentionDays: 7,
      diskLimitGB: 20,
      defaultCaps: { spansPerSecond: 200, logsPerSecond: 500, series: 5000 },
    },
    components: [
      comp("metrics", "v1.122.0", 3.4, 212 * MiB, 512 * MiB, Math.round(1.1 * GiB)),
      comp("logs", "v1.29.0", 2.1, 168 * MiB, 512 * MiB, Math.round(2.6 * GiB)),
      comp("traces", "v0.8.0", 1.2, 124 * MiB, 512 * MiB, Math.round(0.7 * GiB)),
      comp("grafana", "13.0.1", 1.8, 241 * MiB, 512 * MiB, null),
    ],
    ingest: { spans: 42.5, logs: 118.2, points: 860 },
    droppedLastHour: [{ reason: "rate_limit", signal: "logs", count: 1840 }],
    firingAlerts: [{ name: "shed-telemetry-drops", serviceId: "svc_api7mz3", since: iso(BOOT - 14 * MIN) }],
    grafanaUrl: "http://localhost:5173/grafana/",
    otlpEndpoint: "http://localhost:4318",
  };
}

// ------------------------------------------------------------------- GitHub

export const repos: Repo[] = [
  { fullName: "kyledickey/kyle.so", defaultBranch: "main", private: false },
  { fullName: "kyledickey/aretheyup", defaultBranch: "main", private: true },
  { fullName: "kyledickey/dismegle", defaultBranch: "main", private: true },
  { fullName: "kyledickey/alias-party", defaultBranch: "main", private: true },
  { fullName: "kyledickey/shed", defaultBranch: "main", private: false },
  { fullName: "kyledickey/dotfiles", defaultBranch: "main", private: false },
  { fullName: "kyledickey/hark", defaultBranch: "main", private: true },
  { fullName: "kyledickey/recipes", defaultBranch: "master", private: false },
  { fullName: "mayaokafor/trailmap", defaultBranch: "main", private: true },
  { fullName: "kyledickey/pixel-garden", defaultBranch: "main", private: false },
];

export function branchesFor(repo: string): string[] {
  const def = repos.find((r) => r.fullName === repo)?.defaultBranch ?? "main";
  const extra = ["develop", "feat/new-landing", "fix/flaky-tests", "renovate/major-deps"];
  const n = 1 + Math.floor(rand(repo, "branches") * extra.length);
  return [def, ...extra.slice(0, n)];
}

// ------------------------------------------------------------- log history

const histMessages = {
  info: [
    "GET /api/monitors 200 14ms",
    "GET /healthz 200 1ms",
    "POST /api/probes 202 31ms",
    "request completed",
    "cache hit",
    "job completed",
    "connection pool: 8 idle, 2 active",
    "listening on :8080",
    "checkpoint complete: wrote 118 buffers",
    "background save terminated with success",
  ],
  warn: [
    "slow request GET /api/monitors/9f2 1.8s",
    "retrying probe after ECONNRESET (attempt 2/3)",
    "connection pool near capacity (46/50)",
    "deprecated option ignored: legacyAuth",
    "upstream responded 429, backing off 2s",
  ],
  error: [
    "unhandled error: TypeError: Cannot read properties of null (reading 'body')",
    "probe timed out after 8000ms: https://status.example.net",
    "db query failed: deadlock detected",
    "failed to send webhook: 503 Service Unavailable",
  ],
  debug: ["scheduler tick: 214 monitors due", "resolved example.com in 12ms", "tls handshake took 38ms"],
};

function genLogHistory(svc: Service, deps: Deployment[]): LogRecord[] {
  const out: LogRecord[] = [];
  const total = svc.status === "stopped" ? 60 : 520;
  const active = deps.find((d) => d.status === "active" || d.status === "crashed");
  for (let i = 0; i < total; i++) {
    const seed = `${svc.id}:hist:${i}`;
    const r = rand(seed, "lvl");
    const level = r < 0.03 ? "debug" : r < 0.82 ? "info" : r < 0.95 ? "warn" : "error";
    const ago = Math.pow(i / total, 2.2) * 7 * DAY + 30_000 + Math.floor(rand(seed, "j") * 40_000);
    const msg = pick(histMessages[level], seed, "m");
    const otlp = svc.kind === "app" && rand(seed, "src") < 0.4;
    const traced = otlp && rand(seed, "tr") < 0.6;
    const dep = deps.find((d) => Date.parse(d.createdAt) <= BOOT - ago) ?? active;
    out.push({
      time: iso(BOOT - ago),
      source: otlp ? "otlp" : "container",
      stream: otlp ? "" : level === "error" ? "stderr" : "stdout",
      level,
      message: msg,
      deploymentId: dep?.id ?? "",
      traceId: traced ? hex(seed + "t", 32) : "",
      fields: otlp
        ? { "http.route": pick(["/api/monitors", "/api/probes", "/healthz"], seed, "route"), service: svc.name }
        : {},
    });
  }
  return out;
}
