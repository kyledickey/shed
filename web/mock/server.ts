// Dev-only mock of shed's JSON API. Run with `bun run mock`, then `bun run dev`.
// State is in memory and resets on restart. Contract: docs/design.md.
import type {
  AuditPage,
  Backup,
  BackupPolicyInput,
  BackupSettingsInput,
  Deployment,
  DeploymentStatus,
  Domain,
  HostMetrics,
  LogPage,
  Metrics,
  MetricsRange,
  NewService,
  NotificationChannelInput,
  NotificationChannelPatch,
  NotificationRulePatch,
  Project,
  ProjectDetail,
  Restore,
  Service,
  ServicePatch,
  ServiceTelemetry,
  TelemetrySettings,
  Variables,
  Volume,
} from "../src/api/types";
import {
  BOOT,
  GiB,
  MIN,
  MiB,
  branchesFor,
  hex,
  iso,
  me,
  newId,
  nextCron,
  pick,
  rand,
  repos,
  seed,
  setup,
} from "./fixtures";

type BunServer = { port: number; hostname: string };
const BunRuntime = (globalThis as unknown as {
  Bun: {
    serve(opts: {
      port: number;
      hostname: string;
      idleTimeout: number;
      fetch(req: Request): Response | Promise<Response>;
    }): BunServer;
  };
}).Bun;

const state = seed();

// ------------------------------------------------------------------- helpers

const JSON_HEADERS = { "Content-Type": "application/json" };
const json = (data: unknown, status = 200, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(data), { status, headers: { ...JSON_HEADERS, ...headers } });
const noContent = () => new Response(null, { status: 204 });
const fail = (status: number, error: string) => json({ error }, status);
const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

type Ctx = { req: Request; url: URL; params: Record<string, string>; body: any };
type Handler = (c: Ctx) => Response | object | Promise<Response | object | undefined> | undefined;
type Route = { method: string; re: RegExp; keys: string[]; handler: Handler };
const routes: Route[] = [];

function route(method: string, path: string, handler: Handler) {
  const keys: string[] = [];
  const re = new RegExp(
    "^" +
      path.replace(/:(\w+)/g, (_, k: string) => {
        keys.push(k);
        return "([^/]+)";
      }) +
      "$",
  );
  routes.push({ method, re, keys, handler });
}
const GET = (p: string, h: Handler) => route("GET", p, h);
const POST = (p: string, h: Handler) => route("POST", p, h);
const PUT = (p: string, h: Handler) => route("PUT", p, h);
const PATCH = (p: string, h: Handler) => route("PATCH", p, h);
const DELETE = (p: string, h: Handler) => route("DELETE", p, h);

function projectSummary(p: { id: string; name: string; createdAt: string }): Project {
  return {
    ...p,
    services: [...state.services.values()]
      .filter((s) => s.projectId === p.id)
      .map((s) => ({ id: s.id, name: s.name, kind: s.kind, status: s.status })),
  };
}

function service(id: string): Service | undefined {
  const s = state.services.get(id);
  if (!s) return undefined;
  s.latestDeployment = state.deployments.get(id)?.[0] ?? null;
  return s;
}

function findDeployment(id: string): Deployment | undefined {
  for (const list of state.deployments.values()) {
    const d = list.find((x) => x.id === id);
    if (d) return d;
  }
  return undefined;
}

const pending = new Set<DeploymentStatus>(["queued", "waiting", "building", "deploying"]);
const isPending = (s: DeploymentStatus) => pending.has(s);

function audit(action: string, s?: Service, detail: Record<string, unknown> = {}, outcome: "ok" | "failed" = "ok") {
  const id = (state.audit[0]?.id ?? 0) + 1;
  state.audit.unshift({
    id,
    ts: iso(Date.now()),
    actorKind: "user",
    actor: me.login,
    action,
    outcome,
    projectId: s?.projectId ?? "",
    serviceId: s?.id ?? "",
    target: s?.id ?? "",
    detail,
  });
}

/** startDeploy queues a deployment that walks through building and deploying to active. */
function startDeploy(
  svc: Service,
  trigger: Deployment["trigger"],
  from?: Deployment,
): Deployment {
  const list = state.deployments.get(svc.id) ?? [];
  const sha = from?.commitSha ?? (svc.repo ? hex(newId("sha"), 40) : "");
  const dep: Deployment = {
    id: `dep_${hex(newId("dep"), 9)}`,
    serviceId: svc.id,
    status: "queued",
    trigger,
    commitSha: sha,
    commitMessage: from?.commitMessage ?? (svc.repo ? "chore: deploy from the dashboard" : ""),
    commitAuthor: from?.commitAuthor ?? (svc.repo ? me.name : ""),
    image: from?.image ?? (svc.repo ? "" : svc.image),
    imageAvailable: false,
    error: "",
    traceId: hex(newId("trace"), 32),
    createdAt: iso(Date.now()),
    startedAt: null,
    finishedAt: null,
  };
  list.unshift(dep);
  state.deployments.set(svc.id, list);
  const prevStatus = svc.status;
  svc.status = "deploying";
  const step = (status: DeploymentStatus, after: number, fn?: () => void) =>
    setTimeout(() => {
      if (dep.status === "canceled") return;
      dep.status = status;
      if (!dep.startedAt) dep.startedAt = iso(Date.now());
      fn?.();
    }, after);
  step("building", 1200);
  step("deploying", from?.image ? 3000 : 9000, () => {
    if (svc.repo && !dep.image) dep.image = `shed/${svc.name}:${sha.slice(0, 7)}`;
  });
  step("active", from?.image ? 6000 : 13000, () => {
    for (const d of list) if (d !== dep && d.status === "active") d.status = "removed";
    dep.finishedAt = iso(Date.now());
    dep.imageAvailable = true;
    svc.status = "active";
    if (prevStatus === "stopped") svc.status = "active";
  });
  return dep;
}

// ----------------------------------------------------------------- auth/setup

GET("/me", () => me);
GET("/setup", () => setup);
POST("/auth/logout", () => noContent());
GET("/auth/login", ({ url }) => new Response(null, { status: 302, headers: { Location: url.searchParams.get("next") || "/" } }));
GET("/auth/callback", () => new Response(null, { status: 302, headers: { Location: "/" } }));
GET("/setup/github", () => new Response(null, { status: 302, headers: { Location: "/" } }));
POST("/setup/github/import", () => setup);

// ------------------------------------------------------------------- projects

GET("/projects", () => state.projects.map(projectSummary));
POST("/projects", ({ body }) => {
  const name = String(body?.name ?? "").trim();
  if (!name) return fail(400, "name is required");
  const p = { id: newId("prj"), name, createdAt: iso(Date.now()) };
  state.projects.push(p);
  audit("project.create", undefined, { name });
  return json(projectSummary(p), 201);
});
GET("/projects/:id", ({ params }) => {
  const p = state.projects.find((x) => x.id === params.id);
  if (!p) return fail(404, "project not found");
  const detail: ProjectDetail = {
    ...p,
    services: [...state.services.values()].filter((s) => s.projectId === p.id).map((s) => service(s.id) as Service),
  };
  return detail;
});
PATCH("/projects/:id", ({ params, body }) => {
  const p = state.projects.find((x) => x.id === params.id);
  if (!p) return fail(404, "project not found");
  const name = String(body?.name ?? "").trim();
  if (!name) return fail(400, "name is required");
  audit("project.rename", undefined, { from: p.name, to: name });
  p.name = name;
  return projectSummary(p);
});
DELETE("/projects/:id", ({ params }) => {
  const i = state.projects.findIndex((x) => x.id === params.id);
  if (i < 0) return fail(404, "project not found");
  for (const s of [...state.services.values()]) if (s.projectId === params.id) removeService(s.id);
  state.projects.splice(i, 1);
  return noContent();
});

