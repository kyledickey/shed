# shed concepts

## Projects

A project is a name and a Docker network (`shed-<projectID>`). Its name is
unique across the instance. Services in the same project reach each other by
service name, for example `postgres:5432`. Services in different projects
cannot see each other. There are no environments: for staging, make a second
project.

## Services

A service is one workload. Its name is a DNS label (lowercase letters, digits,
hyphens) and cannot change, because it is also the private hostname.

| Field | Meaning |
| ----- | ------- |
| `repo`, `branch` | An app built from a GitHub repository. Pushes to the branch deploy it. |
| `rootDir` | Subdirectory used as the build context. Empty means the repo root. |
| `dockerfilePath` | Dockerfile relative to `rootDir`. Empty means auto-detect. |
| `image` | An app run from a Docker image, or a database's image. |
| `port` | Container port. New repo apps start at 8080 and receive it as `PORT`. |
| `startCommand` | Overrides the image's command. |
| `healthcheckPath` | Starts with `/`. Empty means a TCP connect to the port. |
| `publicPort` | Publishes a host TCP port. |
| `cpuLimit`, `memoryLimit` | 1 core and 1 GiB by default. 0 is unlimited. |
| `autoDeploy` | On by default. Deploy on every push to the branch. |
| `waitForCi` | Off by default. Hold each deployment until the commit's CI passes. |

## Kinds

`app`, `postgres`, `mysql`, `mongo`, `redis`. Databases come from built-in
templates that fix the image, port, and data volume, and generate the
variables an app needs to connect (for example `DATABASE_URL`). Volumes keep
data across deployments.

## Service status

A service's status is derived, not stored. The first rule that matches wins:

1. Its latest deployment is still in the pipeline: `deploying`.
2. The service was stopped by the user: `stopped`.
3. It has an active deployment whose container is running: `active`. If the
   container is not running: `crashed`.
4. Its latest deployment `failed` and nothing is active: `failed`.
5. Otherwise: `offline`.

A failed redeploy of a service that still has an active deployment leaves it
`active`, because the old container keeps serving. Look at the latest
deployment, not only the service status, to notice a failed deploy.

## Domains

A service can have any number of domains; the first is exposed as
`SHED_PUBLIC_DOMAIN`. A service with domains but no port is not routed.
Caddy, embedded in shed, terminates HTTPS and sends each hostname to the
active container. Domains need a DNS `A` or `AAAA` record pointing at the
server before a certificate can be issued.

## When settings apply

Settings are saved immediately, but a running container keeps what it started
with. Variables, port, health check path, start command, limits, and volumes
reach it with the next deployment. Domains and the `autoDeploy`, `branch`, and
`repo` settings apply immediately. Restart does not re-read settings.

## Backups

Every service with a volume, and shed's own database, can be backed up
manually or on a schedule. Each service has a policy: whether it is enabled,
a cron schedule, how many local copies to keep, whether to upload to S3, and
how many remote copies to keep. `list_backups` returns the policy and the
backups. Restores are a dashboard action.
