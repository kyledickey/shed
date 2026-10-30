# Changelog

Each release's section becomes its GitHub release notes, which the dashboard
shows under Settings → Updates.

## v0.2.6 - 2026-10-23

1Password now keeps every service's variables in one vault you choose, with one service account for all of shed.

### Features

#### Variables

- Added a 1Password vault for variables. Create a vault in 1Password, then a service account with read and write access to it, and paste its token in Settings → 1Password. shed adds an item for each service, titled `<project> / <service>` and tagged by project, and moves your variables into them. ([#11](https://github.com/kyledickey/shed/pull/11))
- Changed the Variables tab to read and write those items. Edits made in the 1Password app apply on the next deploy. ([#11](https://github.com/kyledickey/shed/pull/11))
- Added automatic item upkeep: new services get an item, renaming a project renames its items, and deleting a service archives its item. ([#11](https://github.com/kyledickey/shed/pull/11))
- Changed disconnecting 1Password to copy the variables back to the server. ([#11](https://github.com/kyledickey/shed/pull/11))

### Breaking Changes

- Removed the per-service 1Password Environment links from v0.2.4 and v0.2.5. Connect once in Settings → 1Password to move your variables into a vault. ([#11](https://github.com/kyledickey/shed/pull/11))

## v0.2.5 - 2026-10-23

Each service's 1Password link now has its own service account token.

### Features

#### Variables

- Changed each service's 1Password link to use its own service account token, so a token can read only that service's Environment. 1Password can't change a service account's access after it's created, so create one per Environment and paste its token with the Environment ID from the 1Password button on the Variables tab. ([67d2631](https://github.com/kyledickey/shed/commit/67d2631))
- Improved linking so shed reads the Environment before saving the link, and a wrong ID or token fails right away. ([67d2631](https://github.com/kyledickey/shed/commit/67d2631))
- Removed the 1Password card from Settings. ([67d2631](https://github.com/kyledickey/shed/commit/67d2631))

### Bug Fixes

- Fixed deploys failing with "invalid client id" a while after 1Password was connected. ([#10](https://github.com/kyledickey/shed/pull/10))

### Breaking Changes

- Services linked in v0.2.4 keep their Environment but lose the shared token, so their deploys fail. Add a token for each on its Variables tab.

## v0.2.4 - 2026-10-23

Services can now load variables from a 1Password Environment.

### Features

#### Variables

- Added loading variables from a 1Password Environment. Connect a 1Password service account in Settings → 1Password, then link an Environment with the 1Password button on a service's Variables tab. shed reads the Environment each time the service deploys and shows its variables in the Variables tab. Variables you set in shed take priority. 1Password doesn't announce changes, so redeploy after you change a value. While connected, 1Password uses about 110 MB of shed's memory. ([#10](https://github.com/kyledickey/shed/pull/10))

## v0.2.3 - 2026-10-06

- Added the Apache License 2.0. ([04ff0b6](https://github.com/kyledickey/shed/commit/04ff0b6))
- Improved the dashboard on phones with more space between the logo and the tabs. ([a2a9438](https://github.com/kyledickey/shed/commit/a2a9438))

## v0.2.2 - 2026-10-06

### Features

#### Dashboard

- Improved the metric charts so they end at the limit when there is one (vCPUs, total memory and disk, a service's limits) and otherwise fit the data closely. Byte axes no longer overshoot, for example 46.6 GB for a 23.4 GB server. ([01f6074](https://github.com/kyledickey/shed/commit/01f6074))

#### Agents

- Changed Settings → Agents to show one connect command at a time, with tabs for any agent, Claude Code, Codex, and the URL. ([d9170d0](https://github.com/kyledickey/shed/commit/d9170d0))

## v0.2.1 - 2026-10-06

- Added `npx add-mcp` (any agent) and Codex connect instructions to Settings → Agents, the MCP docs, and the `shed` skill, alongside Claude Code. ([3bb4fe4](https://github.com/kyledickey/shed/commit/3bb4fe4))

## v0.2.0 - 2026-10-06

Secrets in shed.db are now encrypted at rest, agents can inspect shed through a read-only MCP server, and shed has a public site at shed.land.

### Features

#### Networking

- Added `proxy.cloudflare`, which trusts Cloudflare's edge for the client IP and sets `X-Forwarded-For` and `X-Forwarded-Host` correctly for proxied domains. ([#4](https://github.com/kyledickey/shed/pull/4))

#### Dashboard

- Added a new logo and favicon. ([4d1f305](https://github.com/kyledickey/shed/commit/4d1f305))
- Improved the layout on phones with a full-bleed frame. ([6903049](https://github.com/kyledickey/shed/commit/6903049))
- Removed the right-hand outline from docs pages. ([a51fb74](https://github.com/kyledickey/shed/commit/a51fb74))

#### Agents

- Added a read-only MCP server at `/mcp` that lets agents like Claude Code inspect projects, deployments, build and runtime logs, and metrics. Agents sign in through a built-in OAuth 2.1 server, and Settings lists and revokes them. The repository ships a `shed` skill for agents. ([#6](https://github.com/kyledickey/shed/pull/6))

#### Installation and Updates

- Added a public site at shed.land with the docs, and a shorter installer: `curl -fsSL https://shed.land/install.sh | sudo bash`. ([#5](https://github.com/kyledickey/shed/pull/5))

#### Other

- Added encryption at rest for secrets in shed.db: variables, GitHub App credentials, the backup age identity, S3 secret keys, and deployment runtimes. On first start shed generates `/etc/shed/shed.key` (or reads it from systemd credentials) and converts the existing database. ([#7](https://github.com/kyledickey/shed/pull/7))

### Breaking Changes

- Back up `/etc/shed/shed.key` off the server. Restoring shed.db needs it, and shed refuses to start with the wrong one.

## v0.1.1 - 2026-10-05

### Features

#### Variables

- Changed the Variables table to hide values until revealed, and copying a value with references now copies it resolved. ([#3](https://github.com/kyledickey/shed/pull/3))

#### Dashboard

- Removed the image's `@sha256:` digest from the service header. ([#3](https://github.com/kyledickey/shed/pull/3))

### Bug Fixes

- Fixed logs masking variable values shorter than 8 characters, which mangled timestamps and JSON in services with values like `1` or `true`. ([#3](https://github.com/kyledickey/shed/pull/3))

## v0.1.0 - 2026-10-05

The first release of shed.

### Features

#### Deployments

- Added projects and services: apps from a GitHub repo (Dockerfile or Railpack) or a Docker image, and one-click Postgres, MySQL, MongoDB, and Redis. ([#1](https://github.com/kyledickey/shed/pull/1))
- Added push-to-deploy through a GitHub App, optionally waiting for CI, with zero-downtime switchovers, rollbacks, and build and runtime logs. ([#1](https://github.com/kyledickey/shed/pull/1))

#### Variables

- Added variables with `${{ service.KEY }}` references. ([#1](https://github.com/kyledickey/shed/pull/1))

#### Networking

- Added domains with automatic HTTPS, volumes, resource limits, and public TCP ports. ([#1](https://github.com/kyledickey/shed/pull/1))

#### Backups

- Added scheduled, compressed, optionally encrypted backups to local disk and S3, with restore and download. ([#1](https://github.com/kyledickey/shed/pull/1))

#### Observability

- Added host and service metrics and shed's own log. ([#1](https://github.com/kyledickey/shed/pull/1))

#### Installation and Updates

- Added a one-command installer, signed releases, and in-app updates. ([#1](https://github.com/kyledickey/shed/pull/1))

#### Other

- Added in-app docs. ([#1](https://github.com/kyledickey/shed/pull/1))
