# shed design

shed is a self-hosted, single-VPS deployment platform inspired by Railway.
One Go binary runs the dashboard, the API, the deploy pipeline, and an
embedded Caddy reverse proxy. Docker runs all workloads.

## Scope (MVP)

- Projects contain services. No environments.
- Service kinds: `app` (GitHub repo or Docker image), `postgres`, `mysql`,
  `mongo`, `redis`.
- Apps build from a Dockerfile if present, otherwise Railpack.
- Push to the configured branch deploys automatically; optionally wait for CI
  (GitHub check runs + commit statuses) to pass first.
- Per-service variables, domains, volumes, deployment history, build logs,
  runtime logs.
- Persistent Docker volumes survive deploys.
- Sign in with GitHub through a single GitHub App, which is also used for repo
  access, push webhooks, and CI status.

## Host requirements

Linux, Docker Engine with the buildx plugin, `git`, and `railpack` on `PATH`.
shed runs as root (or a docker-group user) under systemd.

## Configuration

koanf loads a TOML file (default `/etc/shed/shed.toml`, `-config` flag) and then
environment variables prefixed with `SHED_`. The env mapping strips the
prefix, lowercases, and replaces the first `_` with `.`:
`SHED_SERVER_URL` → `server.url`, `SHED_PROXY_ACME_EMAIL` → `proxy.acme_email`.

The application log is written next to the config file (`shed.log`) through
lumberjack and also mirrored to stderr.

```toml
[server]
listen = "127.0.0.1:3000"          # dashboard + API listener (Caddy fronts it)
url    = "https://shed.example.com" # public dashboard URL; used for GitHub callbacks

[data]
dir = "/var/lib/shed"              # shed.db, builds/, logs/

[proxy]
enabled     = true
http_port   = 80
https_port  = 443
acme_email  = ""
base_domain = ""                   # e.g. "apps.example.com" → generated domains

[auth]
allowed_users = []                 # Explicit GitHub logins; configure before first sign-in

[log]
level = "info"
max_size_mb = 20
max_backups = 5
max_age_days = 30
```

GitHub App credentials, the session secret, and other runtime state live in
the database (`settings` table), not the config file.

## Packages

Each package is a self-contained piece with a narrow API. Leaf packages import
nothing from `internal/`. Consumers define the small interfaces they need
(Go idiom: interfaces belong to the consumer), so pieces can be swapped.

| Package | Responsibility | Imports internal |
|---|---|---|
| `cmd/shed` | flags, config, logging, wiring, signals | everything |
| `internal/config` | koanf → `Config` struct with defaults + validation | — |
| `internal/store` | SQLite (modernc), embedded migrations, CRUD on plain structs | — |
| `internal/docker` | Docker Engine ops via `github.com/moby/moby/client` | — |
| `internal/build` | clone a commit, build an image (Dockerfile or Railpack) | — |
| `internal/proxy` | embedded Caddy; `Apply(routes)` | — |
| `internal/github` | GitHub App: manifest, JWT, installation tokens, repos, branches, CI status, OAuth, webhooks | — |
| `internal/vars` | `${{ ... }}` variable reference resolution | — |
| `internal/catalog` | database templates (image, port, volume path, default vars) | — |
| `internal/deploy` | deployment pipeline, per-service queue, reconcile on boot | interfaces only + store/catalog/vars types |
| `internal/metrics` | container resource sampling, per-service time series | interfaces only + docker/store types |
| `internal/auth` | sessions, GitHub sign-in handlers, middleware | store via interface |
| `internal/api` | JSON HTTP API, SSE logs, webhook endpoint, SPA serving | deploy, auth, github, metrics, store |
| `web` | Vite+ React dashboard; `embed.go` exposes `dist` as `fs.FS` | — |

Style: Google Go style guide and Go doc comments. Every package has a
`doc.go`-quality package comment. Errors are wrapped with context
(`fmt.Errorf("build: clone %s: %w", repo, err)`). `log/slog` everywhere,
passed in explicitly (no globals). `context.Context` first arg on anything
that does I/O.

