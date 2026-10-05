# shed

shed is a self-hosted, single-server deployment platform inspired by Railway.
One Go binary runs the dashboard, the API, the deploy pipeline, and an
embedded Caddy reverse proxy with automatic HTTPS; Docker runs the workloads.

- Deploy apps from GitHub repositories (Dockerfile, or Railpack when there is
  none) or from Docker images, automatically on push, optionally after CI
  passes.
- One-click Postgres, MySQL, MongoDB, and Redis with persistent volumes.
- Per-service variables with `${{ service.KEY }}` references, domains,
  deployment history with rollbacks, and build and runtime logs.

See [docs/design.md](docs/design.md) for how it works.

## Requirements

- Linux with Docker Engine and the buildx plugin
- `git` and [`railpack`](https://railpack.com) on `PATH`
- Ports 80 and 443 free for the proxy

## Install

```sh
curl -fsSL https://github.com/kyledickey/shed/releases/latest/download/install.sh | sudo bash
```

The installer adds whatever is missing (Docker with buildx, `git`, `railpack`),
asks for the dashboard domain, an optional apps base domain and ACME email, and
the GitHub logins allowed to sign in, then installs the latest release,
writes `/etc/shed/shed.toml`, starts the systemd service, and prints the setup
URL and token. Run it again on an installed host to upgrade the binary and keep
the config.

To run it unattended, pass the answers as environment variables
(`SHED_BASE_DOMAIN`, `SHED_ACME_EMAIL`, and `SHED_VERSION` are optional):

```sh
curl -fsSL https://github.com/kyledickey/shed/releases/latest/download/install.sh \
  | sudo SHED_DOMAIN=shed.example.com SHED_ALLOWED_USERS=your-github-login SHED_YES=1 bash
```

### Manual install

```sh
make build                                   # needs Go and Bun
sudo install bin/shed /usr/local/bin/shed
sudo mkdir -p /etc/shed
sudo cp deploy/shed.example.toml /etc/shed/shed.toml   # then edit it
sudo cp deploy/shed.service /etc/systemd/system/
sudo systemctl enable --now shed
```

Set `server.url` (the public dashboard URL), `proxy.acme_email`, and
`auth.allowed_users = ["your-github-login"]`.
Every setting can also be given as an environment variable, such as
`SHED_SERVER_URL`. Logs go to `shed.log` next to the config file and to the
journal.

## DNS

- An `A` (and `AAAA`) record for the dashboard host in `server.url`,
  pointing at the server.
- For generated service domains, set `proxy.base_domain` (for example
  `apps.example.com`) and add a wildcard record `*.apps.example.com` pointing
  at the server.
- Custom domains need their own record pointing at the server.

## First run

1. On first start, shed logs a one-time **setup token**. The installer prints
   it with the setup URL; otherwise find it with
   `journalctl -u shed | grep token`.
2. Open the dashboard, enter the token, and follow the link to create the
   GitHub App. GitHub sends you back to shed, which stores the App's
   credentials.
3. Install the App on the accounts or repositories you want to deploy.
4. Sign in with a GitHub login listed in `auth.allowed_users`. There is no
   automatic first-user enrollment.

## Development

```sh
make test                      # Go tests
cp deploy/shed.example.toml shed.dev.toml
# In shed.dev.toml: url = "http://localhost:5173", a writable data.dir, and
# proxy.enabled = false.
make dev                       # runs shed with shed.dev.toml
cd web && bun run dev --port 5173 --strictPort  # dashboard with hot reload
```