function removeService(id: string) {
  state.services.delete(id);
  state.deployments.delete(id);
  state.variables.delete(id);
  state.servicePolicies.delete(id);
  state.backups = state.backups.filter((b) => b.serviceId !== id);
}

// ------------------------------------------------------------------- services

POST("/projects/:id/services", ({ params, body }) => {
  const p = state.projects.find((x) => x.id === params.id);
  if (!p) return fail(404, "project not found");
  const n = body as NewService;
  if (!n?.name?.trim()) return fail(400, "name is required");
  if ([...state.services.values()].some((s) => s.projectId === p.id && s.name === n.name)) {
    return fail(409, `a service named "${n.name}" already exists in this project`);
  }
  const isDb = n.kind !== "app";
  const images: Record<string, [string, number]> = {
    postgres: ["postgres:17", 5432],
    mysql: ["mysql:8.4", 3306],
    mongo: ["mongo:8", 27017],
    redis: ["redis:7-alpine", 6379],
  };
  const [dbImage, dbPort] = images[n.kind] ?? ["", 0];
  const s: Service = {
    id: newId("svc"),
    projectId: p.id,
    name: n.name,
    kind: n.kind,
    repo: n.repo ?? "",
    branch: n.branch ?? (n.repo ? "main" : ""),
    rootDir: "",
    image: isDb ? dbImage : (n.image ?? ""),
    dockerfilePath: "",
    startCommand: "",
    port: isDb ? dbPort : 3000,
    healthcheckPath: "",
    publicPort: 0,
    cpuLimit: 0,
    memoryLimit: 0,
    autoDeploy: !isDb && !!n.repo,
    waitForCi: false,
    status: "offline",
    privateHost: n.name,
    domains: [],
    volumes: isDb
      ? [{ id: newId("vol"), mountPath: "/var/lib/data", createdAt: iso(Date.now()) } satisfies Volume]
      : [],
    latestDeployment: null,
    restoreFence: null,
    createdAt: iso(Date.now()),
  };
  state.services.set(s.id, s);
  state.deployments.set(s.id, []);
  state.variables.set(s.id, isDb ? { PASSWORD: hex(s.id, 24) } : {});
  startDeploy(s, "create");
  audit("service.create", s, { name: s.name, kind: s.kind });
  return json(service(s.id), 201);
});

GET("/services/:id", ({ params }) => service(params.id as string) ?? fail(404, "service not found"));

PATCH("/services/:id", ({ params, body }) => {
  const s = service(params.id as string);
  if (!s) return fail(404, "service not found");
  const patch = body as ServicePatch;
  Object.assign(s, patch);
  audit("service.update", s, { fields: Object.keys(patch) });
  return s;
});

DELETE("/services/:id", ({ params }) => {
  const s = state.services.get(params.id as string);
  if (!s) return fail(404, "service not found");
  audit("service.delete", s, { name: s.name });
  removeService(s.id);
  return noContent();
});

function control(action: "stop" | "start" | "restart") {
  return ({ params }: Ctx) => {
    const s = service(params.id as string);
    if (!s) return fail(404, "service not found");
    if (s.restoreFence) return fail(409, "service is fenced by a restore");
    const deps = state.deployments.get(s.id) ?? [];
    if (action === "stop") {
      for (const d of deps) if (isPending(d.status)) d.status = "canceled";
      state.stuck.clear();
      s.status = "stopped";
    } else if (action === "start") {
      if (!deps.length) return fail(409, "service has never been deployed");
      s.status = "active";
    } else {
      if (s.status === "stopped") return fail(409, "service is stopped");
      s.status = "active";
    }
    audit(`service.${action}`, s);
    return s;
  };
}
POST("/services/:id/stop", control("stop"));
POST("/services/:id/start", control("start"));
POST("/services/:id/restart", control("restart"));
POST("/services/:id/restore-fence/clear", ({ params }) => {
  const s = service(params.id as string);
  if (!s) return fail(404, "service not found");
  s.restoreFence = null;
  return s;
});

// ------------------------------------------------------------------ variables

const INJECTED = (s: Service): Variables => {
  const p = state.projects.find((x) => x.id === s.projectId);
  const dep = state.deployments.get(s.id)?.find((d) => d.status === "active");
  return {
    ...(s.kind === "app" && s.port > 0 ? { PORT: String(s.port) } : {}),
    SHED_PROJECT_NAME: p?.name ?? "",
    SHED_SERVICE_NAME: s.name,
    SHED_PRIVATE_DOMAIN: s.name,
    ...(s.domains[0] ? { SHED_PUBLIC_DOMAIN: s.domains[0].host } : {}),
    ...(dep?.commitSha ? { SHED_GIT_COMMIT_SHA: dep.commitSha } : {}),
    ...(s.branch ? { SHED_GIT_BRANCH: s.branch } : {}),
  };
};

function resolveVars(s: Service, depth = 0): Variables {
  const own = { ...INJECTED(s), ...(state.variables.get(s.id) ?? {}) };
  const out: Variables = {};
  const expand = (v: string): string =>
    v.replace(/\$\{\{\s*([\w.-]+?)\s*\}\}/g, (m, ref: string) => {
      if (depth > 4) return m;
      const dot = ref.indexOf(".");
      if (dot < 0) return own[ref] !== undefined ? expand(own[ref] as string) : m;
      const other = [...state.services.values()].find((x) => x.projectId === s.projectId && x.name === ref.slice(0, dot));
      const val = other ? resolveVars(other, depth + 1)[ref.slice(dot + 1)] : undefined;
      return val ?? m;
    });
  for (const [k, v] of Object.entries(own)) out[k] = expand(v);
  return out;
}

GET("/services/:id/variables", ({ params }) => {
  const v = state.variables.get(params.id as string);
  return v ?? fail(404, "service not found");
});
PUT("/services/:id/variables", ({ params, body }) => {
  const s = service(params.id as string);
  if (!s) return fail(404, "service not found");
  const vars = body as Variables;
  audit("service.variables.update", s, { keys: Object.keys(vars) });
  state.variables.set(s.id, { ...vars });
  return vars;
});
GET("/services/:id/variables/resolved", ({ params }) => {
  const s = service(params.id as string);
  return s ? resolveVars(s) : fail(404, "service not found");
});

// ---------------------------------------------------------------- 1Password

GET("/onepassword", () => state.onePassword);
POST("/onepassword/vaults", ({ body }) => {
  if (!body?.token) return fail(400, "token is required");
  return [
    { id: "vlt_7q2xk4n5m", name: "shed" },
    { id: "vlt_b9d3p1c8w", name: "Personal" },
  ];
});
PUT("/onepassword", ({ body }) => {
  if (state.onePassword.connected) return fail(409, "1Password is already connected");
  state.onePassword = { connected: true, vault: body?.vault === "vlt_b9d3p1c8w" ? "Personal" : "shed", items: state.services.size };
  return state.onePassword;
});
DELETE("/onepassword", () => {
  state.onePassword = { connected: false, vault: "", items: 0 };
  return state.onePassword;
});

// ------------------------------------------------------- domains and volumes