## Data model (SQLite)

IDs are random 12-char lowercase base32 strings. Times are RFC 3339 UTC text.

```sql
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);

CREATE TABLE users (
  github_id INTEGER PRIMARY KEY,
  login TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL DEFAULT '',
  avatar_url TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE TABLE sessions (
  token_hash TEXT PRIMARY KEY,           -- sha256 hex of the cookie token
  github_id INTEGER NOT NULL REFERENCES users(github_id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL
);

CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL
);

CREATE TABLE services (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name TEXT NOT NULL,                    -- DNS label; also the private hostname
  kind TEXT NOT NULL,                    -- app | postgres | mysql | mongo | redis
  repo TEXT NOT NULL DEFAULT '',         -- owner/name
  branch TEXT NOT NULL DEFAULT '',
  root_dir TEXT NOT NULL DEFAULT '',
  image TEXT NOT NULL DEFAULT '',        -- image source (non-repo apps, databases)
  dockerfile_path TEXT NOT NULL DEFAULT '', -- '' = auto (Dockerfile if present, else Railpack)
  start_command TEXT NOT NULL DEFAULT '',
  port INTEGER NOT NULL DEFAULT 0,       -- container port; injected as PORT for apps
  healthcheck_path TEXT NOT NULL DEFAULT '',
  public_port INTEGER NOT NULL DEFAULT 0,-- host TCP port to publish (0 = none)
  auto_deploy INTEGER NOT NULL DEFAULT 1,
  wait_for_ci INTEGER NOT NULL DEFAULT 0,
  stopped INTEGER NOT NULL DEFAULT 0,    -- stopped by the user; see "Stopping a service"
  created_at TEXT NOT NULL,
  UNIQUE (project_id, name)
);

CREATE TABLE variables (
  service_id TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  PRIMARY KEY (service_id, key)
);

CREATE TABLE volumes (
  id TEXT PRIMARY KEY,
  service_id TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
  mount_path TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (service_id, mount_path)
);                                        -- docker volume name: shed-vol-<id>

CREATE TABLE domains (
  id TEXT PRIMARY KEY,
  service_id TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
  host TEXT NOT NULL UNIQUE,
  generated INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);

CREATE TABLE deployments (
  id TEXT PRIMARY KEY,
  service_id TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
  status TEXT NOT NULL,      -- see statuses below
  trigger TEXT NOT NULL,     -- push | manual | redeploy | create
  commit_sha TEXT NOT NULL DEFAULT '',
  commit_message TEXT NOT NULL DEFAULT '',
  commit_author TEXT NOT NULL DEFAULT '',
  image TEXT NOT NULL DEFAULT '',         -- built or pulled image ref
  container_id TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT
);
CREATE INDEX deployments_service ON deployments(service_id, created_at DESC);

CREATE TABLE metric_samples (            -- one row per service per sampling tick
  service_id TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
  ts INTEGER NOT NULL,                   -- unix seconds
  cpu REAL NOT NULL,                     -- percent of one core
  memory INTEGER NOT NULL,               -- bytes
  net_rx REAL NOT NULL,                  -- bytes/s
  net_tx REAL NOT NULL,
  disk_read REAL NOT NULL,
  disk_write REAL NOT NULL,
  PRIMARY KEY (service_id, ts)
) WITHOUT ROWID;
```

Deployment statuses: `queued`, `waiting` (for CI), `building`, `deploying`,
`active`, `failed`, `crashed`, `removed` (superseded), `canceled`, `skipped`
(CI failed).

## Runtime model

- Docker network per project: `shed-<projectID>`. Each container joins with
  network alias `<service name>`, so services reach each other at
  `<name>:<port>` (the "private host").
- Container name: `shed-<serviceID>-<deploymentID>`. Labels:
  `shed.project`, `shed.service`, `shed.deployment`.
  Restart policy `unless-stopped`.
- Volumes: Docker named volume `shed-vol-<volumeID>`, mounted at `mount_path`.
  Database services get one automatically.
