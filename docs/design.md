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
- Scheduled, compressed, optionally encrypted backups of every service with a
  volume and of shed's own database, kept locally and in S3-compatible storage,
  with restore, download, and run-now from the dashboard.
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
lumberjack and also mirrored to stderr. Its last 1000 lines are also kept in
memory (`internal/logtail`), and `GET /api/logs` replays them and then follows
new lines; the dashboard shows them under Server → Logs.

```toml
[server]
listen = "127.0.0.1:3000"          # dashboard + API listener (Caddy fronts it)
url    = "https://shed.example.com" # public dashboard URL; used for GitHub callbacks

[data]
dir = "/var/lib/shed"              # shed.db, builds/, logs/, backups/

[proxy]
enabled     = true
http_port   = 80
https_port  = 443
acme_email  = ""
base_domain = ""                   # e.g. "apps.example.com" → generated domains

[auth]
allowed_users = []                 # Explicit GitHub logins; configure before first sign-in

[build]
memory_mb = 2048                   # BuildKit builder container memory (no swap); 0 = unlimited
cpus = 2                           # BuildKit builder container CPU quota in cores; 0 = unlimited
min_free_mb = 2048                 # refuse builds below this free space on data dir / Docker root, cancel running builds below half; 0 = off

[deployments]
log_max_mb = 10                    # per-deployment build log cap; 0 = unlimited
keep = 50                          # deployments kept per service; 0 = keep all

[log]
level = "info"
max_size_mb = 20
max_backups = 5
max_age_days = 30
```

GitHub App credentials and other runtime state live in the database
(`settings` table), not the config file. Sessions are rows of `sessions`.

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
| `internal/logtail` | in-memory tail of shed's own log, followed over SSE | — |
| `internal/host` | host CPU, memory, network, disk I/O, and filesystem usage from procfs/sysfs/statfs | — |
| `internal/s3` | S3-compatible object storage client (put, get, delete, check) | — |
| `internal/backup` | backup/restore of service data and shed.db: dumps, archives, zstd, age, schedule, retention, upload | interfaces only + store/docker types |
| `internal/deploy` | deployment pipeline, per-service queue, reconcile on boot | interfaces only + store/catalog/vars types |
| `internal/metrics` | container and host resource sampling, per-service and host time series | interfaces only + docker/host/store types |
| `internal/auth` | sessions, GitHub sign-in handlers, middleware | store via interface |
| `internal/api` | JSON HTTP API, SSE logs, webhook endpoint, SPA serving | deploy, auth, github, metrics, backup, store |
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
  cpu_limit REAL NOT NULL DEFAULT 1,     -- cores (0 = unlimited)
  memory_limit INTEGER NOT NULL DEFAULT 1073741824, -- bytes, no extra swap (0 = unlimited)
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
  port INTEGER NOT NULL DEFAULT 0,        -- service port it was started with; routes use it
  container_id TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  runtime TEXT NOT NULL DEFAULT '' -- JSON container config of the active deployment
);
CREATE INDEX deployments_service ON deployments(service_id, created_at DESC);
-- At most one active deployment per service.
CREATE UNIQUE INDEX deployments_one_active ON deployments(service_id) WHERE status = 'active';

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

CREATE TABLE host_samples (             -- one row per sampling tick
  ts INTEGER PRIMARY KEY,                -- unix seconds
  cpu REAL NOT NULL,                     -- percent of one core
  memory INTEGER NOT NULL,               -- bytes
  disk_used INTEGER NOT NULL,            -- bytes used on the data dir's filesystem
  net_rx REAL NOT NULL,                  -- bytes/s
  net_tx REAL NOT NULL,
  disk_read REAL NOT NULL,
  disk_write REAL NOT NULL
) WITHOUT ROWID;

CREATE TABLE backup_policies (           -- absent row = default policy
  service_id TEXT PRIMARY KEY REFERENCES services(id) ON DELETE CASCADE,
  enabled INTEGER NOT NULL,
  schedule TEXT NOT NULL,                -- cron expression, UTC unless CRON_TZ=
  compression TEXT NOT NULL,             -- fastest | default | better | best
  keep_local INTEGER NOT NULL,           -- scheduled backups kept on disk
  upload INTEGER NOT NULL,               -- also store in S3 (when configured)
  keep_remote INTEGER NOT NULL           -- scheduled backups kept in S3
);

CREATE TABLE backups (
  id TEXT PRIMARY KEY,
  service_id TEXT REFERENCES services(id) ON DELETE CASCADE, -- NULL = shed.db
  trigger TEXT NOT NULL,                 -- schedule | manual | pre-restore
  method TEXT NOT NULL,                  -- dump | volume | sqlite
  status TEXT NOT NULL,                  -- queued | running | uploading | succeeded | failed
  file TEXT NOT NULL DEFAULT '',         -- archive file name, e.g. <id>.sql.zst.age
  size INTEGER NOT NULL DEFAULT 0,       -- archive bytes
  encrypted INTEGER NOT NULL DEFAULT 0,
  local INTEGER NOT NULL DEFAULT 0,      -- archive present under <data>/backups
  remote_key TEXT NOT NULL DEFAULT '',   -- S3 object key; '' = not uploaded
  destination_id TEXT REFERENCES backup_destinations(id), -- where remote_key is; NULL = not uploaded
  remote_error TEXT NOT NULL DEFAULT '', -- last upload failure
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT
);
CREATE INDEX backups_service ON backups(service_id, created_at DESC);

CREATE TABLE backup_destinations (       -- S3 locations backups were uploaded to; never deleted
  id TEXT PRIMARY KEY,
  endpoint TEXT NOT NULL,
  region TEXT NOT NULL,
  bucket TEXT NOT NULL,
  prefix TEXT NOT NULL,                  -- without surrounding slashes
  path_style INTEGER NOT NULL,
  access_key_id TEXT NOT NULL,           -- credentials may be replaced
  secret_access_key TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (endpoint, region, bucket, prefix, path_style)
);

