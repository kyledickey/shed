# Changelog

Each release's section becomes its GitHub release notes, which the dashboard
shows under Settings → Updates.

## v0.2.6 - 2026-10-23

- 1Password now keeps every service's variables in one vault you choose,
  with one service account for all of shed. Create a vault in 1Password,
  then a service account with read and write access to it, and paste its
  token in Settings → 1Password. shed adds an item for each service, titled
  `<project> / <service>` and tagged by project, and moves your variables
  into them. The Variables tab reads and writes those items, and edits made
  in the 1Password app apply on the next deploy. Disconnecting copies the
  variables back to the server.
- New services get an item automatically, renaming a project renames its
  items, and deleting a service archives its item.
- The per-service 1Password Environment links from v0.2.4 and v0.2.5 are
  removed. Connect once in Settings → 1Password to move your variables into
  a vault.

## v0.2.5 - 2026-10-23

- Each service's 1Password link has its own service account token, so a
  token can read only that service's Environment. 1Password can't change a
  service account's access after it's created, so create one per
  Environment and paste its token with the Environment ID from the
  1Password button on the Variables tab. shed reads the Environment before
  saving the link, so a wrong ID or token fails right away. The 1Password
  card in Settings is gone.
- Services linked in v0.2.4 keep their Environment but lose the shared
  token. Their deploys fail until you add a token on the Variables tab.
- Fixed deploys failing with "invalid client id" a while after 1Password
  was connected.

## v0.2.4 - 2026-10-23

- Services can load variables from a 1Password Environment. Connect a
  1Password service account in Settings → 1Password, then link an
  Environment with the 1Password button on a service's Variables tab. shed
  reads the Environment each time the service deploys and shows its
  variables in the Variables tab; variables you set in shed take priority.
  1Password doesn't announce changes, so redeploy after you change a value.
  While connected, 1Password uses about 110 MB of shed's memory.

## v0.2.3 - 2026-10-06

- shed is licensed under the Apache License 2.0.
- More space between the logo and the tabs on phones.

## v0.2.2 - 2026-10-06

- Metric charts end at the limit when there is one (vCPUs, total memory and
  disk, a service's limits) and otherwise fit the data closely. Byte axes no
  longer overshoot, e.g. 46.6 GB for a 23.4 GB server.
- Settings → Agents shows one connect command at a time, with tabs for any
  agent, Claude Code, Codex, and the URL.

## v0.2.1 - 2026-10-06

- Settings → Agents, the MCP docs, and the `shed` skill show how to connect
  with `npx add-mcp` (any agent) and Codex, alongside Claude Code.

## v0.2.0 - 2026-10-06

- Secrets in shed.db are encrypted at rest: variables, GitHub App
  credentials, the backup age identity, S3 secret keys, and deployment
  runtimes. On first start shed generates `/etc/shed/shed.key` (or reads it
  from systemd credentials) and converts the existing database. Back up
  `shed.key` off the server: restoring shed.db needs it, and shed refuses to
  start with the wrong one.
- A read-only MCP server at `/mcp` lets agents like Claude Code inspect
  projects, deployments, build and runtime logs, and metrics. Agents sign in
  through a built-in OAuth 2.1 server, and Settings lists and revokes them.
  The repository ships a `shed` skill for agents.
- `proxy.cloudflare` trusts Cloudflare's edge for the client IP and sets
  `X-Forwarded-For` and `X-Forwarded-Host` correctly for proxied domains.
- A public site at shed.land with the docs, and a shorter installer:
  `curl -fsSL https://shed.land/install.sh | sudo bash`.
- A new logo and favicon, a full-bleed layout on phones, and docs without the
  right-hand outline.

## v0.1.1 - 2026-10-05

- Variable values are hidden in the table until revealed, and copying a value
  with references copies it resolved.
- Logs no longer mask variable values shorter than 8 characters, which
  mangled timestamps and JSON in services with values like `1` or `true`.
- The service header no longer shows an image's `@sha256:` digest.

## v0.1.0 - 2026-10-05

The first release of shed.

- Projects and services: apps from a GitHub repo (Dockerfile or Railpack) or a
  Docker image, and one-click Postgres, MySQL, MongoDB, and Redis.
- Push-to-deploy through a GitHub App, optionally waiting for CI, with
  zero-downtime switchovers, rollbacks, and build and runtime logs.
- Variables with `${{ service.KEY }}` references, domains with automatic
  HTTPS, volumes, resource limits, and public TCP ports.
- Scheduled, compressed, optionally encrypted backups to local disk and S3,
  with restore and download.
- Host and service metrics, shed's own log, and in-app docs.
- One-command installer, signed releases, and in-app updates.
