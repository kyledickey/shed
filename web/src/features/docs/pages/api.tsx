import { Fragment } from "react";
import { Link } from "@tanstack/react-router";
import { Badge } from "../../../components/Badge";
import { endpointGroups } from "../lib/apiEndpoints";
import { CodeBlock, Doc, DocSection, DocTable, Figure } from "../kit";
import { RoutingDiagram } from "./api/diagram";
import { CurlBuilder } from "./api/CurlBuilder";
import styles from "./api.module.css";

export function ApiDoc() {
  return (
    <Doc
      slug="api"
      lede="Everything the dashboard does goes through a JSON API under /api, and you can call it too. Anything you can click has an endpoint, and curl can call it."
    >
      <DocSection page="api" id="conventions">
        <p>
          The dashboard is just a client of this API, so anything you can click has an endpoint. It
          is not versioned.
        </p>
        <Figure caption="One handler serves the API, the webhook, and the dashboard.">
          <RoutingDiagram />
        </Figure>
        <ul>
          <li>
            <strong>JSON, camelCase.</strong> IDs are 12-character lowercase base32 strings. Times
            are RFC 3339 UTC. A <code>204</code> has no body.
          </li>
          <li>
            <strong>Errors</strong> are <code>&#123;"error": "message"&#125;</code> with a real
            status. Unexpected failures are logged and reported as a generic{" "}
            <code>500 internal error</code>.
          </li>
          <li>
            <strong>Authentication</strong> is the <code>shed_session</code> cookie. Auth, setup,
            and the webhook are public; everything else answers 401 without a session. Unknown{" "}
            <code>/api/*</code> paths are a JSON 404, not the dashboard.
          </li>
          <li>
            <strong>Mutations</strong> from a browser must come from the origin of{" "}
            <code>server.url</code>, and POST, PUT, and PATCH need{" "}
            <code>Content-Type: application/json</code> even with an empty body. See{" "}
            <Link to="/docs/$slug" params={{ slug: "security" }} hash="requests">
              request protections
            </Link>
            .
          </li>
        </ul>
        <DocTable
          mono
          head={["Status", "Meaning"]}
          rows={[
            [
              "400",
              "Invalid input, or a request the target can't satisfy, such as a backup of a service with no volumes.",
            ],
            ["401", "No session, or the user is no longer on allowed_users."],
            ["403", "untrusted request origin."],
            ["404", "Unknown route or id."],
            [
              "409",
              "Conflict: a name in use, a service busy with a backup or restore, fenced, stopped, or being deleted.",
            ],
            ["413 / 415", "Webhook body too large, or a mutation without a JSON content type."],
            ["429", "The webhook guard is full."],
            ["502 / 503", "GitHub failed on repos and branches, or shed is shutting down."],
          ]}
        />
        <h3>Streams</h3>
        <p>
          Build logs, runtime logs, and shed's own log are server-sent events. Each frame is{" "}
          <code>event: log</code> with one line per <code>data:</code>, <code>event: status</code>{" "}
          when a deployment's status changes (data <code>&#123;"status":"…"&#125;</code>), and{" "}
          <code>event: end</code> when there's nothing more. An idle stream sends a{" "}
          <code>: ping</code> comment every 15 seconds. Reconnecting clients get history replayed.
          Runtime log lines start with the container's RFC 3339 timestamp and a space.
        </p>
        <CodeBlock lang="sh">
          {`curl -N https://your-shed/api/services/<id>/logs \\
  -H 'Cookie: shed_session=<your session token>'`}
        </CodeBlock>
      </DocSection>

      <DocSection page="api" id="explorer">
        <p>
          The API has no tokens. You authenticate with the <code>shed_session</code> cookie, so the
          simplest way to script it is to sign in with your browser and copy that cookie's value. It
          is HttpOnly, so page scripts can't read it; find it in your browser's developer tools
          under Application, Cookies. It is valid for 30 days.
        </p>
        <h3>A read</h3>
        <CodeBlock lang="sh">
          {`curl -s https://shed.example.com/api/projects \\
  -H 'Cookie: shed_session=<your session token>'`}
        </CodeBlock>
        <CodeBlock title="response" lang="json">
          {`[
  {
    "id": "k3m9x2q7a1bz",
    "name": "blog",
    "createdAt": "2026-10-04T09:12:44Z",
    "services": [
      { "id": "p8d4n6t2c5wy", "name": "web", "kind": "app", "status": "active" },
      { "id": "h2v7r1j9e4ms", "name": "db", "kind": "postgres", "status": "active" }
    ]
  }
]`}
        </CodeBlock>
        <h3>A mutation</h3>
        <p>
          POST, PUT, and PATCH need <code>Content-Type: application/json</code>, even with no body.
          curl sends no <code>Origin</code> header, so the origin check passes and the cookie is
          what authorizes the call. A browser-based client must instead run on the origin of{" "}
          <code>server.url</code>.
        </p>
        <CodeBlock lang="sh">
          {`curl -s -X POST https://shed.example.com/api/services/<id>/backups \\
  -H 'Cookie: shed_session=<your session token>' \\
  -H 'Content-Type: application/json'`}
        </CodeBlock>
        <p>
          Work that runs in the background answers <code>202</code> with the new row, which you can
          poll. Failures are <code>&#123;"error": "message"&#125;</code>:
        </p>
        <CodeBlock title="409 Conflict" lang="json">
          {`{ "error": "service is busy with a backup or restore" }`}
        </CodeBlock>
        <h3>Build your own</h3>
        <p>
          Pick any endpoint from the reference below and type your server's URL. The command is
          built in your browser and sends nothing.
        </p>
        <CurlBuilder />
      </DocSection>

      <DocSection page="api" id="endpoints">
        <p>
          Placeholders like <code>&#123;id&#125;</code> are ids. <code>POST</code> endpoints that
          start work answer 202 with the new row.
        </p>
        {endpointGroups.map((g) => (
          <div key={g.title}>
            <h3>{g.title}</h3>
            <DocTable
              head={["Method", "Path", "Body", "Response", "Notes"]}
              rows={g.endpoints.map((e) => [
                <Badge key="m" size="sm" mono>
                  {e.method}
                </Badge>,
                <span key="p" className={styles.mono}>
                  {breakable(e.path)}
                </span>,
                e.body ? (
                  <span key="b" className={styles.mono}>
                    {e.body}
                  </span>
                ) : (
                  ""
                ),
                <span key="r" className={styles.mono}>
                  {e.response}
                </span>,
                e.notes ?? "",
              ])}
            />
          </div>
        ))}
        <p>
          <code>GET /*</code> serves the dashboard with an <code>index.html</code> fallback, so
          client-side routes survive a reload. Missing assets are 404.
        </p>
      </DocSection>

      <DocSection page="api" id="types">
        <p>These are the shapes behind the responses above, written as TypeScript types.</p>
        <h3>Account and setup</h3>
        <CodeBlock title="TypeScript">
          {`type User = { login: string; name: string; avatarUrl: string };
type Setup = { githubConfigured: boolean; appSlug: string; installUrl: string };
type ImportApp = {
  token: string;
  appId: number;
  clientId: string;
  clientSecret: string;
  webhookSecret: string;
  privateKey: string;
};`}
        </CodeBlock>
        <h3>Projects and services</h3>
        <CodeBlock title="TypeScript">
          {`type ServiceKind = "app" | "postgres" | "mysql" | "mongo" | "redis";
type ServiceStatus = "offline" | "deploying" | "active" | "failed" | "crashed" | "stopped";

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

type Domain = { id: string; host: string; generated: boolean; url: string };
type Volume = { id: string; mountPath: string; createdAt: string };
type Repo = { fullName: string; defaultBranch: string; private: boolean };`}
        </CodeBlock>
        <h3>Deployments</h3>
        <CodeBlock title="TypeScript">
          {`type DeploymentStatus =
  | "queued" | "waiting" | "building" | "deploying" | "active"
  | "failed" | "crashed" | "removed" | "canceled" | "skipped";

type Deployment = {
  id: string; serviceId: string; status: DeploymentStatus;
  trigger: "push" | "manual" | "redeploy" | "create";
  commitSha: string; commitMessage: string; commitAuthor: string;
  image: string; error: string;
  createdAt: string; startedAt: string | null; finishedAt: string | null;
};`}
        </CodeBlock>
        <h3>Metrics</h3>
        <CodeBlock title="TypeScript">
          {`// Container resource usage. Sample i is at start + i*step seconds; series
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
};`}
        </CodeBlock>
        <h3>Backups</h3>
        <CodeBlock title="TypeScript">
          {`type BackupCompression = "fastest" | "default" | "better" | "best";
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
type BackupStatus = "queued" | "running" | "uploading" | "succeeded" | "failed";
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
type ServiceBackups = { policy: BackupPolicy; backups: Backup[]; restore: Restore | null };
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
};`}
        </CodeBlock>
      </DocSection>
    </Doc>
  );
}

/** breakable lets a path wrap only after its slashes and query marks. */
function breakable(path: string) {
  return path.split(/(?<=[/?&])/).map((part, i) => (
    <Fragment key={i}>
      {i > 0 && <wbr />}
      {part}
    </Fragment>
  ));
}