POST("/services/:id/domains", ({ params, body }) => {
  const s = service(params.id as string);
  if (!s) return fail(404, "service not found");
  const host: string =
    body?.host?.trim() || `${s.name}-production-${hex(newId("h"), 4)}.apps.kyle.so`;
  const d: Domain = { id: newId("dom"), host, generated: !body?.host, url: `https://${host}` };
  s.domains.push(d);
  audit("service.domain.create", s, { host });
  return json(d, 201);
});
DELETE("/domains/:id", ({ params }) => {
  for (const s of state.services.values()) {
    const i = s.domains.findIndex((d) => d.id === params.id);
    if (i >= 0) {
      audit("service.domain.delete", s, { host: s.domains[i]?.host });
      s.domains.splice(i, 1);
      return noContent();
    }
  }
  return fail(404, "domain not found");
});
POST("/services/:id/volumes", ({ params, body }) => {
  const s = service(params.id as string);
  if (!s) return fail(404, "service not found");
  const v: Volume = { id: newId("vol"), mountPath: String(body?.mountPath ?? "/data"), createdAt: iso(Date.now()) };
  s.volumes.push(v);
  audit("service.volume.create", s, { mountPath: v.mountPath });
  return json(v, 201);
});
DELETE("/volumes/:id", ({ params }) => {
  for (const s of state.services.values()) {
    const i = s.volumes.findIndex((v) => v.id === params.id);
    if (i >= 0) {
      audit("service.volume.delete", s, { mountPath: s.volumes[i]?.mountPath });
      s.volumes.splice(i, 1);
      return noContent();
    }
  }
  return fail(404, "volume not found");
});

// ---------------------------------------------------------------- deployments

/**
 * page cuts a newest-first list the way shed pages deployments and backups:
 * limit (default 10, at most 100) records after the one whose id is before.
 * An unknown before is an empty page.
 */
function page<T extends { id: string }>(list: T[], url: URL): { items: T[]; next: string | null } | Response {
  const q = url.searchParams;
  const raw = q.get("limit");
  const n = raw === null || raw === "" ? 10 : Number(raw);
  if (!Number.isInteger(n) || n < 1) return fail(400, "limit must be a positive integer");
  const limit = Math.min(n, 100);
  const before = q.get("before");
  const start = before ? list.findIndex((x) => x.id === before) + 1 : 0;
  if (before && start === 0) return { items: [], next: null };
  const items = list.slice(start, start + limit);
  const more = list.length > start + limit;
  return { items, next: more ? (items[items.length - 1]?.id ?? null) : null };
}

GET("/services/:id/deployments", ({ params, url }) => {
  const list = state.deployments.get(params.id as string);
  if (!list) return fail(404, "service not found");
  const p = page(list, url);
  return p instanceof Response ? p : { deployments: p.items, next: p.next };
});
POST("/services/:id/deployments", ({ params }) => {
  const s = service(params.id as string);
  if (!s) return fail(404, "service not found");
  if (s.restoreFence) return fail(409, "service is fenced by a restore");
  audit("deployment.create", s, { trigger: "manual" });
  return json(startDeploy(s, "manual"), 201);
});
GET("/deployments/:id", ({ params }) => findDeployment(params.id as string) ?? fail(404, "deployment not found"));
POST("/deployments/:id/redeploy", ({ params }) => {
  const d = findDeployment(params.id as string);
  const s = d && service(d.serviceId);
  if (!d || !s) return fail(404, "deployment not found");
  if (s.restoreFence) return fail(409, "service is fenced by a restore");
  audit("deployment.redeploy", s, { from: d.id });
  return json(startDeploy(s, "redeploy", d), 201);
});
POST("/deployments/:id/cancel", ({ params }) => {
  const d = findDeployment(params.id as string);
  if (!d) return fail(404, "deployment not found");
  if (isPending(d.status)) {
    d.status = "canceled";
    d.error = "canceled by kyledickey";
    d.finishedAt = iso(Date.now());
    state.stuck.delete(d.id);
    const s = state.services.get(d.serviceId);
    if (s && s.status === "deploying") {
      s.status = state.deployments.get(s.id)?.some((x) => x.status === "active") ? "active" : "offline";
    }
    audit("deployment.cancel", s, { deployment: d.id });
  }
  return d;
});

// -------------------------------------------------------------------- metrics

/** CRASH_MIN is how long before server start the crashed worker died. */
const CRASH_MIN = 20;
const RANGE_SECONDS: Record<MetricsRange, number> = { "1h": 3600, "6h": 21600, "24h": 86400, "7d": 604800 };
const BUCKETS = 180;

function parseRange(url: URL): MetricsRange {
  const r = url.searchParams.get("range");
  return r === "6h" || r === "24h" || r === "7d" ? r : "1h";
}

/** spike returns 0..1: how close t is to a pseudo-random burst of activity. */
function spike(seedKey: string, t: number, step: number, density = 0.16): number {
  const slot = 1800;
  const w = Math.max(240, step * 1.2);
  const k0 = Math.floor(t / slot);
  let best = 0;
  for (const k of [k0 - 1, k0, k0 + 1]) {
    if (rand(seedKey, "spike", k) >= density) continue;
    const center = (k + rand(seedKey, "center", k)) * slot;
    const d = (t - center) / w;
    best = Math.max(best, (0.35 + 0.65 * rand(seedKey, "amp", k)) * Math.exp(-d * d));
  }
  return best;
}

type Profile = {
  cpu: [number, number];
  mem: [number, number];
  rx: [number, number];
  tx: [number, number];
  rd: [number, number];
  wr: [number, number];
};

const profiles: Record<string, Profile> = {
  app: { cpu: [4, 60], mem: [190 * MiB, 90 * MiB], rx: [18_000, 380_000], tx: [46_000, 1_400_000], rd: [2_000, 80_000], wr: [8_000, 400_000] },
  postgres: { cpu: [3, 38], mem: [310 * MiB, 220 * MiB], rx: [22_000, 300_000], tx: [60_000, 900_000], rd: [40_000, 6_000_000], wr: [90_000, 4_000_000] },
  mysql: { cpu: [3, 30], mem: [380 * MiB, 160 * MiB], rx: [14_000, 200_000], tx: [40_000, 600_000], rd: [30_000, 3_000_000], wr: [70_000, 2_500_000] },
  mongo: { cpu: [2, 26], mem: [420 * MiB, 200 * MiB], rx: [12_000, 180_000], tx: [36_000, 520_000], rd: [60_000, 4_000_000], wr: [80_000, 3_000_000] },
  redis: { cpu: [1, 9], mem: [38 * MiB, 14 * MiB], rx: [10_000, 150_000], tx: [14_000, 220_000], rd: [0, 0], wr: [2_000, 120_000] },
};

function windowOf(range: MetricsRange) {
  const step = RANGE_SECONDS[range] / BUCKETS;
  const last = Math.floor(Date.now() / 1000 / step) * step;
  const start = last - (BUCKETS - 1) * step;
  return { step, start };
}

/** noisy builds one series: baseline with a daily wave, jitter, and spikes. */
function series(
  key: string,
  start: number,
  step: number,
  base: number,
  burst: number,
  opts: { noise?: number; density?: number; wave?: number } = {},
): number[] {
  const { noise = 0.25, density = 0.16, wave = 0.2 } = opts;
  return Array.from({ length: BUCKETS }, (_, i) => {
    const t = start + i * step + step / 2;
    const daily = 1 + wave * Math.sin((t / 86400) * 2 * Math.PI);
    const jitter = 1 + noise * (rand(key, "n", Math.floor(t / step)) - 0.5) * 2;
    return Math.max(0, base * daily * jitter + burst * spike(key, t, step, density));
  });
}