CREATE TABLE restores (
  id TEXT PRIMARY KEY,
  service_id TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
  backup_id TEXT NOT NULL,               -- not a foreign key: the backup may be pruned later
  status TEXT NOT NULL,                  -- running | succeeded | failed
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  finished_at TEXT
);
CREATE INDEX restores_service ON restores(service_id, created_at DESC);

CREATE TABLE restore_fences (            -- a restore is changing the service's data
  service_id TEXT PRIMARY KEY REFERENCES services(id) ON DELETE CASCADE,
  restore_id TEXT NOT NULL,
  phase TEXT NOT NULL,                   -- retaining | replacing | loading
  image TEXT NOT NULL,                   -- image of the helper containers
  volume_ids TEXT NOT NULL,              -- space-separated IDs of the volumes being replaced
  was_stopped INTEGER NOT NULL,          -- services.stopped before the restore
  created_at TEXT NOT NULL
);

CREATE TABLE pending_pushes (             -- newest undeployed push per app service
  service_id TEXT PRIMARY KEY REFERENCES services(id) ON DELETE CASCADE,
  id TEXT NOT NULL,                       -- changes with every newer push
  repo TEXT NOT NULL,
  branch TEXT NOT NULL,
  commit_sha TEXT NOT NULL,
  commit_message TEXT NOT NULL DEFAULT '',
  commit_author TEXT NOT NULL DEFAULT '',
  received_at TEXT NOT NULL,
  prior_deployment_id TEXT NOT NULL DEFAULT '' -- latest deployment when stored
);
```

Deployment statuses: `queued`, `waiting` (for CI), `building`, `deploying`,
`active`, `failed`, `crashed`, `removed` (superseded), `canceled`, `skipped`
(CI failed).

## Runtime model

- Docker network per project: `shed-<projectID>`. The active deployment's
  container has network alias `<service name>`, so services reach each other
  at `<name>:<port>` (the "private host"). A new deployment's container
  starts with no alias (only its container name resolves); once healthy, it
  is disconnected and reconnected with the alias, requesting the address it
  had so that it does not change, and must pass its health check again; on
  activation the previous container is disconnected before it stops.
- Container name: `shed-<serviceID>-<deploymentID>`. Labels:
  `shed.project`, `shed.service`, `shed.deployment`.
  Restart policy `unless-stopped`.
- Volumes: Docker named volume `shed-vol-<volumeID>`, mounted at `mount_path`.
  Database services get one automatically.
- Images: `shed/<serviceID>:<deploymentID>`. Keep the 5 most recent per service.
- Build logs: `<data>/logs/<deploymentID>.log`, capped at
  `deployments.log_max_mb`; output beyond the cap is replaced by a final
  `==> Log truncated` line. Runtime logs: Docker logs of the active container.
- Deployment history: after each deployment ends, a service's finished
  deployments (`failed`, `removed`, `canceled`, `skipped`) beyond its newest
  `deployments.keep` are deleted with their build logs. Active, crashed, and
  in-progress deployments are never deleted.
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
   `skipped`. No checks is not approval: keep waiting until checks appear
   and pass. Timeout 60 min → `failed`.
2. **Build**: repo apps clone the commit with an installation token and build
   `shed/<serviceID>:<deploymentID>`. Image apps and databases pull their
   image and record its immutable local image ID. Containers and rollbacks use
   that ID even after the configured tag moves. Redeploys of old deployments
   reuse the recorded image and skip this step. The image must still be on the
   host: redeploy checks before queueing (409 if unavailable), and the pipeline
   checks again before stopping the previous container. Legacy tag-only records for
   pulled images cannot be redeployed safely and must be deployed afresh.
   Railpack writes its generated plan and info into the private build workspace,
   outside the repository-controlled source tree.
   Once the image is available, a service with `port` 0 gets the lowest TCP
   port the image exposes (`EXPOSE`) as its `port`, which is saved.
3. **Start**: resolve variables, record the service's port and container
   configuration on the deployment (persisted on activation), and create and start the new container, without the service's network
   alias. Services with volumes or a public port stop the old container first
   if it is running (volumes can't be shared safely, e.g. database data dirs);
   others overlap for zero downtime.
4. **Health**: if `port > 0`, wait up to 120s for a TCP connect, or a 2xx/3xx
   on `healthcheck_path` if set, at the container's IP on the project network.
   A service without a port is watched for 3s instead. Either way the
   deployment fails if the container exits meanwhile.
5. **Switch**: give the healthy candidate the service's network alias
   (reconnecting it with its existing IPv4/IPv6 address reserved), then probe it
   again as in step 4 for up to 10s (or, without a port, check it is still
   running); if that fails, the deployment fails and the previous container
   keeps the alias and routes. Then apply proxy routes to it and, in one
   transaction, mark the new deployment `active` and the previous one
   `removed`; then clear the service's `stopped` flag. Only after routing succeeds, disconnect the previous container from
   the network and stop/remove it.
6. On failure at any step: mark `failed`, record `error`, remove the new
   container, and restart the previous container if step 3 stopped it and
   every other container of the service is confirmed removed (by label, so
   a replacement whose start failed ambiguously is found even without its
   ID).

The build log has a `==> ` heading per step with detail lines under it: the
container's name, image, network and private address, volumes, published
port, number of variables (never their values), and start command; what the
health check probes, a "Not ready yet" line every 5s, and how long it took;
which domains route to the new deployment and which deployment it replaces.
From start until the health check ends (or the 3s watch for services without
a port), the new container's own stdout/stderr is copied into the build log
without Docker's timestamps, so its boot output and last words before an exit
are visible.

On boot, `deploy.Reconcile` marks orphaned in-progress deployments `failed`,
ensures the active deployment's container of every service that is not
stopped is running, and applies routes. It restores one deployment per
service, the newest active one, and removes the service's other containers,
such as a predecessor left behind by a crash during a switchover. A missing
container is recreated only if its image and deployed volumes (except ones
deleted in shed) are on the host; otherwise no network,
volume, or container is created, the error is logged, and the service shows
`crashed`.

The active deployment's `runtime` stores its command, resolved variables,
masked variable keys, CPU/memory limits, public port, and volume IDs and mount
paths. Activation writes it and clears the superseded deployment's runtime
in the same transaction, so resolved variables remain only on the active record.
A missing container is recreated from this snapshot, including during boot,
start, or release of a hold. Saved settings wait for a new deployment. Volumes
deleted since are not mounted and their Docker volumes are removed; newly added
volumes wait for deployment. Deployments activated before migration 011 have no
snapshot and fall back to current settings with a warning when recreated.

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
one `metric_samples` row per tick.

On the same tick it reads the host through `internal/host` and stores one
`host_samples` row, with rates from the difference to the previous reading:
CPU is busy time (all but idle and iowait in `/proc/stat`) in percent of one
core, memory is `MemTotal - MemAvailable`, network and disk I/O are bytes/s
summed over physical devices only (those with a `device` link in sysfs, which
leaves out Docker bridges, veth pairs, loop and device-mapper devices), and
disk used is the used space of the filesystem holding `data.dir`.

Samples older than 7 days are pruned hourly.

A query for range `1h`, `6h`, `24h`, or `7d` returns 180 buckets of width
range/180 (20s, 120s, 480s, 3360s), aligned to multiples of the width since
the Unix epoch. The last bucket is the one containing now if it has samples
yet; otherwise the window shifts back one step to end at the last complete
bucket, so the series never ends in a null just because the current bucket is
young. Each bucket is the average of its samples, or null if it has none. `cpuLimit` and `memoryLimit`
are the running container's configured limits (0 = unlimited).

Host queries bucket the same way; `cpus`, `memoryTotal`, and `diskTotal`
are read live at query time.

### Backups

`internal/backup` backs up every service that has at least one volume, and
shed's own database. A backup is one archive file: data → zstd → optionally
age. Archives are standard formats, so they can be recovered by hand with
`age -d -i key.txt | zstd -d`, `tar`, `psql`, etc.

**Methods.** What gets archived depends on the service and its state:

| Service | Container running | Method | Archive | File |
|---|---|---|---|---|
| postgres | yes | `dump` | `pg_dumpall --clean --if-exists` (plain SQL) | `<id>.sql.zst` |
| mysql | yes | `dump` | `mysqldump --all-databases --single-transaction --routines --events --triggers --set-gtid-purged=OFF` | `<id>.sql.zst` |
| mongo | yes | `dump` | `mongodump --archive` (uncompressed; zstd compresses better) | `<id>.archive.zst` |
| redis | yes | `dump` | `BGSAVE`, wait for it to finish, then the RDB file | `<id>.rdb.zst` |
| database | no | `volume` | tar of its volumes (consistent because the server is stopped) | `<id>.tar.zst` |
| app | either | `volume` | tar of its volumes, read live | `<id>.tar.zst` |
| shed.db | — | `sqlite` | `VACUUM INTO` snapshot | `<id>.db.zst` |

Encrypted archives get a `.age` suffix. The method is chosen when a backup
is queued and chosen again when it runs; if the service's state changed in
between, the row's method is updated. Dumps run with `docker exec` in the
active container, through `sh -c`, so credentials come from the container's
own environment (`POSTGRES_USER`/`POSTGRES_PASSWORD`, `MYSQL_ROOT_PASSWORD`,
`MONGO_INITDB_ROOT_*`, `REDIS_PASSWORD`). Passwords never appear on a
command line: they are passed as `PGPASSWORD`, `MYSQL_PWD`, and
`REDISCLI_AUTH`, and to the mongo tools through a private `--config` file. The
mysql restore sets `lock_wait_timeout=300` for its session, so a restore blocked
by a metadata lock from an application transaction fails after 5 minutes instead
of hanging. A failed command's error ends with the last 4 KiB of its stderr.
The redis dump waits until no background save is running, starts `BGSAVE SCHEDULE`, waits
until `rdb_saves` has advanced and no save is in progress, checks
`rdb_last_bgsave_status:ok`, and streams `/data/dump.rdb` (redis 7 or newer).

Volume archives are read with the Docker archive API from a helper container:
it is created but never started, uses the active deployment's image, mounts
the service's volumes read-only, is named `shed-backup-<backupID>`, and is
labeled `shed.backup=<backupID>`. Removing a helper also removes the
anonymous volumes Docker created for its image's other `VOLUME` paths (for
example mongo's `/data/configdb`); `docker.Client.Remove` always removes a
container's anonymous volumes, never its named ones. Tar entries are rooted
at each volume's mount path relative to `/` (for example `var/lib/data/...`), and ownership,
modes, and extended attributes are kept. A volume nested inside another's
mount path is archived only once, from its own volume. A volume backup of a
stopped database holds the service (see Restore) so that nothing starts it
mid-archive. App volumes are archived without a hold, while the app runs. A
service with volumes but no active deployment cannot be backed up yet:
`POST` returns 400, and scheduled runs skip it.

**Compression.** zstd (`klauspost/compress`), streaming with up to four
goroutines. The policy's `compression` maps to the encoder levels `fastest`,
`default`, `better`, and `best`; `best` uses a 16 MiB window, which the
`zstd` CLI decodes without extra flags. Encoding stays under about 100 MiB of
memory. The default is `best`, because backups are written once and kept for
a long time.

**Encryption.** Encryption is a global setting (`backup.encrypt`, `true` or
`false`). When it is first enabled, shed generates an age X25519 identity and
stores it in `settings` (`backup.age_identity`) with an insert that does
nothing if the key exists, then reads the stored one back, so concurrent
saves agree on one identity and a stored identity is never replaced. While encryption is on,
every new archive, local and remote, is encrypted to that identity's
recipient. Turning encryption off keeps the identity. Older archives keep
their own `encrypted` flag, and shed decrypts them with the stored identity.
The dashboard reveals the identity so the user can store it off the server.
Without it, encrypted backups, including those of shed.db, cannot be
recovered after the server is lost.

**S3.** Destinations are rows of `backup_destinations` (endpoint URL,
region, bucket, prefix, path-style, access key ID, secret). The current one
is named by `settings` `backup.destination` (its ID; absent or empty means
none). Saving the settings stores the destination at that location
(endpoint, region, bucket, prefix, path-style): an existing row there keeps
its ID and takes the new credentials, and any other location gets a new row.
A row's location never changes and rows are never deleted. Each uploaded
backup records its `destination_id`, and downloads, restores, deletes, and
pruning of its object use that destination, not the current one, so
changing or removing the destination never points old backups elsewhere.
Migration 010 turned the former `backup.s3` JSON setting into the first row
and pointed every uploaded backup at it. `internal/s3` wraps minio-go and uses multipart
upload for large objects. Object keys are `<prefix>/services/<serviceID>/<file>`
and `<prefix>/system/<file>`; the prefix is stored without surrounding
slashes and omitted when empty. The API never returns the secret. Saving or
testing a destination with an empty secret uses the stored one. Testing
writes, reads, and deletes `<prefix>/.shed-check-<random>`.

**Storage and flow.** A backup row starts `queued`. One backup or restore
runs at a time across shed, and the others wait in order. A service may have
only one job queued or running, except that a restore may be queued behind
its backup; conflicting requests return 409. While running, the archive is written to
`<data>/backups/<serviceID or "system">/<file>.partial`, synced, and then
renamed, and `local` is set. If the policy uploads and S3 is configured, the
backup is then `uploading` while the local file is uploaded; it is still the
service's running job, so it cannot be deleted and another backup cannot
start. Once the upload's outcome is recorded, or right away when there is
nothing to upload, the backup is `succeeded` and `finished_at` is set. Upload
failure still ends `succeeded` but records `remote_error`.
Before uploading, the intended `destination_id` and `remote_key` are
recorded on the `uploading` row. After a successful upload, the row is
committed `succeeded` with that remote location first; only then is the local
file removed, and `local = false` recorded in a separate update. A failure
removing the file or recording it leaves a valid backup. A failed upload
clears the intended location.
`keep_local = 0` (allowed only when uploading) deletes the local file only
after a successful upload: without S3, or when the upload fails, the local
file is kept, so a backup is never left without a copy. Removing the S3
destination is therefore allowed while a policy has `keep_local = 0`. After
every job, failed backups older than 30 days are deleted.

**Schedule.** One loop wakes every minute. It evaluates each service with
volumes (its stored policy, or the default) and the system policy.
Schedules are standard 5-field cron or descriptors such as `@daily`, in UTC
unless prefixed with `CRON_TZ=<zone>`, and they are parsed with robfig/cron.
Next-run times are kept in memory and computed from boot time (or from when
the policy was saved), so runs missed while shed was down are skipped. A run
that finds the target busy is skipped too. The default service policy is
enabled, `0 3 * * *`, `best`, keep 7 local, upload, keep 30 remote. The
system policy lives in `settings` (`backup.system`, JSON) with the same
fields and defaults. Policies are validated: a parsable schedule, a known
compression, no negative counts, `keep_local ≥ 1` unless uploading, and
`keep_remote ≥ 1` when uploading.

**Retention.** After each scheduled backup, the newest `keep_local`
succeeded scheduled backups with a local file keep it, and older ones lose it;
with `keep_local = 0`, only backups that are in S3 lose their local file.
While the policy uploads, `keep_remote` works the same way for S3 objects;
with upload off, S3 objects are left alone. A pruned backup with neither a
local file nor a remote object is deleted. Manual and pre-restore backups are
never pruned automatically.

**Restore.** `POST /api/backups/{id}/restore` creates a `restores` row
(`running` while it waits in the queue) and runs in the background. Restores
apply to successful service backups only. To restore shed.db, follow the
manual steps below.

1. Take a `pre-restore` backup of the current state, using the policy's
   compression and upload settings. It runs inside the restore's job. If it
   fails, the restore fails.
2. Open the archive, local or downloaded from S3 into a temp file
   (`<restoreID>.download.partial`), and decode all of it to check that it
   decrypts and decompresses. Volume archives are also checked for unsafe
   entries: absolute names, names leaving the root, entries beneath a
   symlink of the archive, and hard links leaving their volume reject the
   whole archive.
3. Hold the service through `deploy` for the rest of the restore. Deployments
   in progress are canceled. Deploy, redeploy, start, stop, restart, delete,
   and volume deletion are rejected with `ErrServiceBusy` (409).
4. `volume`, and redis `dump`: the volumes being replaced are the service's
   volumes whose mount path appears in the archive (redis: the `/data`
   volume). Archive paths that are not a volume of the service, and volumes
   absent from the archive, are left alone; the former are logged. A volume's
   data is never removed before a checked copy of it exists:
   1. Fence the service: in one transaction, insert a `restore_fences` row
      (phase `retaining`) and set `services.stopped`, remembering its old
      value. Nothing starts a stopped service, including `Reconcile` after a
      restart, so the fence holds across restarts. While the row exists,
      deploys, redeploys, start, restart, and container recreation are
      refused too (see "Restore fences" below).
   2. Stop and remove the active container (the deployment stays `active`).
   3. Copy each volume to a fresh `shed-vol-<volumeID>-pre-restore` volume
      and check the copy, then set the phase to `replacing`.
   4. Empty each volume (remove and recreate it), extract the archive into it
      at `/`, and check it.
   5. Lift the fence: restore `services.stopped` and delete the row in one
      transaction, then remove the pre-restore volumes.

   Copies and extractions stream a tar through the Docker archive API between
   helper containers labeled `shed.backup=<restoreID>`: `shed-restore-<id>`
   mounts the service's volumes writable for the extraction, and
   `shed-restore-<id>-src` (read-only) and `shed-restore-<id>-dst` mount the
   source and target of a copy at the service's mount paths. A check reads
   the target back and compares it with the tar that was written: every
   entry must be there with the same type, symlink target, and, for files,
   size and SHA-256. Hard links compare as the file they link to. Extra
   entries are allowed, since Docker may fill an empty volume with what the
   image has at the mount path.

   If stopping or copying aside fails, the volumes are unchanged: the fence
   is lifted and the service starts again. If emptying, extracting, or the
   check fails, the pre-restore copies are put back (empty, copy, check) and
   the fence is lifted; the error says so. If putting back fails too, the
   fence and the pre-restore volumes are kept, so the service stays stopped
   and fenced, and the error names the volumes holding the previous data. If the restore
   is canceled while replacing (shutdown), the fence is kept and the next
   start puts the data back.

   A redis RDB goes to `/data/dump.rdb`, and also, as a hard link, to
   `/data/appendonlydir/appendonly.aof.1.base.rdb` with a manifest naming it
   as the base of a fresh multi-part AOF: a server with `appendonly yes`
   loads only the AOF and would otherwise start empty.
5. postgres, mysql, and mongo `dump`: the active container must be running.
   Stream the decoded dump into `psql` / `mysql` / `mongorestore --archive
   --drop` with `docker exec`. psql runs with `ON_ERROR_STOP=1`. Before the
   dump it disallows connections to every other database and terminates
   other sessions, because `DROP DATABASE` fails while an app is connected
   and the rest of the dump would then be mixed into the old data. The
   dump's `DROP ROLE` and `CREATE ROLE` of the connected user, which always
   fail, are filtered out. Connections are allowed again afterwards, also
   when the load fails. A restore replaces the databases rather than
   merging the dump into them: the dumps drop and recreate only what they
   contain, so first every database is dropped except postgres's `postgres`
   and templates (`DROP DATABASE … WITH (FORCE)`, postgres 13 or newer),
   mysql's `mysql`, `sys`, and schema views, and mongo's `admin`, `config`,
   and `local`. mysql drops run in the `mysql` session that loads the dump,
   with foreign key checks off, and the client stops at the first error;
   mongo drops run in `mongosh`, which reads the credentials from the
   environment. Objects in those kept databases and roles or users created
   after the backup are kept. The service is fenced (phase `loading`) for the
   load and the fence is lifted when it succeeds. If the load fails or is
   canceled (shutdown), the database may hold part of the dump, and
   canceling `docker exec` only aborts the stream: the command keeps
   running in the container. So before the hold is released, the service's
   containers are stopped and removed, which ends the load, and the service
   is left stopped; the fence row is deleted and the error says to restore
   a backup again or start the service to keep the data. If stopping fails,
   the fence stays and the next boot stops the container.
6. Release the hold. Unless the service is stopped, by the user or by a
   fence a failure left in place, the active deployment's container is
   started again (recreated if removed) and routes are applied. If that
   fails after the data was restored, the restore is recorded as failed with
   that error.

**Boot, shutdown, and deletion.** On boot, before the deployer reconciles,
`queued` and `running` backups and `running` restores are marked `failed`
("interrupted by restart"), and `uploading` backups are settled by what survives. With the local file
present, the backup is `succeeded` and local, with that message as its
`remote_error` and the intended remote location cleared. Without it, the
backup is remote-only (`succeeded`, `local = false`) if the object at its
recorded `remote_key` can be read, and `failed` otherwise. A succeeded backup
therefore always has a local file or a remote object. A backup that shutdown interrupts while `uploading` likewise ends `succeeded`
with the upload error. Leftover `.partial` files (archives, snapshots,
downloads) and `shed.backup` helper containers are removed. Then each
restore fence is resolved: in phase `retaining` the volumes are unchanged,
so the fence is lifted and the pre-restore volumes are removed; in phase
`replacing` the pre-restore copies are put back and checked first. If that
fails, the error is logged and the fence stays, so `Reconcile` leaves the
service stopped and the next boot tries again. In phase `loading` the active
container is stopped, since the load may still be running in it, and the
fence row is deleted; the service stays stopped. A fenced service that is no
longer stopped (only a shed that did not enforce fences could start it) has
its active container stopped and `stopped` set again; its fence, data, and
pre-restore volumes are kept and the error is logged. Recovery never drops a
fence because the service looks started. On shutdown the running job is canceled and it and
the queued ones are marked `failed` ("interrupted by shutdown"). Deleting a
service or project first pauses its services in the backup manager: running
backups and queued jobs are canceled ("canceled: service is being deleted"),
their helper containers are removed, and new jobs get `ErrBusy` until the delete
ends; a running restore makes the delete fail with 409. After a
service is deleted, any remaining jobs are canceled ("service deleted") and
`<data>/backups/<serviceID>` is removed. Its rows go with the
service. S3 objects are kept as the off-site copy, and the user can remove
them by hand. Deleting a single backup removes its local file and its S3
object from the destination it was uploaded to.

**Restore fences.** A `restore_fences` row is authoritative: while it exists,
`Deploy` and `Redeploy` (pushes, manual deploys, rollbacks), `StartService`,
`RestartService`, and container recreation (`Reconcile`, the release of a
hold) refuse the service with `deploy.ErrFenced`, and a new restore into it
with `backup.ErrFenced`; the API answers 409. Stopping and deleting the
service still work. A push for a fenced service is stored pending and the webhook answers
202. It deploys once the fence is cleared unless superseded meanwhile. The fence goes away only when its restore finishes, when boot
recovery puts the previous data back (or, in phase `loading`, stops the
container), or when the user clears it with
`POST /api/services/{id}/restore-fence/clear`. Clearing deletes the row and
nothing else: the service stays stopped with the data it has now, and the
pre-restore volumes, if any, are left for the user to inspect or remove by
hand (the next restore of the service overwrites them). Clearing is refused
with `ErrServiceBusy` (409) while the service is held, so the fence of a
restore in progress cannot be cleared. Clearing a service without a fence
does nothing. The service API exposes the fence as `restoreFence`, and the
dashboard shows it with a confirmed "Keep current data" action.

**Downloads.** A download is the archive decrypted but still compressed,
read from the local file or else from S3, named
`<service name or "shed">-<YYYYMMDD-HHMMSS>.<ext>.zst` from its creation
time in UTC.

**Recovering shed.db.** Stop shed, then fetch the newest
`<prefix>/system/*.db.zst[.age]`. Run `age -d -i key.txt` if it is encrypted,
then `zstd -d -o /var/lib/shed/shed.db`, and start shed.

### Proxy

Embedded Caddy (`caddy.Load` with a generated JSON config). Routes:
- dashboard: host of `server.url` → `server.listen`
- each domain → `<container IP>:<port>` of the service's active container,
  where `port` is the one recorded on the active deployment. Editing a
  service's port takes effect with its next deployment; until then routes,
  and a container recreated for the active deployment, keep the old port.
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

To reuse an App that already exists (for example after losing the database),
the setup page instead posts the token and credentials the user generated in
the App's GitHub settings to `POST /api/setup/github/import`. shed signs an
App JWT with the key and calls `GET /app` to confirm the ID, key, and client
ID belong together and to learn the slug, then stores the credentials the
same way. GitHub has no API to check the client or webhook secret. The App's
webhook and callback URLs must already point at `server.url`.

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
`auto_deploy` gets the push stored as its pending push. A newer push replaces
an older undeployed one. shed then tries to deploy it with the head commit's
sha, message, and author, and removes it once the deployment is created.
Held or fenced services keep the push; the webhook still answers 202. A storage
failure answers 503. `ReplayPushes` retries at startup after reconcile and
every 10 seconds, logging each failure once per push and reason.

A pending push is dropped if the service is deleted, no longer deploys that
repo and branch on push, or its latest deployment differs from the recorded
`prior_deployment_id`. This comparison uses IDs, not timestamps. Storing a push,
checking and enqueueing it, and the API's other deployment requests share a lock,
so newer pushes and manual deployments cannot interleave with that check.
If deleting an enqueued push fails, its own deployment supersedes it on retry.
Other events are acknowledged and ignored. GitHub does not automatically
redeliver failures; pushes sent while shed is unreachable must be redelivered
from GitHub or deployed manually.

## HTTP API

JSON over `/api`, camelCase. Errors: `{"error": "message"}` with a proper
status. All routes except auth, setup, and webhook require a session.
Mutating browser requests must match the origin of `server.url`; sibling
application origins are rejected. POST, PUT, and PATCH require
`Content-Type: application/json`, even with an empty body. Logout and the
setup import have the same protections. For Vite development, set `server.url`
to the dashboard dev origin.
Every response from shed's handler (API and dashboard) carries
`Content-Security-Policy: frame-ancestors 'none'`, `X-Frame-Options: DENY`,
`X-Content-Type-Options: nosniff`, and `Referrer-Policy: same-origin`, so the
dashboard cannot be framed by another site.
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
POST   /api/setup/github/import  ImportApp     → Setup

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
POST   /api/services/{id}/start                 → Service  (409 if never deployed, fenced, or recreation lacks its image or volumes)
POST   /api/services/{id}/restart               → Service  (409 if stopped, nothing deployed, or fenced)
POST   /api/services/{id}/restore-fence/clear   → Service  (drop a failed restore's fence, keep data; 409 while held)

GET    /api/services/{id}/variables             → Record<string,string>
PUT    /api/services/{id}/variables  Record     → Record  (replace all)

POST   /api/services/{id}/domains   {host?}     → Domain  (no host = generate; Shed-Routes: pending if route application fails)
DELETE /api/domains/{id}                        204 (Shed-Routes: pending if route application fails)
POST   /api/services/{id}/volumes   {mountPath} → Volume
DELETE /api/volumes/{id}                        204  (removes data)

GET    /api/services/{id}/deployments           → Deployment[]  (newest first, 50; imageAvailable when Docker is reachable)
POST   /api/services/{id}/deployments           → Deployment    (deploy branch head / image; 409 if fenced)
GET    /api/deployments/{id}                    → Deployment
POST   /api/deployments/{id}/redeploy           → Deployment    (reuse image = rollback; 409 if fenced or image unavailable)
POST   /api/deployments/{id}/cancel             → Deployment
GET    /api/deployments/{id}/logs               SSE build log (replays file, follows while building)
GET    /api/services/{id}/logs                  SSE runtime logs (tail 500, follow)
GET    /api/services/{id}/metrics?range=        → Metrics  (range: 1h | 6h | 24h | 7d; default 1h)
GET    /api/host/metrics?range=                 → HostMetrics  (same ranges)
GET    /api/logs                                SSE shed's own log (last 1000 lines, follow)

GET    /api/services/{id}/backups               → ServiceBackups  (newest first, 100)
PUT    /api/services/{id}/backups/policy  BackupPolicyInput → BackupPolicy
POST   /api/services/{id}/backups               202 → Backup  (run now; 409 if one is queued/running/uploading; 400 if no volumes or never deployed)
GET    /api/backups/system                      → SystemBackups
PUT    /api/backups/system/policy  BackupPolicyInput → BackupPolicy
POST   /api/backups/system                      202 → Backup
GET    /api/backups/{id}/download               archive, decrypted, still zstd-compressed (Content-Disposition)
POST   /api/backups/{id}/restore                202 → Restore  (409 if busy or fenced; 400 for shed.db, unsuccessful, or vanished backups)
DELETE /api/backups/{id}                        204  (local file and S3 object; 409 while queued/running/uploading or being restored)
GET    /api/backups/settings                    → BackupSettings
PUT    /api/backups/settings  BackupSettingsInput → BackupSettings
POST   /api/backups/settings/test  BackupSettingsInput → 204  (400 {error} with the S3 failure; blank secret = stored)
GET    /api/backups/settings/key                → { identity: string }  (age secret key; 404 if none)

GET    /api/github/repos                        → Repo[]
GET    /api/github/repos/{owner}/{repo}/branches → string[]
POST   /api/github/webhook

GET    /*                                       SPA (web/dist, index.html fallback)
```

### Types

```ts
type User = { login: string; name: string; avatarUrl: string };
type Setup = { githubConfigured: boolean; appSlug: string; installUrl: string };
type ImportApp = {
  token: string;
  appId: number;
  clientId: string;
  clientSecret: string;
  webhookSecret: string;
  privateKey: string;
};

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
  cpuLimit: number;                    // cores, 0 = unlimited
  memoryLimit: number;                 // bytes, 0 = unlimited
  autoDeploy: boolean; waitForCi: boolean;
  status: ServiceStatus;
  privateHost: string;                 // "<name>"
  domains: Domain[];
  volumes: Volume[];
  latestDeployment: Deployment | null;
  restoreFence: RestoreFence | null;   // set while a restore runs or after one failed
  createdAt: string;
};
type RestoreFence = {
  restoreId: string;
  phase: "retaining" | "replacing" | "loading";
  createdAt: string;
};

// Create: kind "app" needs repo+branch or image; database kinds need only name.
type NewService = { name: string; kind: ServiceKind; repo?: string; branch?: string; image?: string };
// Patch: any subset of the editable Service fields (name excluded).
type ServicePatch = Partial<Pick<Service,
  "repo" | "branch" | "rootDir" | "image" | "dockerfilePath" | "startCommand" |
  "port" | "healthcheckPath" | "publicPort" | "cpuLimit" | "memoryLimit" |
  "autoDeploy" | "waitForCi">>;

type Deployment = {
  id: string; serviceId: string; status: DeploymentStatus;
  trigger: "push" | "manual" | "redeploy" | "create";
  commitSha: string; commitMessage: string; commitAuthor: string;
  image: string; error: string;
  imageAvailable?: boolean; // only in lists: image remains on this server
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

// Resource usage of the whole host, bucketed like Metrics.
type HostMetrics = {
  range: "1h" | "6h" | "24h" | "7d";
  start: string; step: number;
  cpus: number;                        // online CPUs
  memoryTotal: number;                 // bytes
  diskTotal: number;                   // bytes, filesystem holding data.dir
  cpu: (number | null)[];              // percent of one core (max cpus*100)
  memory: (number | null)[];           // bytes in use (total - available)
  diskUsed: (number | null)[];         // bytes used on that filesystem
  netRx: (number | null)[];            // bytes/s, physical interfaces
  netTx: (number | null)[];
  diskRead: (number | null)[];         // bytes/s, physical disks
  diskWrite: (number | null)[];
};

type BackupCompression = "fastest" | "default" | "better" | "best";
type BackupPolicy = {
  enabled: boolean;
  schedule: string;                    // cron, UTC unless "CRON_TZ=<zone> ..."
  compression: BackupCompression;
  keepLocal: number;                   // 0 only with upload; local kept until uploaded
  upload: boolean;                     // ignored while S3 is not configured
  keepRemote: number;                  // >= 1 when upload
  nextRunAt: string | null;            // null when disabled
};
type BackupPolicyInput = Omit<BackupPolicy, "nextRunAt">;
type BackupStatus = "queued" | "running" | "uploading" | "succeeded" | "failed"; // uploading: archive written, S3 upload in progress; poll until succeeded
type Backup = {
  id: string; serviceId: string | null;  // null = shed.db
  trigger: "schedule" | "manual" | "pre-restore";
  method: "dump" | "volume" | "sqlite";
  status: BackupStatus;
  fileName: string;                    // download name, e.g. "postgres-20261004-030000.sql.zst"
  size: number;                        // archive bytes
  encrypted: boolean;
  local: boolean; remote: boolean;
  remoteError: string; error: string;
  createdAt: string; finishedAt: string | null;
};
type Restore = {
  id: string; serviceId: string; backupId: string;
  status: "running" | "succeeded" | "failed"; error: string;
  createdAt: string; finishedAt: string | null;
};
type ServiceBackups = { policy: BackupPolicy; backups: Backup[]; restore: Restore | null }; // latest restore
type SystemBackups = { policy: BackupPolicy; backups: Backup[] };
type S3Settings = {
  endpoint: string;                    // URL, e.g. "https://s3.us-east-1.amazonaws.com"
  region: string; bucket: string; prefix: string;
  accessKeyId: string; pathStyle: boolean;
  hasSecret: boolean;
};
type BackupSettings = {
  s3: S3Settings | null;
  encryption: { enabled: boolean; recipient: string };  // recipient "" until a key exists
};
// secretAccessKey omitted or "" keeps the stored secret. s3 null removes the destination.
type BackupSettingsInput = {
  s3: (Omit<S3Settings, "hasSecret"> & { secretAccessKey?: string }) | null;
  encryption: { enabled: boolean };
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
as a service variable. Runtime redaction uses the active deployment's recorded
values, so saving a new value does not unmask the one still running. Deployments
without a runtime snapshot fall back to current variables. Changing variables
cannot retroactively remove secrets from older saved logs.

### Replacement storage safety

A deployment that needs exclusive volumes or a published host port must confirm
that the previous container is stopped before starting its replacement. Docker
inspection and stop failures abort deployment and preserve the active container.
Before stopping it, every other container labeled with the service (such as a
failed candidate whose removal failed) is stopped and removed; then every
container of the service must be listed as stopped. Any failure fails the
deployment before its container is created.

If removing a failed replacement cannot be confirmed, recovery leaves the
predecessor stopped rather than risking concurrent use of its persistent volume,
and the failed deployment's error says so. A failed container start is treated
as ambiguous, since Docker may have started the container anyway: the
container is removed, and if that fails its ID is still returned so the
pipeline can retry. Before restarting the predecessor, every container labeled
with the service other than the predecessor's is stopped and removed, using a
context that outlives the canceled deployment; any failure leaves the service
down.
Starting the active container of such a service (on boot, `start`, or the end
of a backup hold) likewise first removes every container of the service from
another deployment; if that fails, the service stays down and the error is
returned or logged.

### Routing activation safety

The proxy must accept routes targeting a healthy candidate before that deployment
is recorded active or its predecessor is retired. Failed reloads fail the candidate,
remove its container, and retain the previous active deployment and route. Route
updates are serialized through activation to prevent stale route publication.

A service loses its routes only when its absence is confirmed: it is stopped,
has no active deployment, or Docker reports its container not found. Any
other store or Docker error while computing routes fails the update, so the
proxy keeps its last applied configuration (and a deployment switching over
fails instead of publishing routes without the other services).

Once routing succeeds, activation and cleanup finish even if the deployment is
canceled concurrently, so a routed candidate is not removed mid-activation.

After a domain is created or deleted, the API applies routes with a 30-second
context deadline. The change is already stored, so a failure does not fail the
request: the response carries `Shed-Routes: pending`, the error is logged, and
`Server.SyncRoutes` (started after `Reconcile`) retries immediately, then with
backoff from 2 seconds doubling to 1 minute until it succeeds. It does nothing
while routes are applied. Attempts are numbered so an older attempt's success
cannot clear a newer failure. Route failures inside the deploy pipeline are
not tracked by this loop.

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

A service held for a backup or restore (`Deployer.Hold`) rejects deploys,
redeploys, runtime controls, its own deletion, its project's deletion, and
deletion of its volumes with `ErrServiceBusy` (409) until released. Pushes that
arrive during a hold are stored pending (202) and deploy after release unless
superseded meanwhile (see "Webhooks").

A service with a restore fence rejects deploys, redeploys, start, and restart
with `ErrFenced` (409), checked under the same lock as the other admission
guards; pushes for it remain pending (see "Webhooks"). Volume deletion is serialized with the other runtime operations.

### Expansion and log memory limits

Variable resolution rejects raw or expanded values over 64 KiB, cumulative
resolved values over 1 MiB, and reference chains deeper than 64. Limits are checked
before appending expanded content. Build redaction streams chunks while retaining
only enough overlap to mask secrets split across writes. Runtime SSE and build-log
replay split oversized unterminated lines into bounded chunks (approximately
64 KiB), preserving their content without accumulating arbitrary-length lines.

### Workload resource ceilings

Each service sets its own CPU quota (`cpuLimit`, in cores) and memory limit
(`memoryLimit`, in bytes, with no additional swap); 0 means unlimited. New
services start at one CPU and 1 GiB. The CPU limit must be at least 0.01 and at
most the host's CPU count, and the memory limit at least 64 MiB. Like other
settings, changed limits apply from the service's next deployment. Every app and
database container also has a 512-process limit, and Docker JSON logs rotate at
10 MiB with at most three files per container. Host capacity must still account
for the total number of workloads and build overhead.

The shared build runner allows one active build across all services. A 30-minute
build deadline includes queueing, cloning, Railpack preparation, and image
construction. Cancellation terminates the subprocess group, including child
processes, before releasing the build slot.

Builds, Dockerfile and Railpack alike, run on a dedicated buildx builder named
`shed` (`docker buildx build --builder shed`) using the docker-container driver,
because workload limits do not constrain Docker's default BuildKit. Before its
first build, shed removes the builder with `--keep-state` (keeping the build
cache) and recreates it with `memory`/`memory-swap` set to `build.memory_mb` and
a CFS quota of `build.cpus` cores, so configuration changes apply after a
restart. A build fails before cloning when the filesystem of the build
directory or Docker's root directory (`docker info`) has less than
`build.min_free_mb` available. While a build runs, shed also polls free space on
those paths every three seconds. If either drops below half of
`build.min_free_mb`, it kills the build commands, removes the workspace, and
fails the build with a low-disk error that the deploy log shows. This is
best-effort monitoring, not a hard quota: a build can still write quickly
between polls, and shed does not prune the builder's cache afterward.

### Webhook resource budget

The public webhook endpoint rejects missing or malformed signatures before reading
bodies. Validly shaped requests share a four-request concurrency cap and a token
bucket (eight-request burst, one request per second). Payloads are limited to
1 MiB and body reads to ten seconds. Excess traffic receives 429; oversized
payloads receive 413. Large GitHub push events above this budget require a manual
deployment.

### Volume deletion failures

Deleting a volume tolerates a missing Docker volume and defers removal only when
Docker reports that the volume is still in use. Other Docker errors fail the
request and preserve the volume record for retry; they are not reported as a
successful deferred deletion.

### Webhook delivery deduplication

Successfully authenticated push payloads are hashed and tracked per matching
service. Repeated deliveries skip pushes already stored; concurrent
processing or storage failures return 503, allowing manual retries without
duplicating stored pushes. Deployment failures leave the push pending. The in-memory cache holds at most 1,024 entries for up to
24 hours and clears on restart; old completed entries may be evicted at capacity.
