# Changelog

Each release's section becomes its GitHub release notes, which the dashboard
shows under Settings → Updates.

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