function serviceMetrics(s: Service, range: MetricsRange): Metrics {
  const { step, start } = windowOf(range);
  const p = (profiles[s.kind] ?? profiles["app"]) as Profile;
  const key = s.id;
  const cpu = series(key + "cpu", start, step, ...p.cpu);
  const rx = series(key + "rx", start, step, ...p.rx);
  const tx = series(key + "tx", start, step, ...p.tx);
  const rd = series(key + "rd", start, step, ...p.rd, { density: 0.1 });
  const wr = series(key + "wr", start, step, ...p.wr, { density: 0.14 });
  // Memory drifts up in a sawtooth that resets at restarts, plus small jitter.
  const mem = Array.from({ length: BUCKETS }, (_, i) => {
    const t = start + i * step + step / 2;
    const phase = ((t + rand(key, "phase") * 21 * 3600) % (21 * 3600)) / (21 * 3600);
    return p.mem[0] + p.mem[1] * 0.5 * phase + p.mem[1] * 0.04 * (rand(key, "m", Math.floor(t / step)) - 0.5) * 2 + p.mem[1] * 0.3 * spike(key + "mem", t, step, 0.08);
  });

  let up: boolean[] = Array(BUCKETS).fill(true);
  if (s.status === "stopped") up = Array(BUCKETS).fill(false);
  if (s.status === "crashed") {
    const crashAt = BOOT / 1000 - CRASH_MIN * 60;
    const limit = s.memoryLimit || 384 * MiB;
    up = up.map((_, i) => start + i * step < crashAt);
    for (let i = 0; i < BUCKETS; i++) {
      const t = start + i * step;
      const ramp = (t - (crashAt - 18 * 60)) / (18 * 60);
      if (ramp > 0 && ramp <= 1) {
        mem[i] = (mem[i] as number) + (limit * 0.97 - (mem[i] as number)) * ramp;
        cpu[i] = (cpu[i] as number) + 35 * ramp;
      }
    }
  }
  const mask = (xs: number[]) => xs.map((v, i) => (up[i] ? Math.round(v * 100) / 100 : null));
  return {
    range,
    start: iso(start * 1000),
    step,
    cpuLimit: s.cpuLimit,
    memoryLimit: s.memoryLimit,
    cpu: mask(cpu),
    memory: mask(mem.map(Math.round)),
    netRx: mask(rx.map(Math.round)),
    netTx: mask(tx.map(Math.round)),
    diskRead: mask(rd.map(Math.round)),
    diskWrite: mask(wr.map(Math.round)),
  };
}

function hostMetrics(range: MetricsRange): HostMetrics {
  const { step, start } = windowOf(range);
  const total = 8 * GiB;
  const diskTotal = 80 * 1e9;
  const cpu = series("host:cpu", start, step, 34, 190, { noise: 0.3, density: 0.12 });
  const memory = series("host:mem", start, step, 3.3 * GiB, 0.9 * GiB, { noise: 0.03, density: 0.06, wave: 0.05 });
  const disk = Array.from({ length: BUCKETS }, (_, i) => {
    const t = start + i * step;
    const daysAgo = (Date.now() / 1000 - t) / 86400;
    return 31.4e9 - daysAgo * 0.22e9 + 0.3e9 * Math.sin(t / 5400) * 0.2;
  });
  return {
    range,
    start: iso(start * 1000),
    step,
    cpus: 4,
    memoryTotal: total,
    diskTotal,
    cpu: cpu.map((v) => Math.round(Math.min(v, 400) * 10) / 10),
    memory: memory.map((v) => Math.round(Math.min(v, total * 0.92))),
    diskUsed: disk.map(Math.round),
    netRx: series("host:rx", start, step, 90_000, 1_800_000, { density: 0.12 }).map(Math.round),
    netTx: series("host:tx", start, step, 210_000, 4_200_000, { density: 0.12 }).map(Math.round),
    diskRead: series("host:rd", start, step, 120_000, 9_000_000, { density: 0.1 }).map(Math.round),
    diskWrite: series("host:wr", start, step, 300_000, 7_000_000, { density: 0.14 }).map(Math.round),
  };
}

GET("/services/:id/metrics", ({ params, url }) => {
  const s = service(params.id as string);
  return s ? serviceMetrics(s, parseRange(url)) : fail(404, "service not found");
});
GET("/host/metrics", ({ url }) => hostMetrics(parseRange(url)));

// -------------------------------------------------------------------- backups

function sortedBackups(filter: (b: Backup) => boolean): Backup[] {
  return state.backups.filter(filter).sort((a, b) => b.createdAt.localeCompare(a.createdAt));
}

const emptyPolicy = () => ({
  enabled: false,
  schedule: "0 3 * * *",
  compression: "default" as const,
  keepLocal: 7,
  upload: true,
  keepRemote: 30,
  nextRunAt: null,
});

function applyPolicy(input: BackupPolicyInput) {
  return { ...input, nextRunAt: input.enabled ? nextCron(input.schedule) : null };
}

/** runBackup queues a backup that finishes a few seconds later. */
function runBackup(serviceId: string | null): Backup {
  const svc = serviceId ? state.services.get(serviceId) : undefined;
  const now = new Date();
  const stamp = now.toISOString().replace(/[-:]/g, "").replace("T", "-").slice(0, 15);
  const kind = svc?.kind;
  const ext = !svc ? "db.zst" : kind === "postgres" || kind === "mysql" ? "sql.zst" : kind === "redis" ? "rdb.zst" : kind === "mongo" ? "archive.zst" : "tar.zst";
  const b: Backup = {
    id: newId("bak"),
    serviceId,
    trigger: "manual",
    method: !svc ? "sqlite" : kind === "app" ? "volume" : "dump",
    status: "queued",
    fileName: `${svc?.name ?? "shed"}-${stamp.slice(0, 8)}-${stamp.slice(9)}.${ext}`,
    size: 0,
    encrypted: !!state.backupSettings.encryption.enabled,
    local: false,
    remote: false,
    remoteError: "",
    error: "",
    createdAt: now.toISOString(),
    finishedAt: null,
  };
  state.backups.unshift(b);
  setTimeout(() => (b.status = "running"), 800);
  setTimeout(() => (b.status = state.backupSettings.s3 ? "uploading" : "running"), 2500);
  setTimeout(() => {
    b.status = "succeeded";
    b.size = Math.round((svc ? 12 : 3.5) * MiB * (0.9 + Math.random() * 0.2));
    b.local = true;
    b.remote = !!state.backupSettings.s3;
    b.finishedAt = iso(Date.now());
  }, 4500);
  audit("backup.run", svc, { backup: b.id });
  return b;
}

