import { Link } from "@tanstack/react-router";
import { CodeBlock, Doc, DocSection, DocTable, Figure } from "../kit";
import { DataDirDiagram, LayerDiagram } from "./configuration/diagrams";
import { EnvConverter } from "./configuration/EnvConverter";
import { BuildLimitsDemo } from "./configuration/BuildLimitsDemo";

export function ConfigurationDoc() {
  return (
    <Doc
      slug="configuration"
      lede="shed reads one TOML file and a set of SHED_* environment variables. Everything else, including GitHub App credentials, sessions, and backup settings, lives in the database."
    >
      <DocSection page="configuration" id="requirements">
        <p>shed is a single binary, but it drives other tools on the host.</p>
        <DocTable
          head={["Requirement", "Why"]}
          rows={[
            ["Linux", "Host metrics are read from procfs, sysfs, and statfs."],
            [
              "Docker Engine with the buildx plugin",
              "Every workload is a container. Builds use a dedicated buildx builder.",
            ],
            [<code key="g">git</code>, "Clones the commit being built."],
            [<code key="r">railpack</code>, "On PATH. Builds apps that have no Dockerfile."],
            [
              "Root, or a user in the docker group",
              "To talk to the Docker socket. The example unit runs as root.",
            ],
            [
              "Ports 80 and 443",
              <>
                Caddy terminates TLS there. Change them with <code>proxy.http_port</code> and{" "}
                <code>proxy.https_port</code>, or turn the proxy off with <code>proxy.enabled</code>
                .
              </>,
            ],
          ]}
        />
        <p>
          The binary is usually run under systemd with <code>CAP_NET_BIND_SERVICE</code> so it can
          bind low ports. The example unit that ships with shed:
        </p>
        <CodeBlock title="/etc/systemd/system/shed.service">
          {`[Unit]
Description=shed deployment platform
After=network-online.target docker.service
Wants=network-online.target
Requires=docker.service

[Service]
ExecStart=/usr/local/bin/shed -config /etc/shed/shed.toml
Restart=always
RestartSec=2
AmbientCapabilities=CAP_NET_BIND_SERVICE
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target`}
        </CodeBlock>
      </DocSection>

      <DocSection page="configuration" id="config-file">
        <p>
          The file defaults to <code>/etc/shed/shed.toml</code>; point at another with{" "}
          <code>-config</code>. A missing file is not an error: you get the built-in defaults. Every
          key is optional. The values below are the defaults, except <code>server.url</code>, where
          the built-in default is <code>http://localhost:3000</code> and the example shows a public
          URL.
        </p>
        <Figure caption="Later layers override earlier ones, key by key.">
          <LayerDiagram />
        </Figure>
        <CodeBlock title="shed.toml">
          {`[server]
listen = "127.0.0.1:3000"           # dashboard + API listener (Caddy fronts it)
url    = "https://shed.example.com" # public dashboard URL; GitHub callbacks and the origin check use it

[data]
dir = "/var/lib/shed"               # shed.db, builds/, logs/, backups/, caddy/

[proxy]
enabled     = true
http_port   = 80
https_port  = 443
acme_email  = ""                    # contact address for certificate issuance
base_domain = ""                    # e.g. "apps.example.com" -> <service>-<project>.apps.example.com

[auth]
allowed_users = []                  # GitHub logins. Empty denies everyone.

[build]
memory_mb   = 2048                  # builder container memory, no swap; 0 = unlimited
cpus        = 2                     # builder container CPU quota in cores; 0 = unlimited
min_free_mb = 2048                  # 0 = disk check off

[deployments]
log_max_mb = 10                     # per-deployment build log cap; 0 = unlimited
keep       = 50                     # finished deployments kept per service; 0 = keep all

[log]
level        = "info"               # debug | info | warn | error
max_size_mb  = 20
max_backups  = 5
max_age_days = 30`}
        </CodeBlock>
        <p>
          shed validates on boot and refuses to start on a bad value: <code>server.listen</code> and{" "}
          <code>data.dir</code> can't be empty, <code>server.url</code> must be an http(s) URL with
          a host, ports must be 1-65535, <code>log.level</code> must be one of the four levels, and
          no limit or log setting may be negative.
        </p>
        <p>
          Set <code>allowed_users</code> before the first sign-in. See{" "}
          <Link to="/docs/$slug" params={{ slug: "security" }} hash="authentication">
            Security
          </Link>{" "}
          for what it controls.
        </p>
      </DocSection>

      <DocSection page="configuration" id="env-overrides">
        <p>
          Any key can be set with an environment variable, and the variable wins over the file. The
          rule is mechanical: strip <code>SHED_</code>, lowercase, and replace the{" "}
          <strong>first</strong> underscore with a dot. The remaining underscores stay.
        </p>
        <CodeBlock lang="sh">
          {`SHED_SERVER_URL=https://shed.example.com      # server.url
SHED_PROXY_ACME_EMAIL=you@example.com         # proxy.acme_email
SHED_AUTH_ALLOWED_USERS=alice,Bob             # auth.allowed_users (comma-separated)`}
        </CodeBlock>
        <p>
          Values are strings that shed decodes into each field's type, so{" "}
          <code>SHED_PROXY_ENABLED=false</code> works. <code>auth.allowed_users</code> is the one
          list: split on commas, with blanks trimmed. A name that maps to no key is ignored without
          a warning, so a typo like <code>SHED_PROXYACME_EMAIL</code> silently does nothing. The
          converter flags those.
        </p>
        <EnvConverter />
      </DocSection>

      <DocSection page="configuration" id="data-dir">
        <p>
          shed creates <code>data.dir</code>, <code>logs</code>, <code>builds</code>, and{" "}
          <code>backups</code> on boot (mode 0750). The application log is the exception: it goes
          next to the config file, not in the data directory.
        </p>
        <Figure caption="Docker volumes live in Docker's own storage, so back up with shed, not by copying data.dir.">
          <DataDirDiagram />
        </Figure>
        <ul>
          <li>
            <strong>shed.log</strong> is rotated by <code>log.max_size_mb</code>,{" "}
            <code>log.max_backups</code>, and <code>log.max_age_days</code>, and mirrored to stderr.
            The last 1000 lines are also kept in memory, which is what <code>GET /api/logs</code>{" "}
            replays before it follows new lines (Server, Logs in the dashboard).
          </li>
          <li>
            <strong>shed.db</strong> is the only copy of your projects, variables, deployments,
            sessions, metrics, and GitHub App credentials. shed backs it up like any service.
          </li>
          <li>
            <strong>backups/</strong> holds one directory per service id, plus <code>system</code>.
            Directories are mode 0700 and archives 0600.
          </li>
          <li>
            <strong>builds/</strong> and <strong>logs/</strong> are named by deployment id. Build
            workspaces are deleted after the build. Logs are deleted with their deployment.
          </li>
        </ul>
      </DocSection>

      <DocSection page="configuration" id="build-limits">
        <p>
          Builds run on a dedicated buildx builder named <code>shed</code> with the docker-container
          driver, because workload limits don't constrain Docker's default BuildKit. Before its
          first build, shed removes the builder with <code>--keep-state</code> (the build cache
          survives) and recreates it with memory and swap set to <code>build.memory_mb</code> and a
          CFS quota of <code>build.cpus</code> cores. That is why a limit change applies after a
          restart.
        </p>
        <DocTable
          mono
          head={["Key", "Default", "Effect"]}
          rows={[
            ["build.memory_mb", "2048", "Builder memory in MiB, no swap. 0 = unlimited."],
            ["build.cpus", "2", "Builder CPU quota in cores. 0 = unlimited."],
            [
              "build.min_free_mb",
              "2048",
              "A build is refused below this much free space on the build directory's filesystem or Docker's root. A running build is canceled below half of it. 0 = off.",
            ],
            [
              "deployments.log_max_mb",
              "10",
              "Cap on each build log. Output beyond it becomes one final '==> Log truncated' line. 0 = unlimited.",
            ],
            [
              "deployments.keep",
              "50",
              "Finished deployments (failed, removed, canceled, skipped) kept per service, newest first. Older ones are deleted with their logs. Active, crashed, and in-progress ones never are. 0 = keep all.",
            ],
          ]}
        />
        <p>
          While a build runs, shed polls free space every three seconds. This is best-effort
          monitoring, not a hard quota: a build can write quickly between polls, and shed doesn't
          prune the builder's cache afterwards. One build runs at a time across all services, with a
          30-minute deadline that includes queueing.
        </p>
        <BuildLimitsDemo />
      </DocSection>
    </Doc>
  );
}
