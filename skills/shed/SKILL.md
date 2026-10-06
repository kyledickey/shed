---
name: shed
description: Inspect and troubleshoot apps deployed on shed, a self-hosted single-server deployment platform, through its read-only MCP tools. Use when the user deploys on shed, mentions shed, or asks about their shed projects, services, deployments, build logs, runtime logs, metrics, variables, or backups, including "why did my deploy fail" or "is my service healthy".
---

# shed

shed is a self-hosted deployment platform for one Linux server. It builds apps
from a GitHub repository (Dockerfile, or Railpack when there isn't one) or runs
a Docker image, runs everything in Docker behind HTTPS, and offers Postgres,
MySQL, MongoDB, and Redis as database services. You reach it through its MCP
server, which is **read-only**: you can look at everything that matters for
diagnosis, and you cannot change anything.

## Before you start

Use the `shed` MCP tools listed below. If they are not available, the server is
not connected. Tell the user to run this, then sign in in the browser and
approve access:

```sh
claude mcp add --transport http shed https://<shed-host>/mcp
```

Other MCP clients take the same URL (`https://<shed-host>/mcp`) and sign in
with OAuth. `<shed-host>` is the address of their shed dashboard. If a tool
fails with an authorization error, the grant was revoked or expired: the user
reconnects, or re-approves under Settings, Agents in the dashboard.

## Mental model

- A **project** is a group of services on one private network. There are no
  environments; staging is a second project.
- A **service** is an **app** (a GitHub repo and branch, or a Docker image) or
  a database (`postgres`, `mysql`, `mongo`, `redis`).
- A **deployment** is one attempt to get a commit or image running: build,
  start a new container beside the old one, health check, then switch traffic.
  If it fails, the old container keeps serving.
- IDs are 12-character lowercase base32 strings. Tools take IDs, not names:
  get them from `list_projects` and `get_project`.

More in [references/concepts.md](references/concepts.md).

## Tools

All tools are read-only.

| Tool | Use it for |
| ---- | ---------- |
| `list_projects` | Every project with its services and their status. Start here. |
| `get_project(project_id)` | One project: its services with settings, domains, volumes, latest deployment. |
| `get_service(service_id)` | One service in full: kind, repo, branch, port, limits, status, domains, volumes, latest deployment. |
| `list_deployments(service_id, limit?)` | Deployment history, newest first (shed keeps 50 per service). |
| `get_deployment(deployment_id)` | One deployment: status, trigger, commit, image, and the recorded `error`. |
| `build_log(deployment_id, lines?)` | The deployment's build log. Default 200 lines, max 2000. |
| `runtime_logs(service_id, lines?)` | A snapshot of the active container's recent log. Default 200, max 2000. Not a live stream. |
| `service_metrics(service_id, range?)` | CPU, memory, network, disk I/O. Range `1h`, `6h`, `24h`, or `7d`. |
| `host_metrics(range?)` | The same for the whole server, plus disk used and totals. |
| `list_variables(service_id)` | Variable **names** only, never values. |
| `list_backups(service_id)` | The service's backups and its backup policy (schedule, retention). |
| `shed_log(lines?)` | shed's own application log. |
| `update_status` | Running shed version and whether a newer release exists. |
| `list_repos` | GitHub repositories the shed GitHub App can see. |
| `list_branches(owner, repo)` | Branches of one of those repositories. |

## What you cannot do

There are no write tools in this version. Never claim to have deployed,
redeployed, restarted, changed a variable, or taken a backup. When a fix needs
a change, say which dashboard action to take:

| To do this | The user does this |
| ---------- | ------------------ |
| Apply a code or config fix | Push to the branch (auto deploy), or Deploy in the service page. |
| Retry the same image or roll back | Service, Deployments: **Redeploy** on a deployment, or **Roll back to this** on a replaced one. |
| Stop a stuck deployment | **Cancel** on the deployment. |
| Restart, stop, or start a service | The service's **Restart** / **Stop** buttons. |
| Add or change a variable | Service, **Variables** tab, then deploy to apply. |
| Change port, health check path, start command, limits | Service, **Settings** tab, then deploy to apply. |
| Back up or restore | Service, **Backups** tab. |
| Install a shed update | Settings, Updates in the dashboard. |

Settings and variables saved in the dashboard reach a running container only
with the next deployment.

## Secrets: names only

Variable values are never available to you. `list_variables` returns names, and
that is enough to check whether a required variable exists or whether a
`${{ ... }}` reference points at a name that exists. If you need a value to
diagnose something, ask the user to check it in the dashboard rather than to
paste it into the conversation. Logs are masked: you may see `***` where a
variable value was. Values shorter than 8 characters are not masked, and
masking is by exact string, so treat logs as possibly sensitive anyway. See
[references/variables.md](references/variables.md).

## Workflow: why did my deploy fail

1. `list_projects`, then `get_service` for the service. Note `status` and
   `latestDeployment`.
2. `list_deployments` and look at the recent statuses and triggers.
3. `get_deployment` on the failing one and read its `error`.
4. `build_log` for that deployment. Find the last `==> ` heading: it is the
   stage that failed.
5. If it failed after the container started, `runtime_logs` and
   `service_metrics` (memory against the limit, CPU pegged).
6. Explain the cause and name the dashboard action that fixes it.

The full procedure, including how to read each stage and common causes, is in
[references/diagnosing-deploys.md](references/diagnosing-deploys.md).

## More workflows

- **Is it healthy?** `get_service`: status `active` means the container is
  running. `crashed` means the active deployment's container is not running.
  Then `runtime_logs` and `service_metrics`.
- **Is the server short on resources?** `host_metrics` for CPU, memory, and
  disk. A full disk makes builds fail with `not enough free disk space`.
- **A variable seems missing.** `list_variables`, and compare against the names
  the app reads. Injected names (`PORT`, `SHED_PRIVATE_DOMAIN`, ...) appear
  there too.
- **Backups.** `list_backups` shows recent backups with status and the policy.
  Failed backups carry an error.
- **Something wrong in shed itself** (webhooks, routes, sign-in): `shed_log`.
- **Setting up a new service.** `list_repos` and `list_branches` show what the
  user can pick. Creating the service is done in the dashboard.

## Reference files

- [concepts.md](references/concepts.md): projects, services, kinds, status rules, when settings apply.
- [diagnosing-deploys.md](references/diagnosing-deploys.md): the deployment pipeline, statuses, reading logs, common failures.
- [builds.md](references/builds.md): Dockerfile versus Railpack, build secrets, ports, builder limits.
- [variables.md](references/variables.md): reference syntax, injected variables, database variables.