GET("/services/:id/backups", ({ params, url }) => {
  const id = params.id as string;
  if (!state.services.has(id)) return fail(404, "service not found");
  const p = page(sortedBackups((b) => b.serviceId === id), url);
  if (p instanceof Response) return p;
  const restore = [...state.restores.values()].filter((r) => r.serviceId === id).sort((a, b) => b.createdAt.localeCompare(a.createdAt))[0] ?? null;
  return {
    policy: state.servicePolicies.get(id) ?? emptyPolicy(),
    backups: p.items,
    next: p.next,
    restore,
  };
});
PUT("/services/:id/backups/policy", ({ params, body }) => {
  const policy = applyPolicy(body as BackupPolicyInput);
  state.servicePolicies.set(params.id as string, policy);
  audit("backup.policy.update", service(params.id as string));
  return policy;
});
POST("/services/:id/backups", ({ params }) => {
  const s = state.services.get(params.id as string);
  if (!s) return fail(404, "service not found");
  if (!s.volumes.length) return fail(400, "service has no volumes to back up");
  if (state.backups.some((b) => b.serviceId === s.id && (b.status === "queued" || b.status === "running" || b.status === "uploading"))) {
    return fail(409, "a backup is already in progress");
  }
  return json(runBackup(s.id), 202);
});
GET("/backups/system", ({ url }) => {
  const p = page(sortedBackups((b) => b.serviceId === null), url);
  return p instanceof Response ? p : { policy: state.systemPolicy, backups: p.items, next: p.next };
});
PUT("/backups/system/policy", ({ body }) => {
  state.systemPolicy = applyPolicy(body as BackupPolicyInput);
  return state.systemPolicy;
});
POST("/backups/system", () => json(runBackup(null), 202));
GET("/backups/settings", () => state.backupSettings);
PUT("/backups/settings", ({ body }) => {
  const input = body as BackupSettingsInput;
  const prev = state.backupSettings.s3;
  state.backupSettings = {
    s3: input.s3
      ? {
          endpoint: input.s3.endpoint,
          region: input.s3.region,
          bucket: input.s3.bucket,
          prefix: input.s3.prefix,
          accessKeyId: input.s3.accessKeyId,
          pathStyle: input.s3.pathStyle,
          hasSecret: !!input.s3.secretAccessKey || !!prev?.hasSecret,
        }
      : null,
    encryption: {
      enabled: input.encryption.enabled,
      recipient: state.backupSettings.encryption.recipient || "age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p",
    },
  };
  audit("backup.settings.update");
  return state.backupSettings;
});
POST("/backups/settings/test", async ({ body }) => {
  await sleep(700);
  const input = body as BackupSettingsInput;
  if (!input.s3?.bucket) return fail(400, "s3: bucket is required");
  return noContent();
});
GET("/backups/settings/key", () => ({
  identity: "AGE-SECRET-KEY-1" + hex("age-key", 52).toUpperCase(),
}));
GET("/backups/:id/download", ({ params }) => {
  const b = state.backups.find((x) => x.id === params.id);
  if (!b) return fail(404, "backup not found");
  return new Response(new Uint8Array(512).fill(0), {
    headers: { "Content-Type": "application/zstd", "Content-Disposition": `attachment; filename="${b.fileName}"` },
  });
});
POST("/backups/:id/restore", ({ params }) => {
  const b = state.backups.find((x) => x.id === params.id);
  if (!b || !b.serviceId) return fail(400, "backup cannot be restored");
  if (b.status !== "succeeded") return fail(400, "only successful backups can be restored");
  const s = state.services.get(b.serviceId);
  if (!s) return fail(404, "service not found");
  if (s.restoreFence) return fail(409, "a restore is already running");
  const r: Restore = {
    id: newId("rst"),
    serviceId: s.id,
    backupId: b.id,
    status: "running",
    error: "",
    createdAt: iso(Date.now()),
    finishedAt: null,
  };
  state.restores.set(r.id, r);
  s.restoreFence = { restoreId: r.id, phase: "retaining", createdAt: r.createdAt };
  setTimeout(() => s.restoreFence && (s.restoreFence.phase = "replacing"), 1500);
  setTimeout(() => s.restoreFence && (s.restoreFence.phase = "loading"), 3000);
  setTimeout(() => {
    r.status = "succeeded";
    r.finishedAt = iso(Date.now());
    s.restoreFence = null;
  }, 5000);
  audit("backup.restore", s, { backup: b.id });
  return json(r, 202);
});
DELETE("/backups/:id", ({ params }) => {
  const i = state.backups.findIndex((x) => x.id === params.id);
  if (i < 0) return fail(404, "backup not found");
  const b = state.backups[i] as Backup;
  if (b.status === "queued" || b.status === "running" || b.status === "uploading") return fail(409, "backup is in progress");
  state.backups.splice(i, 1);
  return noContent();
});

// --------------------------------------------------------------------- audit

GET("/audit", ({ url }) => {
  const q = url.searchParams;
  const limit = Math.min(Number(q.get("limit")) || 100, 500);
  const before = q.get("before") ? Number(q.get("before")) : Infinity;
  const actor = q.get("actor");
  const action = q.get("action");
  const svc = q.get("service");
  const outcome = q.get("outcome");
  if (outcome && !["ok", "failed", "denied"].includes(outcome)) return fail(400, "invalid outcome");
  const matches = state.audit.filter(
    (e) =>
      e.id < before &&
      (!actor || e.actor === actor || e.actorKind === actor) &&
      (!action || e.action === action || e.action.startsWith(action + ".")) &&
      (!svc || e.serviceId === svc) &&
      (!outcome || e.outcome === outcome),
  );
  const events = matches.slice(0, limit);
  const page: AuditPage = {
    events,
    next: matches.length > limit ? ((events[events.length - 1] as { id: number }).id) : null,
  };
  return page;
});
GET("/audit/settings", () => ({ retentionDays: state.auditRetentionDays }));
PUT("/audit/settings", ({ body }) => {
  const d = Number(body?.retentionDays);
  if (!(d >= 1 && d <= 3650)) return fail(400, "retentionDays must be 1 to 3650");
  state.auditRetentionDays = d;
  audit("audit.settings.update", undefined, { retentionDays: d });
  return { retentionDays: d };
});

// ------------------------------------------------------------- notifications

const publicChannel = ({ target: _t, ...c }: (typeof state.channels)[number]) => c;

GET("/notifications/providers", () => ["hark"]);
GET("/notifications/channels", () => state.channels.map(publicChannel));
POST("/notifications/channels", ({ body }) => {
  const input = body as NotificationChannelInput;
  if (!input.name?.trim()) return fail(400, "name is required");
  if (input.provider !== "hark") return fail(400, `unknown provider "${input.provider}"`);
  if (!input.target?.startsWith("https://")) return fail(400, "hark: target must be an https:// webhook URL");
  const c = { id: newId("chn"), name: input.name, provider: input.provider, enabled: input.enabled, createdAt: iso(Date.now()), target: input.target };
  state.channels.push(c);
  audit("notification.channel.create", undefined, { name: c.name, provider: c.provider });
  return json(publicChannel(c), 201);
});
PATCH("/notifications/channels/:id", ({ params, body }) => {
  const c = state.channels.find((x) => x.id === params.id);
  if (!c) return fail(404, "channel not found");
  const p = body as NotificationChannelPatch;
  if (p.name !== undefined) c.name = p.name;
  if (p.enabled !== undefined) c.enabled = p.enabled;
  if (p.target) c.target = p.target;
  return publicChannel(c);
});
DELETE("/notifications/channels/:id", ({ params }) => {
  const i = state.channels.findIndex((x) => x.id === params.id);
  if (i < 0) return fail(404, "channel not found");
  state.channels.splice(i, 1);
  state.deliveries = state.deliveries.filter((d) => d.channelId !== params.id);
  return noContent();
});
POST("/notifications/channels/:id/test", async ({ params }) => {
  const c = state.channels.find((x) => x.id === params.id);
  if (!c) return fail(404, "channel not found");
  await sleep(600);
  return noContent();
});
GET("/notifications/rules", () => state.rules);
PATCH("/notifications/rules/:id", ({ params, body }) => {
  const r = state.rules.find((x) => x.id === params.id);
  if (!r) return fail(404, "rule not found");
  const p = body as NotificationRulePatch;
  if (p.enabled !== undefined) r.enabled = p.enabled;
  if (p.threshold !== undefined) {
    if (!(p.threshold > 0)) return fail(400, "threshold must be greater than 0");
    r.threshold = p.threshold;
  }
  if (p.forSeconds !== undefined) r.forSeconds = p.forSeconds;
  return r;
});
GET("/notifications/deliveries", ({ url }) => {
  const limit = Math.min(Number(url.searchParams.get("limit")) || 50, 200);
  return state.deliveries.slice(0, limit);
});

// -------------------------------------------------------------------- update

const u = state.update;
POST("/update/check", async () => {
  await sleep(500);
  u.checkedAt = iso(Date.now());
  return u;
});
POST("/update/download", () => {
  if (!u.available) return fail(409, "no newer release");
  u.state = "downloading";
  setTimeout(() => {
    u.state = "idle";
    u.staged = u.latest?.version ?? "";
  }, 3500);
  return json(u, 202);
});
PUT("/update/settings", ({ body }) => {
  if (typeof body?.autoDownload === "boolean") u.autoDownload = body.autoDownload;
  if (body?.channel === "stable" || body?.channel === "nightly") u.channel = body.channel;
  else if (body?.channel !== undefined) return fail(400, "channel must be stable or nightly");
  return u;
});
POST("/update/install", () => {
  if (!u.staged) return fail(409, "nothing is downloaded");
  u.state = "restarting";
  const next = u.staged;
  setTimeout(() => {
    u.current = next;
    u.available = false;
    u.staged = "";
    u.state = "idle";
  }, 6000);
  return json(u, 202);
});
// The restart screen polls this; the bare path is the same as GET /update.
GET("/update", () => u);

