# Diagnosing a failed deploy

## The pipeline

Each service has one worker, so its deployments run one at a time. A newer
deployment cancels older unfinished ones (error `superseded by a newer
deployment`). The stages, in order:

1. **Wait for CI**: only when `waitForCi` is on and there is a commit.
2. **Build**: repo apps are cloned at the commit and built. Image apps and
   databases pull their image. A redeploy reuses a recorded image and skips
   this stage.
3. **Start**: variables are resolved and a new container is created beside
   the old one.
4. **Health check**: up to 120 seconds, a TCP connect to the port or a 2xx/3xx
   from the health check path. A service without a port is only watched for
   3 seconds.
5. **Switch**: traffic moves to the new container, the old one is removed, the
   deployment becomes `active`.

If any stage fails, the new container is removed, the deployment is marked
`failed` with an `error`, and the previous container keeps serving. Services
with volumes or a public port cannot overlap two containers, so for them the
old container stops first and there is a short outage; shed restarts it if the
deployment fails.

## Statuses

`queued`, `waiting` (holding for CI), `building`, `deploying` are in progress.
Final: `active` (live), `removed` (was active, replaced), `failed`, `canceled`
(a newer deployment, the Cancel button, or stopping or deleting the service),
`skipped` (CI failed, never built). `crashed` is reserved and not recorded; a
container that dies shows as a `crashed` service, not a deployment status.

Triggers: `push`, `manual`, `redeploy`, `create`.

## Procedure

1. `get_service`: status, latest deployment, port, health check path, limits.
2. `list_deployments`: is this the first failure or a pattern? Did a `push`
   deployment fail while an older one is still `active`?
3. `get_deployment`: read `error`. It usually names the stage.
4. `build_log` with a generous `lines` (the failure is at the end, but the
   cause may be earlier). Find the last `==> ` heading.
5. After the container started: `runtime_logs`, then `service_metrics`.

## Reading the build log

Each stage prints a heading beginning `==> `. Detail lines follow. The last
heading tells you where it stopped.

| Heading | Stage |
| ------- | ----- |
| `==> Waiting for CI on <sha>` | Waiting for CI. |
| `==> Building <repo>@<sha>`, `==> Pulling <image>`, `==> Reusing image <ref>` | Start of build, pull, or redeploy. |
| `==> Cloning` | Clone of the commit. |
| `==> Building with Dockerfile`, `==> Building with Railpack` | The image build; tool output follows. |
| `==> Detected port N from the image` | Port was 0; the lowest exposed port was saved. |
| `==> Stopping previous deployment <id>` | Service has volumes or a public port. |
| `==> Starting container` | Name, image, network, volumes, variable count (never values), start command. |
| `==> Waiting for port N to become healthy`, `==> Waiting for /path to become healthy` | Health check. `Not ready yet: ...` repeats every 5 s. |
| `==> Watching the container start` | Service without a port. |
| `==> Switching traffic` | Alias, second probe, routes. |
| `==> Deployment failed: <reason>` | Final line. The word after "Deployment" is the status: `failed`, `skipped`, or `canceled`. |

The container's own stdout and stderr are copied into the log from start until
the health check ends, so its last words before exiting are in the log. A
container line that begins with `==> ` is shown with a leading space.

The log is capped (10 MB by default). Past the cap output is dropped and the
log ends with `==> Log truncated`, which can hide the final `Deployment
failed` line. On a truncated log trust the deployment's `status` and `error`.

`build_log` returns the last `lines` lines (200 by default, 2000 at most), so
ask for more if the cause is not in what you got.

## Common causes

| Symptom | Likely cause | Fix (user, in the dashboard) |
| ------- | ------------ | ---------------------------- |
| `health check timed out`, `connection refused` | The app does not listen on the service's `port`, or listens on `127.0.0.1` instead of `0.0.0.0`. `runtime_logs` shows the port it announces; `PORT` is injected. | Settings: set the port, or make the app read `PORT`. Deploy. |
| Health check fails on a path | `healthcheckPath` points at a route that errors or redirects. | Settings: fix the path, or clear it for a TCP check. |
| Container exits right away | Crash at startup: missing variable, bad start command, unreachable database. The last log lines say which. `list_variables` shows whether the name exists. | Variables or Settings, then deploy. |
| Killed or restarting under load | Memory at the limit. `service_metrics` shows memory against `memoryLimit` (default 1 GiB). | Settings: raise the memory limit. |
| `vars: reference cycle: ...`, `deploy: resolve variables: ...` | A bad `${{ }}` reference. See variables.md. | Fix the variable, deploy. |
| `build: not enough free disk space`, or canceled mid-build | Host disk nearly full. `host_metrics` shows `diskUsed` against `diskTotal`. | Free disk space on the server. |
| Dockerfile not found, path escapes the repo | `dockerfilePath` or `rootDir` is wrong. A set path never falls back to Railpack. | Settings: fix them. |
| Railpack failed, no Dockerfile in the repo | Railpack could not detect the language, or a build step failed. Read the tool output. | Add a Dockerfile, or fix the build. |
| Dockerfile `ARG` secrets are empty | Variables are build secrets, not build args. | Use `RUN --mount=type=secret,id=KEY`. |
| `skipped` | CI failed for the commit. | Fix CI and push again. |
| `canceled`, `superseded by a newer deployment` | A newer push replaced it. | Look at the newer deployment. |
| `interrupted by restart`, `interrupted by shutdown` | shed restarted mid-deployment. Pipelines do not resume. | Deploy again. |
| Redeploy refused | The image is no longer on the server (shed keeps the five newest built images per service). | Deploy the commit afresh so it rebuilds. |
| A push did not deploy | `autoDeploy` is off, the branch differs, or the webhook did not arrive. `shed_log` shows webhook handling. | Check the service settings and GitHub's webhook deliveries. |
| Service `crashed` | The active container is not running, or its image or a volume is gone. `shed_log` has the reason. | Start or redeploy; if a volume is missing, restore from a backup. |

Also rule out staleness: after variables or settings change, the running
container keeps the old values until a new deployment runs.

## After diagnosing

Say what failed, quote the relevant log line (never invent output), and name
the dashboard action. You cannot perform it yourself.
