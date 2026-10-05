import type { DocSection, DocSlug } from "./registry";

type Topic<S extends DocSlug = DocSlug> = {
  page: S;
  section: DocSection<S>;
  /** One or two sentences shown in the tooltip. */
  summary: string;
};

/** TopicTarget is a checked topic: a docs anchor and its summary. */
type TopicTarget = { page: DocSlug; section: string; summary: string };

/** topic checks a topic's section against its page at compile time. */
const topic = <S extends DocSlug>(t: Topic<S>): TopicTarget => t;

/** docTopics are the targets of the dashboard's ? tooltips. */
export const docTopics = {
  projects: topic({
    page: "concepts",
    section: "projects",
    summary: "A project is a group of services that share a private Docker network.",
  }),
  settingsApply: topic({
    page: "concepts",
    section: "settings-apply",
    summary: "Settings save immediately but take effect with the service's next deployment.",
  }),
  pipeline: topic({
    page: "deployments",
    section: "pipeline",
    summary: "Wait for CI, build, start, health check, then switch traffic to the new container.",
  }),
  deploymentStatus: topic({
    page: "deployments",
    section: "statuses",
    summary: "queued → waiting → building → deploying → active, or failed, canceled, or skipped.",
  }),
  buildLog: topic({
    page: "deployments",
    section: "build-log",
    summary: "Each pipeline step gets a ==> heading; the container's boot output is copied in.",
  }),
  buildDetection: topic({
    page: "builds",
    section: "detection",
    summary: "A Dockerfile is used if present; otherwise Railpack generates a build plan.",
  }),
  autoDeploy: topic({
    page: "github",
    section: "webhooks",
    summary: "A push to the service's branch triggers a deployment through the GitHub App webhook.",
  }),
  waitForCi: topic({
    page: "github",
    section: "wait-for-ci",
    summary: "Hold the deploy until every check run and status on the commit passes.",
  }),
  privateNetwork: topic({
    page: "networking",
    section: "private-network",
    summary: "Services in a project reach each other at <name>:<port> on a per-project network.",
  }),
  domains: topic({
    page: "networking",
    section: "domains",
    summary:
      "The embedded Caddy routes each domain to the active container and issues certificates.",
  }),
  generatedDomains: topic({
    page: "networking",
    section: "generated-domains",
    summary: "<service>-<project>.<base_domain>, which needs a wildcard DNS record.",
  }),
  publicPort: topic({
    page: "networking",
    section: "public-port",
    summary:
      "Publish the container port on a host TCP port, e.g. to reach a database from outside.",
  }),
  healthchecks: topic({
    page: "networking",
    section: "healthchecks",
    summary: "A TCP connect, or a 2xx/3xx on the healthcheck path, within 120 seconds.",
  }),
  variables: topic({
    page: "variables",
    section: "references",
    summary: "Values can reference ${{ KEY }} or ${{ service.KEY }}, resolved at deploy time.",
  }),
  injectedVariables: topic({
    page: "variables",
    section: "injected",
    summary: "PORT and SHED_* variables are injected into every deployment.",
  }),
  volumes: topic({
    page: "resources",
    section: "volumes",
    summary:
      "Named Docker volumes that survive deploys. Services with volumes don't overlap during deploys.",
  }),
  resourceLimits: topic({
    page: "resources",
    section: "limits",
    summary: "A CPU quota in cores and a memory limit with no extra swap. 0 means unlimited.",
  }),
  metrics: topic({
    page: "metrics",
    section: "sampling",
    summary: "Docker stats sampled every 10 seconds and kept for 7 days.",
  }),
  hostMetrics: topic({
    page: "metrics",
    section: "host",
    summary: "Read from /proc and sysfs; network and disk count physical devices only.",
  }),
  runtimeLogs: topic({
    page: "logs",
    section: "runtime-logs",
    summary: "The active container's stdout and stderr, streamed over SSE with secrets masked.",
  }),
  shedLog: topic({
    page: "logs",
    section: "shed-log",
    summary: "shed's own log: the last 1000 lines are kept in memory and streamed live.",
  }),
  backupMethods: topic({
    page: "backups",
    section: "methods",
    summary: "Running databases are dumped; everything else is archived as a tar of its volumes.",
  }),
  backupEncryption: topic({
    page: "backups",
    section: "encryption",
    summary:
      "Archives are encrypted with an age key kept in shed's database. Save it off the server.",
  }),
  backupS3: topic({
    page: "backups",
    section: "s3",
    summary: "Archives are uploaded to any S3-compatible bucket and kept off-site.",
  }),
  backupSchedule: topic({
    page: "backups",
    section: "schedule",
    summary: "Standard cron, in UTC unless prefixed with CRON_TZ=<zone>.",
  }),
  backupRetention: topic({
    page: "backups",
    section: "retention",
    summary:
      "Keep the newest N scheduled backups locally and in S3. Manual backups are never pruned.",
  }),
  restoreFences: topic({
    page: "restores",
    section: "fences",
    summary: "While a restore changes its data, a service can't be deployed or started.",
  }),
} as const;

export type DocTopic = keyof typeof docTopics;