// --------------------------------------------------------------------- GitHub

GET("/github/repos", () => repos);
GET("/github/repos/:owner/:repo/branches", ({ params }) => branchesFor(`${params.owner}/${params.repo}`));
POST("/github/webhook", () => noContent());

// ---------------------------------------------------------------------- OAuth

GET("/oauth/requests/:id", ({ params }) => {
  if (params.id === "expired") return fail(404, "request not found");
  return {
    id: params.id,
    clientName: "Claude Code",
    clientUri: "https://claude.com/claude-code",
    redirectHost: "localhost",
    scopes: ["read"],
  };
});
POST("/oauth/requests/:id", ({ body }) => ({
  redirect: body?.approve ? "/settings?authorized=1" : "/settings?denied=1",
}));
GET("/oauth/grants", () => state.grants);
DELETE("/oauth/grants/:id", ({ params }) => {
  const i = state.grants.findIndex((g) => g.id === params.id);
  if (i < 0) return fail(404, "grant not found");
  state.grants.splice(i, 1);
  return noContent();
});

// ------------------------------------------------------------------ telemetry

const telemetryOff = () => !state.telemetry.settings.enabled;

GET("/telemetry", () => state.telemetry);
PUT("/telemetry", ({ body }) => {
  const s = body as TelemetrySettings;
  if (s.diskLimitGB < 1) return fail(400, "diskLimitGB must be at least 1");
  const wasOn = state.telemetry.settings.enabled;
  state.telemetry.settings = s;
  const want = s.enabled ? "running" : "stopped";
  for (const c of state.telemetry.components) {
    if (s.enabled !== wasOn) c.state = s.enabled ? "starting" : "stopped";
    if (c.state === "starting") setTimeout(() => (c.state = want), 3500);
  }
  audit("telemetry.update");
  return state.telemetry;
});
POST("/telemetry/components/:name/restart", ({ params }) => {
  const c = state.telemetry.components.find((x) => x.name === params.name);
  if (!c) return fail(404, "unknown component");
  if (telemetryOff()) return fail(409, "telemetry is off");
  c.state = "starting";
  setTimeout(() => (c.state = "running"), 2500);
  return state.telemetry;
});
DELETE("/telemetry/data", () => (telemetryOff() ? noContent() : fail(409, "telemetry is enabled")));
POST("/telemetry/dashboards/:uid/reset", () => noContent());
POST("/telemetry/dashboards/restore", () => noContent());
GET("/telemetry/trace/:id", ({ params }) => {
  if (!/^[0-9a-f]{32}$/.test(params.id as string)) return fail(400, "trace ID must be 32 hex characters");
  return new Response(null, { status: 302, headers: { Location: `/grafana/explore?trace=${params.id}` } });
});

function serviceTelemetry(s: Service): ServiceTelemetry {
  const t = state.telemetry;
  const caps = state.serviceCaps.get(s.id);
  const dash = (kind: string, title: string) => ({
    uid: `svc-${kind}-${s.id}`,
    title,
    url: `${t.grafanaUrl}d/svc-${kind}-${s.id}`,
    customized: kind === "overview" && s.name === "web",
    outdated: false,
  });
  return {
    caps: caps ?? t.settings.defaultCaps,
    overridden: !!caps,
    dropping: s.id === "svc_api7mz3",
    listenerError: s.kind === "app" ? "" : "databases do not run an OTLP listener",
    folderUrl: t.settings.enabled ? `${t.grafanaUrl}dashboards/f/p-${s.projectId}/` : "",
    dashboards: s.kind === "app" ? [dash("overview", `${s.name} / Overview`), dash("http", `${s.name} / HTTP`)] : [dash("overview", `${s.name} / Overview`)],
    firingAlerts: s.id === "svc_api7mz3" ? [{ name: "shed-telemetry-drops", since: iso(Date.now() - 14 * MIN) }] : [],
  };
}
GET("/services/:id/telemetry", ({ params }) => {
  const s = service(params.id as string);
  return s ? serviceTelemetry(s) : fail(404, "service not found");
});
PUT("/services/:id/telemetry", ({ params, body }) => {
  const s = service(params.id as string);
  if (!s) return fail(404, "service not found");
  if (body?.caps === null) state.serviceCaps.delete(s.id);
  else if (body?.caps) {
    const c = body.caps;
    if (c.spansPerSecond < 1 || c.logsPerSecond < 1 || c.series < 1) return fail(400, "caps must be at least 1");
    state.serviceCaps.set(s.id, c);
  }
  return serviceTelemetry(s);
});

// ---------------------------------------------------------------- log history

function parseQuery(q: string): { words: string[]; fields: [string, string][] } {
  const words: string[] = [];
  const fields: [string, string][] = [];
  for (const m of q.matchAll(/([\w.-]+):"([^"]*)"|([\w.-]+):(\S+)|"([^"]*)"|(\S+)/g)) {
    if (m[1]) fields.push([m[1], (m[2] ?? "").toLowerCase()]);
    else if (m[3] && !/^https?$/.test(m[3])) fields.push([m[3], (m[4] ?? "").toLowerCase()]);
    else words.push((m[5] ?? m[6] ?? m[0]).toLowerCase());
  }
  return { words, fields };
}

GET("/services/:id/logs/history", ({ params, url }) => {
  const id = params.id as string;
  if (!state.services.has(id)) return fail(404, "service not found");
  if (telemetryOff()) return fail(409, "telemetry is off");
  const q = url.searchParams;
  const level = q.get("level");
  if (level && !["debug", "info", "warn", "error"].includes(level)) return fail(400, "invalid level");
  const limit = Math.min(Number(q.get("limit")) || 100, 500);
  const from = q.get("from") ? Date.parse(q.get("from") as string) : 0;
  const to = q.get("to") ? Date.parse(q.get("to") as string) : Infinity;
  const before = q.get("before") ? Date.parse(q.get("before") as string) : Infinity;
  const { words, fields } = parseQuery(q.get("q") ?? "");
  const all = state.logHistory.get(id) ?? [];
  const matches = all.filter((r) => {
    const t = Date.parse(r.time);
    if (t < from || t > to || t >= before) return false;
    if (level && r.level !== level) return false;
    const hay = `${r.message} ${Object.values(r.fields).join(" ")}`.toLowerCase();
    if (!words.every((w) => hay.includes(w))) return false;
    return fields.every(([k, v]) => {
      const val = k === "level" ? r.level : k === "traceId" || k === "trace_id" ? r.traceId : k === "source" ? r.source : r.fields[k];
      return (val ?? "").toLowerCase().includes(v);
    });
  });
  const records = matches.slice(0, limit);
  const page: LogPage = {
    records,
    next: records.length === limit && matches.length > limit ? (records[records.length - 1] as { time: string }).time : "",
  };
  return page;
});

// ----------------------------------------------------------------------- SSE

type Send = (event: string, data: string) => void;

function sse(req: Request, run: (send: Send, signal: AbortSignal, end: () => void) => void | Promise<void>): Response {
  const enc = new TextEncoder();
  const ac = new AbortController();
  let beat: ReturnType<typeof setInterval>;
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      let closed = false;
      const write = (s: string) => {
        if (!closed) controller.enqueue(enc.encode(s));
      };
      const end = () => {
        if (closed) return;
        write("event: end\ndata: \n\n");
        closed = true;
        clearInterval(beat);
        ac.abort();
        controller.close();
      };
      const send: Send = (event, data) => write(`event: ${event}\ndata: ${data}\n\n`);
      write(": connected\n\n");
      beat = setInterval(() => write(": keepalive\n\n"), 15_000);
      req.signal.addEventListener("abort", () => {
        closed = true;
        clearInterval(beat);
        ac.abort();
      });
      void run(send, ac.signal, end);
    },
    cancel() {
      clearInterval(beat);
      ac.abort();
    },
  });
  return new Response(stream, {
    headers: { "Content-Type": "text/event-stream", "Cache-Control": "no-cache, no-transform", Connection: "keep-alive" },
  });
}