- Images: `shed/<serviceID>:<deploymentID>`. Keep the 5 most recent per service.
- Build logs: `<data>/logs/<deploymentID>.log`. Runtime logs: Docker logs of
  the active container.
- Builds run in `<data>/builds/<deploymentID>` and are deleted afterwards.
  Workspace paths are made absolute before running build commands, so relative
  data directories work with Dockerfile and Railpack builds.

### Variables

Users set plain key/values. Values may contain references, resolved at deploy
time by `internal/vars`:

- `${{ KEY }}` — another variable of the same service.
- `${{ service.KEY }}` — a variable of another service in the same project.

shed injects these into every service (also referenceable):
`PORT` (apps, if port > 0), `SHED_PROJECT_NAME`, `SHED_SERVICE_NAME`,
`SHED_PRIVATE_DOMAIN` (= service name), `SHED_PUBLIC_DOMAIN` (first domain,
if any), `SHED_GIT_COMMIT_SHA`, `SHED_GIT_BRANCH`.

Database templates create variables on service creation, e.g. postgres:
`POSTGRES_USER=postgres`, `POSTGRES_PASSWORD=<random>`, `POSTGRES_DB=app`,
`DATABASE_URL=postgresql://${{POSTGRES_USER}}:${{POSTGRES_PASSWORD}}@${{SHED_PRIVATE_DOMAIN}}:5432/${{POSTGRES_DB}}`.
An app then sets `DATABASE_URL=${{ postgres.DATABASE_URL }}`.

### Deploy pipeline

One worker per service; a new deployment cancels older non-terminal ones for
the same service.

1. **Wait for CI** (apps with `wait_for_ci` and a commit): poll GitHub every
   10s until all check runs and statuses for the SHA complete. Any failure →
   `skipped`. Timeout 60 min → `failed`.
2. **Build**: repo apps clone the commit with an installation token and build
   `shed/<serviceID>:<deploymentID>`. Image apps and databases pull their
   image. Redeploys of an old deployment reuse its image and skip this step.
   Railpack writes its generated plan and info into the private build workspace,
   outside the repository-controlled source tree.
   Once the image is available, a service with `port` 0 gets the lowest TCP
   port the image exposes (`EXPOSE`) as its `port`, which is saved.
3. **Start**: resolve variables, create and start the new container.
   Services with volumes or a public port stop the old container first if it
   is running (volumes can't be shared safely, e.g. database data dirs); others
   overlap for zero downtime.
4. **Health**: if `port > 0`, wait up to 120s for a TCP connect, or a 2xx/3xx
   on `healthcheck_path` if set, at the container's IP on the project network.
   A service without a port is watched for 3s instead. Either way the
   deployment fails if the container exits meanwhile.
5. **Switch**: apply proxy routes to the healthy candidate, then mark the new
   deployment `active` and clear the service's `stopped` flag. Only after routing
   succeeds, stop/remove the previous container and mark its deployment `removed`.
6. On failure at any step: mark `failed`, record `error`, remove the new
   container, and restart the previous container if step 3 stopped it and
   removal of the replacement is confirmed.

The build log has a `==> ` heading per step with detail lines under it: the
container's name, image, network and private address, volumes, published
port, number of variables (never their values), and start command; what the
health check probes, a "Not ready yet" line every 5s, and how long it took;
which domains route to the new deployment and which deployment it replaces.
From start until the health check ends (or the 3s watch for services without
a port), the new container's own stdout/stderr is copied into the build log
without Docker's timestamps, so its boot output and last words before an exit
are visible.

On boot, `deploy.Reconcile` ensures the active deployment's container of every
service that is not stopped is running, marks orphaned in-progress deployments
`failed`, and applies routes.

### Stopping a service

`POST /api/services/{id}/stop` sets `stopped`, cancels any deployment in
progress (recorded as `canceled`, "service stopped"), stops the active
deployment's container without removing it, and removes the service's routes.
The active deployment stays `active`: it is what `start` runs again
(recreating its container if it is gone), and what a reboot leaves stopped.
`restart` restarts the active container of a running service. Any new
deployment may still run while stopped; when it goes live it clears `stopped`.

### Metrics

`internal/metrics` samples every running container labeled `shed.service`
every 10s with a one-shot Docker stats call. Rates come from the difference
to the same container's previous sample (a container's first sample, or one
after a counter reset, yields nothing): CPU is percent of one core, memory is
usage minus inactive page cache, network and block I/O are bytes/s. Containers
of the same service (overlap during a zero-downtime deploy) are summed into
one `metric_samples` row per tick. Samples older than 7 days are pruned hourly.

