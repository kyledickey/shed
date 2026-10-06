/** DocTopic names a target of the dashboard's ? tooltips. */
export type DocTopic = keyof typeof docTopics;

/**
 * docTopics are the targets of the dashboard's ? tooltips: a page slug, the
 * id of one of its ## headings, and a one-line summary. topics.test.ts
 * checks every target against content/*.mdx.
 */
export const docTopics = {
  projects: {
    page: "concepts",
    section: "projects",
    summary: "A project is a group of services that share a private Docker network.",
  },
  settingsApply: {
    page: "concepts",
    section: "when-settings-apply",
    summary: "Settings save immediately but take effect with the service's next deployment.",
  },
  pipeline: {
    page: "deployments",
    section: "the-pipeline",
    summary: "Wait for CI, build, start, health check, then switch traffic to the new container.",
  },
  deploymentStatus: {
    page: "deployments",
    section: "deployment-statuses",
    summary: "queued → waiting → building → deploying → active, or failed, canceled, or skipped.",
  },
  buildLog: {
    page: "deployments",
    section: "reading-a-build-log",
    summary: "Each pipeline step gets a ==> heading; the container's boot output is copied in.",
  },
  buildDetection: {
    page: "builds",
    section: "dockerfile-or-railpack",
    summary: "A Dockerfile is used if present; otherwise Railpack generates a build plan.",
  },
  autoDeploy: {
    page: "github",
    section: "push-webhooks",
    summary: "A push to the service's branch triggers a deployment through the GitHub App webhook.",
  },
  waitForCi: {
    page: "github",
    section: "waiting-for-ci",
    summary: "Hold the deploy until every check run and status on the commit passes.",
  },
  privateNetwork: {
    page: "networking",
    section: "private-network",
    summary: "Services in a project reach each other at <name>:<port> on a per-project network.",
  },
  domains: {
    page: "networking",
    section: "custom-domains",
    summary:
      "The embedded Caddy routes each domain to the active container and issues certificates.",
  },
  generatedDomains: {
    page: "networking",
    section: "generated-domains",
    summary: "<service>-<project>.<base_domain>, which needs a wildcard DNS record.",
  },
  publicPort: {
    page: "networking",
    section: "public-tcp-ports",
    summary:
      "Publish the container port on a host TCP port, e.g. to reach a database from outside.",
  },
  healthchecks: {
    page: "networking",
    section: "health-checks",
    summary: "A TCP connect, or a 2xx/3xx on the healthcheck path, within 120 seconds.",
  },
  variables: {
    page: "variables",
    section: "references",
    summary: "Values can reference ${{ KEY }} or ${{ service.KEY }}, resolved at deploy time.",
  },
  injectedVariables: {
    page: "variables",
    section: "injected-variables",
    summary: "PORT and SHED_* variables are injected into every deployment.",
  },
  volumes: {
    page: "resources",
    section: "volumes",
    summary:
      "Named Docker volumes that survive deploys. Services with volumes don't overlap during deploys.",
  },
  resourceLimits: {
    page: "resources",
    section: "cpu-and-memory-limits",
    summary: "A CPU quota in cores and a memory limit with no extra swap. 0 means unlimited.",
  },
  metrics: {
    page: "metrics",
    section: "container-sampling",
    summary: "Docker stats sampled every 10 seconds and kept for 7 days.",
  },
  hostMetrics: {
    page: "metrics",
    section: "host-metrics",
    summary: "Read from /proc and sysfs; network and disk count physical devices only.",
  },
  runtimeLogs: {
    page: "logs",
    section: "runtime-logs",
    summary: "The active container's stdout and stderr, streamed over SSE with secrets masked.",
  },
  shedLog: {
    page: "logs",
    section: "sheds-own-log",
    summary: "shed's own log: the last 1000 lines are kept in memory and streamed live.",
  },
  backupMethods: {
    page: "backups",
    section: "backup-methods",
    summary: "Running databases are dumped; everything else is archived as a tar of its volumes.",
  },
  backupEncryption: {
    page: "backups",
    section: "encryption",
    summary:
      "Archives are encrypted with an age key kept in shed's database. Save it off the server.",
  },
  backupS3: {
    page: "backups",
    section: "s3-storage",
    summary: "Archives are uploaded to any S3-compatible bucket and kept off-site.",
  },
  backupSchedule: {
    page: "backups",
    section: "schedules",
    summary: "Standard cron, in UTC unless prefixed with CRON_TZ=<zone>.",
  },
  backupRetention: {
    page: "backups",
    section: "retention",
    summary:
      "Keep the newest N scheduled backups locally and in S3. Manual backups are never pruned.",
  },
  restoreFences: {
    page: "restores",
    section: "restore-fences",
    summary: "While a restore changes its data, a service can't be deployed or started.",
  },
  updates: {
    page: "installation",
    section: "updates",
    summary:
      "shed checks GitHub for releases, verifies downloads, and installs on request with a short restart.",
  },
  mcp: {
    page: "mcp",
    section: "connecting",
    summary:
      "Agents connect to shed's /mcp endpoint and sign in with GitHub. Access is read-only and never includes variable values.",
  },
} satisfies Record<string, { page: string; section: string; summary: string }>;