/** every calls fn after randomized delays until the signal aborts. */
function every(signal: AbortSignal, minMs: number, maxMs: number, fn: () => void) {
  const tick = () => {
    if (signal.aborted) return;
    fn();
    setTimeout(tick, minMs + Math.random() * (maxMs - minMs));
  };
  setTimeout(tick, minMs);
}

const paths = ["/", "/blog", "/blog/running-a-homelab-vps", "/uses", "/now", "/rss.xml", "/api/health", "/_next/static/chunks/main.js", "/api/monitors", "/api/probes", "/api/incidents", "/rooms/lobby", "/api/rooms", "/up"];
const ips = ["203.0.113.24", "198.51.100.7", "192.0.2.88", "203.0.113.190"];
let lineNo = 0;

/** clfTime formats t like 02/Nov/2026:10:11:12 for common-log lines. */
function clfTime(t: number): string {
  const [, d, mon, y, time] = /^\w+, (\d+) (\w+) (\d+) ([\d:]+)/.exec(new Date(t).toUTCString()) ?? [];
  return `${d}/${mon}/${y}:${time}`;
}

function runtimeLine(s: Service, t: number): string {
  const ts = new Date(t).toISOString();
  const n = lineNo++;
  const r = Math.random();
  switch (s.kind) {
    case "postgres":
      return r < 0.6
        ? `${ts} 2026-11-02 ${ts.slice(11, 23)} UTC [${28 + (n % 5)}] LOG:  duration: ${(Math.random() * 40).toFixed(3)} ms  statement: SELECT id, status FROM monitors WHERE next_run_at <= now() LIMIT 200`
        : r < 0.85
          ? `${ts} 2026-11-02 ${ts.slice(11, 23)} UTC [27] LOG:  checkpoint complete: wrote ${40 + (n % 90)} buffers (0.${n % 9}%); 0 WAL file(s) added`
          : r < 0.95
            ? `${ts} 2026-11-02 ${ts.slice(11, 23)} UTC [${100 + n}] LOG:  connection received: host=${s.name}`
            : `${ts} 2026-11-02 ${ts.slice(11, 23)} UTC [${100 + n}] WARNING:  there is already a transaction in progress`;
    case "mysql":
      return r < 0.85
        ? `${ts} ${ts.slice(0, 19)}.${ts.slice(20, 26)}Z 0 [Note] [MY-010914] [Server] Aborted connection ${1000 + n} to db: 'alias_production' user: 'alias' host: '10.0.2.${n % 200}' (Got timeout reading communication packets).`
        : `${ts} ${ts.slice(0, 19)}.${ts.slice(20, 26)}Z 0 [Warning] [MY-013360] [Server] Plugin mysql_native_password reported: deprecated`;
    case "mongo":
      return r < 0.9
        ? `${ts} {"t":{"$date":"${ts}"},"s":"I","c":"NETWORK","id":22943,"ctx":"listener","msg":"Connection accepted","attr":{"remote":"10.0.3.${n % 50}:${40000 + n}","connectionCount":${6 + (n % 5)}}}`
        : `${ts} {"t":{"$date":"${ts}"},"s":"W","c":"COMMAND","id":23799,"ctx":"conn${n}","msg":"Slow query","attr":{"ns":"dismegle.rooms","durationMillis":${210 + (n % 400)}}}`;
    case "redis":
      return r < 0.7
        ? `${ts} 1:M ${new Date(t).toUTCString().slice(5, 25)}.${String(t % 1000).padStart(3, "0")} * ${1 + (n % 50)} changes in 60 seconds. Saving...`
        : `${ts} 1:M ${new Date(t).toUTCString().slice(5, 25)}.${String(t % 1000).padStart(3, "0")} * Background saving terminated with success`;
    default: {
      const path = pick(paths, n, s.id);
      const status = r < 0.86 ? 200 : r < 0.92 ? 304 : r < 0.96 ? 404 : r < 0.985 ? 429 : 500;
      const ms = Math.round(3 + Math.random() * (status === 500 ? 900 : 60));
      if (s.name === "worker" || s.name === "scheduler") {
        return r < 0.7
          ? `${ts} {"level":"info","msg":"probe finished","monitor":"mon_${hex("m" + (n % 40), 6)}","region":"${pick(["fra", "sin", "iad"], n)}","status":${status === 500 ? 0 : 200},"durationMs":${ms * 4},"ts":"${ts}"}`
          : r < 0.93
            ? `${ts} {"level":"debug","msg":"scheduler tick","due":${180 + (n % 40)}}`
            : `${ts} {"level":"warn","msg":"probe retry after ECONNRESET","attempt":2}`;
      }
      if (s.name === "api" || s.name === "dismegle") {
        const level = status >= 500 ? "error" : status >= 400 ? "warn" : "info";
        return `${ts} {"level":"${level}","msg":"request","method":"${r < 0.2 ? "POST" : "GET"}","path":"${path}","status":${status},"duration":"${ms}ms","ip":"${pick(ips, n)}"}`;
      }
      if (r < 0.04) return `${ts} ${s.name}: warn: image optimization took ${ms * 10}ms for ${path}`;
      return `${ts} ${pick(ips, n)} - - [${clfTime(t)} +0000] "GET ${path} HTTP/1.1" ${status} ${200 + ((n * 37) % 9000)} "-" "Mozilla/5.0" ${ms}ms`;
    }
  }
}

function startupLines(s: Service, t: number): string[] {
  const ts = (o: number) => new Date(t + o).toISOString();
  if (s.kind === "postgres") {
    return [
      `${ts(0)} PostgreSQL init process complete; ready for start up.`,
      `${ts(40)} 2026-11-02 ${ts(40).slice(11, 23)} UTC [1] LOG:  starting PostgreSQL 17.2 on x86_64-pc-linux-musl`,
      `${ts(60)} 2026-11-02 ${ts(60).slice(11, 23)} UTC [1] LOG:  listening on IPv4 address "0.0.0.0", port 5432`,
      `${ts(120)} 2026-11-02 ${ts(120).slice(11, 23)} UTC [1] LOG:  database system is ready to accept connections`,
    ];
  }
  if (s.kind !== "app") return [`${ts(0)} ${s.kind} ${s.name}: ready to accept connections`];
  return [
    `${ts(0)} > ${s.name}@1.0.0 start`,
    `${ts(120)} {"level":"info","msg":"starting","version":"${hex(s.id, 7)}","node":"v22.11.0"}`,
    `${ts(310)} {"level":"info","msg":"connected to database","pool":10}`,
    ...(s.port ? [`${ts(480)} {"level":"info","msg":"listening","port":${s.port}}`] : [`${ts(480)} {"level":"info","msg":"worker ready","concurrency":64}`]),
  ];
}

GET("/services/:id/logs", ({ req, params }) => {
  const s = service(params.id as string);
  if (!s) return fail(404, "service not found");
  return sse(req, (send, signal, end) => {
    if (s.status === "stopped" || s.status === "offline") return end();
    const now = Date.now();
    const lines = startupLines(s, now - 25 * MIN);
    for (let i = 0; i < 80; i++) lines.push(runtimeLine(s, now - 25 * MIN + 1000 + i * 17_000));
    if (s.status === "crashed") {
      lines.push(
        `${new Date(now - CRASH_MIN * MIN).toISOString()} {"level":"error","msg":"fatal: JavaScript heap out of memory","rss":"378MB"}`,
        `${new Date(now - CRASH_MIN * MIN + 20).toISOString()} FATAL ERROR: Reached heap limit Allocation failed - JavaScript heap out of memory`,
      );
      for (const l of lines) send("log", l);
      return end();
    }
    for (const l of lines) send("log", l);
    every(signal, 450, 1400, () => send("log", runtimeLine(s, Date.now())));
  });
});