A query for range `1h`, `6h`, `24h`, or `7d` returns 180 buckets of width
range/180 (20s, 120s, 480s, 3360s), aligned to multiples of the width since
the Unix epoch. The last bucket is the one containing now if it has samples
yet; otherwise the window shifts back one step to end at the last complete
bucket, so the series never ends in a null just because the current bucket is
young. Each bucket is the average of its samples, or null if it has none. `cpuLimit` and `memoryLimit`
are the running container's configured limits (0 = unlimited, which is
always the case today since shed sets none).

### Proxy

Embedded Caddy (`caddy.Load` with a generated JSON config). Routes:
- dashboard: host of `server.url` → `server.listen`
- each domain → `<container IP>:<port>` of the service's active container.
Automatic HTTPS with `acme_email`. Generated domains are
`<service>-<project>.<base_domain>` (requires wildcard DNS).

## GitHub

### Setup (first run)

If no GitHub App is stored, shed logs a one-time setup token. The dashboard's
`/setup` page asks for it, then sends the browser to
`GET /api/setup/github?token=…`, which renders an auto-submitting form that
POSTs an app manifest to `https://github.com/settings/apps/new`:

- `url`: `server.url`
- `hook_attributes.url`: `<server.url>/api/github/webhook`
- `redirect_url`: `<server.url>/api/setup/github/callback`
- `callback_urls`: [`<server.url>/api/auth/callback`]
- `setup_url`: `<server.url>/`
- `public`: true (so the app can be installed on organizations, not just the owner's account)
- `default_permissions`: contents read, metadata read, checks read, statuses read
- `default_events`: push

The callback exchanges `code` via `POST /app-manifests/{code}/conversions`,
stores app ID, slug, client ID/secret, webhook secret, and private key in
`settings`, then redirects to `https://github.com/apps/<slug>/installations/new`.

### Sign-in

`/api/auth/login` → GitHub OAuth authorize (app client ID, random state cookie)
→ `/api/auth/callback` exchanges the code, fetches the user, and checks access:
allowed only if the login is in `auth.allowed_users` (case-insensitive). An
empty list denies all access. The middleware checks this rule on every request;
startup permanently revokes sessions for removed logins. Change the configuration
and restart shed to revoke a user, including all existing streams.
Session cookie `shed_session` (random 32 bytes, stored hashed), HttpOnly,
SameSite=Lax, Secure when `server.url` is https, 30 days.

### Webhooks

`POST /api/github/webhook`, HMAC-SHA256 verified. On `push` to
`refs/heads/<branch>`, every `app` service with matching `repo`, `branch`, and
`auto_deploy` gets a `push` deployment with the head commit's sha, message,
and author. Other events are acknowledged and ignored.

## HTTP API

JSON over `/api`, camelCase. Errors: `{"error": "message"}` with a proper
status. All routes except auth, setup, and webhook require a session.
Mutating browser requests must match the origin of `server.url`; sibling
application origins are rejected. POST, PUT, and PATCH require
`Content-Type: application/json`, even with an empty body. Logout has the same
protections. For Vite development, set `server.url` to the dashboard dev origin.
SSE endpoints emit `event: log` (one line per event), `event: status`
(deployment status changes, data `{"status":"…"}`), and `event: end`.
Reconnecting clients get history replayed. Runtime log lines start with the
container's RFC 3339 timestamp and a space. Runtime logs send `end` right away
when nothing is running. Setup failures redirect to `/setup?error=…`.

```
GET    /api/me                                  → User
GET    /api/auth/login                          302 → GitHub
GET    /api/auth/callback                       302 → /
POST   /api/auth/logout                         204

GET    /api/setup                               → Setup
GET    /api/setup/github?token=                 HTML auto-submit form
GET    /api/setup/github/callback?code=&state=  302 → GitHub install page

GET    /api/projects                            → Project[]
POST   /api/projects            {name}          → Project
GET    /api/projects/{id}                       → ProjectDetail
PATCH  /api/projects/{id}       {name}          → Project
DELETE /api/projects/{id}                       204  (tears down everything)

POST   /api/projects/{id}/services  NewService  → Service  (creates + first deploy)
GET    /api/services/{id}                       → Service
PATCH  /api/services/{id}       ServicePatch    → Service
DELETE /api/services/{id}                       204  (containers, volumes, images)
POST   /api/services/{id}/stop                  → Service  (cancel deploys, stop container, unroute)
POST   /api/services/{id}/start                 → Service  (409 if nothing was ever deployed)
POST   /api/services/{id}/restart               → Service  (409 if stopped or nothing deployed)

GET    /api/services/{id}/variables             → Record<string,string>
PUT    /api/services/{id}/variables  Record     → Record  (replace all)

POST   /api/services/{id}/domains   {host?}     → Domain  (no host = generate)
DELETE /api/domains/{id}                        204
POST   /api/services/{id}/volumes   {mountPath} → Volume
DELETE /api/volumes/{id}                        204  (removes data)

GET    /api/services/{id}/deployments           → Deployment[]  (newest first, 50)
POST   /api/services/{id}/deployments           → Deployment    (deploy branch head / image)
GET    /api/deployments/{id}                    → Deployment
POST   /api/deployments/{id}/redeploy           → Deployment    (reuse image = rollback)
POST   /api/deployments/{id}/cancel             → Deployment
GET    /api/deployments/{id}/logs               SSE build log (replays file, follows while building)
GET    /api/services/{id}/logs                  SSE runtime logs (tail 500, follow)
GET    /api/services/{id}/metrics?range=        → Metrics  (range: 1h | 6h | 24h | 7d; default 1h)

GET    /api/github/repos                        → Repo[]
GET    /api/github/repos/{owner}/{repo}/branches → string[]
POST   /api/github/webhook

GET    /*                                       SPA (web/dist, index.html fallback)
```

### Types

```ts
type User = { login: string; name: string; avatarUrl: string };
type Setup = { githubConfigured: boolean; appSlug: string; installUrl: string };

type ServiceKind = "app" | "postgres" | "mysql" | "mongo" | "redis";
type ServiceStatus = "offline" | "deploying" | "active" | "failed" | "crashed" | "stopped";
type DeploymentStatus =
  | "queued" | "waiting" | "building" | "deploying" | "active"
  | "failed" | "crashed" | "removed" | "canceled" | "skipped";

type Project = {
  id: string; name: string; createdAt: string;
  services: { id: string; name: string; kind: ServiceKind; status: ServiceStatus }[];
};
type ProjectDetail = Omit<Project, "services"> & { services: Service[] };

type Service = {
  id: string; projectId: string; name: string; kind: ServiceKind;
  repo: string; branch: string; rootDir: string; image: string;
  dockerfilePath: string; startCommand: string;
  port: number; healthcheckPath: string; publicPort: number;
  autoDeploy: boolean; waitForCi: boolean;
  status: ServiceStatus;
  privateHost: string;                 // "<name>"
  domains: Domain[];
  volumes: Volume[];
  latestDeployment: Deployment | null;
  createdAt: string;
};

// Create: kind "app" needs repo+branch or image; database kinds need only name.
type NewService = { name: string; kind: ServiceKind; repo?: string; branch?: string; image?: string };
// Patch: any subset of the editable Service fields (name excluded).
type ServicePatch = Partial<Pick<Service,
  "repo" | "branch" | "rootDir" | "image" | "dockerfilePath" | "startCommand" |
  "port" | "healthcheckPath" | "publicPort" | "autoDeploy" | "waitForCi">>;

type Deployment = {
  id: string; serviceId: string; status: DeploymentStatus;
  trigger: "push" | "manual" | "redeploy" | "create";
  commitSha: string; commitMessage: string; commitAuthor: string;
  image: string; error: string;
  createdAt: string; startedAt: string | null; finishedAt: string | null;
};

type Domain = { id: string; host: string; generated: boolean; url: string };
type Volume = { id: string; mountPath: string; createdAt: string };
type Repo = { fullName: string; defaultBranch: string; private: boolean };

// Container resource usage. Sample i is at start + i*step seconds; series
// are the same length, oldest first, null where nothing was running.
type Metrics = {
  range: "1h" | "6h" | "24h" | "7d";
  start: string; step: number;         // step in seconds
  cpuLimit: number;                    // cores, 0 = unlimited
  memoryLimit: number;                 // bytes, 0 = unlimited
  cpu: (number | null)[];              // percent of one core (200 = two cores busy)
  memory: (number | null)[];           // bytes in use
  netRx: (number | null)[];            // bytes/s received
  netTx: (number | null)[];            // bytes/s sent
  diskRead: (number | null)[];         // bytes/s
  diskWrite: (number | null)[];        // bytes/s
};
```

Service status is derived: latest deployment non-terminal → `deploying`;
`stopped` flag set → `stopped`; active deployment whose container is running
→ `active`, not running → `crashed`; latest deployment `failed` with nothing active → `failed`;
otherwise `offline`.

Settings are saved immediately; the dashboard tells the user to redeploy to
apply them.

### Build secret handling

Resolved service variables are supplied to Dockerfile and Railpack builds as
BuildKit secrets in private files outside the source context, never as implicit
Docker build arguments. Dockerfiles consume them explicitly with
`RUN --mount=type=secret,id=KEY` (at `/run/secrets/KEY`). Existing Dockerfiles
using `ARG KEY` for service variables must migrate to secret mounts. Railpack
preparation receives variable values through its process environment; host-tool
control variables such as `PATH`, `HOME`, `LD_*`, `GIT_*`, and `DOCKER_*` are
excluded from preparation but remain available as build secrets and at runtime.
Git clone credentials use a scoped HTTP authorization header in the child
process environment, rather than the command line or repository configuration.
Build logs mask literal service-variable and clone-credential values, including
credentials split across writes. A build can still intentionally embed or encode
secrets it receives; secret mounts do not make untrusted build scripts safe.

Container startup and runtime log streams mask literal resolved values of stored
service variables, including matches split across writes. Injected metadata
(ports and service names) is not treated as secret unless explicitly configured
as a service variable. Runtime redaction uses the current variable configuration;
changing variables cannot retroactively remove secrets from older saved logs.

### Replacement storage safety

A deployment that needs exclusive volumes or a published host port must confirm
that the previous container is stopped before starting its replacement. Docker
inspection and stop failures abort deployment and preserve the active container.

If removing a failed replacement cannot be confirmed, recovery leaves the
predecessor stopped rather than risking concurrent use of its persistent volume.

### Routing activation safety

The proxy must accept routes targeting a healthy candidate before that deployment
is recorded active or its predecessor is retired. Failed reloads fail the candidate,
remove its container, and retain the previous active deployment and route. Route
updates are serialized through activation to prevent stale route publication.

Once routing succeeds, activation and cleanup finish even if the deployment is
canceled concurrently, so a routed candidate is not removed mid-activation.

### Dashboard hostname reservation

Service domain creation rejects the hostname from `server.url` with HTTP
409, including generated names. Proxy configuration rejects duplicate normalized
hostnames, preserving the existing configuration instead of selecting a workload
route over the dashboard route.

### Deletion and deployment admission

Service and project deletion mark their targets as deleting under the deployment
queue lock before canceling workers. New deploys and redeploys targeting them are
rejected throughout teardown. Runtime start, stop, restart, and deletion are
serialized. Failed deletion clears the admission guard so deletion can be retried.