// Build logs: BuildKit plain progress after "==> " headings.
function buildScript(s: Service, d: Deployment): { lines: string[]; failAt: number } {
  const sha = d.commitSha.slice(0, 7);
  const isImage = !s.repo;
  const L: string[] = [];
  if (isImage) {
    L.push(`==> Pulling ${s.image}`, `${s.image.split(":")[1] ?? "latest"}: Pulling from ${s.image.split(":")[0]?.replace(/^ghcr\.io\//, "") ?? ""}`, "Digest: sha256:" + hex(d.id, 64), `Status: Downloaded newer image for ${s.image}`);
    L.push("==> Starting container", `==> Waiting for health check${s.healthcheckPath ? ` GET ${s.healthcheckPath}` : ""}`, "==> Health check passed", "==> Deployed");
    return { lines: L, failAt: -1 };
  }
  L.push(
    `==> Cloning ${s.repo}@${s.branch}`,
    `Cloning into '/workspace'...`,
    `HEAD is now at ${sha} ${d.commitMessage}`,
    `==> Building image shed/${s.name}:${sha}`,
    "#1 [internal] load build definition from Dockerfile",
    "#1 transferring dockerfile: 1.42kB done",
    "#1 DONE 0.0s",
    "#2 [internal] load metadata for docker.io/library/node:22-alpine",
    "#2 DONE 0.9s",
    "#3 [internal] load .dockerignore",
    "#3 transferring context: 2B done",
    "#3 DONE 0.0s",
    "#4 [1/6] FROM docker.io/library/node:22-alpine@sha256:" + hex("node22", 64),
    "#4 DONE 0.0s",
    "#5 [2/6] WORKDIR /app",
    "#5 CACHED",
    "#6 [3/6] COPY package.json package-lock.json ./",
    "#6 DONE 0.1s",
    "#7 [4/6] RUN npm ci",
    "#7 1.204 npm warn deprecated inflight@1.0.6: This module is not supported, and leaks memory.",
    "#7 6.882 added 612 packages, and audited 613 packages in 7s",
    "#7 6.883 found 0 vulnerabilities",
    "#7 DONE 7.4s",
    "#8 [5/6] COPY . .",
    "#8 DONE 0.3s",
    "#9 [6/6] RUN npm run build",
    "#9 0.812 > build",
    "#9 2.440 vite v7.1.4 building for production...",
    "#9 9.015 ✓ 214 modules transformed.",
    "#9 9.502 dist/assets/index-Bq3x9sKe.js   182.44 kB │ gzip: 58.12 kB",
    "#9 DONE 11.8s",
    "#10 exporting to image",
    "#10 exporting layers 1.2s done",
    `#10 naming to docker.io/shed/${s.name}:${sha} done`,
    "#10 DONE 1.4s",
    "==> Starting container",
    `==> Waiting for health check${s.healthcheckPath ? ` GET ${s.healthcheckPath}` : ""}`,
    "==> Health check passed",
    "==> Switching traffic",
    "==> Deployed",
  );
  const failAt = d.status === "failed" ? L.indexOf("#9 DONE 11.8s") : -1;
  return { lines: L, failAt };
}

GET("/deployments/:id/logs", ({ req, params }) => {
  const d = findDeployment(params.id as string);
  const s = d && state.services.get(d.serviceId);
  if (!d || !s) return fail(404, "deployment not found");
  return sse(req, (send, signal, end) => {
    const { lines, failAt } = buildScript(s, d);
    if (d.status === "skipped" || (d.status === "canceled" && !d.startedAt)) return end();
    const stuckCut = lines.indexOf("==> Starting container");
    const live = isPending(d.status);
    if (!live) {
      const body = failAt >= 0 ? [...lines.slice(0, failAt), "#9 1.980 error during build: Rollup failed to resolve import \"./components/Missing\"", `#9 ERROR: process "/bin/sh -c npm run build" did not complete successfully: exit code: 1`] : d.status === "canceled" ? lines.slice(0, Math.min(14, lines.length)) : lines;
      for (const l of body) send("log", l);
      send("status", JSON.stringify({ status: d.status }));
      return end();
    }
    send("status", JSON.stringify({ status: d.status }));
    let i = 0;
    const tick = () => {
      if (signal.aborted) return;
      const stuck = state.stuck.has(d.id);
      if (!isPending(d.status)) {
        while (i < lines.length) send("log", lines[i++] as string);
        send("status", JSON.stringify({ status: d.status }));
        return end();
      }
      const limit = stuck ? stuckCut : lines.length - 1;
      if (i < limit) send("log", lines[i++] as string);
      setTimeout(tick, stuck ? 600 : 450);
    };
    tick();
  });
});

GET("/logs", ({ req }) =>
  sse(req, (send, signal) => {
    const line = (t: number) => {
      const n = lineNo++;
      const r = Math.random();
      const ts = new Date(t).toISOString();
      if (r < 0.55) return `time=${ts} level=INFO msg="http request" method=GET path=${pick(["/api/projects", "/api/me", "/api/services/svc_web4kq1", "/api/host/metrics", "/api/services/svc_api7mz3/deployments"], n)} status=200 duration=${(Math.random() * 6).toFixed(1)}ms`;
      if (r < 0.7) return `time=${ts} level=INFO msg="metrics sampled" containers=${state.services.size - 2} took=${Math.round(20 + Math.random() * 40)}ms`;
      if (r < 0.8) return `time=${ts} level=INFO msg="routes applied" routes=${[...state.services.values()].reduce((a, s) => a + s.domains.length, 0)}`;
      if (r < 0.9) return `time=${ts} level=DEBUG msg="reconcile tick" services=${state.services.size} drift=0`;
      if (r < 0.96) return `time=${ts} level=INFO msg="github webhook" event=push repo=kyledickey/aretheyup ref=refs/heads/main`;
      return `time=${ts} level=WARN msg="container restarting" service=worker project=aretheyup exit_code=137`;
    };
    const now = Date.now();
    for (let i = 0; i < 120; i++) send("log", line(now - (120 - i) * 9000));
    every(signal, 600, 2200, () => send("log", line(Date.now())));
  }),
);

// -------------------------------------------------------------------- server

const port = Number(process.env["PORT"]) || 3000;
const server = BunRuntime.serve({
  port,
  hostname: "127.0.0.1",
  idleTimeout: 0,
  async fetch(req) {
    const url = new URL(req.url);
    if (!url.pathname.startsWith("/api/")) {
      return fail(404, "not found");
    }
    const path = url.pathname.slice(4).replace(/\/$/, "") || "/";
    for (const r of routes) {
      if (r.method !== req.method) continue;
      const m = r.re.exec(path);
      if (!m) continue;
      const params: Record<string, string> = {};
      r.keys.forEach((k, i) => (params[k] = decodeURIComponent(m[i + 1] as string)));
      let body: unknown;
      if (req.method !== "GET" && req.method !== "DELETE") {
        body = await req.json().catch(() => ({}));
      }
      try {
        const out = await r.handler({ req, url, params, body });
        if (out === undefined) return noContent();
        return out instanceof Response ? out : json(out);
      } catch (err) {
        console.error(`mock: ${req.method} ${path} threw`, err);
        return fail(500, "mock server error");
      }
    }
    console.error(`mock: no route for ${req.method} ${url.pathname}${url.search}`);
    return fail(404, "not found");
  },
});

console.log(`shed mock API listening on http://${server.hostname}:${server.port} (${routes.length} routes)`);
