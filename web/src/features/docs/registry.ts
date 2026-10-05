/**
 * The docs table of contents. Section ids are anchors on the page and the
 * targets of HelpTip links (see topics.ts), so renaming one breaks links.
 */
export const docGroups = [
  {
    title: "Getting started",
    pages: [
      {
        slug: "overview",
        title: "Overview",
        sections: [
          { id: "what-is-shed", title: "What shed is" },
          { id: "architecture", title: "Architecture" },
          { id: "request-path", title: "Life of a request" },
        ],
      },
      {
        slug: "concepts",
        title: "Projects and services",
        sections: [
          { id: "projects", title: "Projects" },
          { id: "services", title: "Services" },
          { id: "service-kinds", title: "Service kinds" },
          { id: "service-status", title: "Service status" },
          { id: "settings-apply", title: "When settings apply" },
        ],
      },
    ],
  },
  {
    title: "Deploying",
    pages: [
      {
        slug: "deployments",
        title: "Deployments",
        sections: [
          { id: "pipeline", title: "The pipeline" },
          { id: "statuses", title: "Deployment statuses" },
          { id: "zero-downtime", title: "Zero-downtime switchover" },
          { id: "build-log", title: "Reading a build log" },
          { id: "rollbacks", title: "Redeploys and rollbacks" },
          { id: "history", title: "History and retention" },
          { id: "stopping", title: "Stop, start, restart" },
          { id: "reconcile", title: "Reconcile on boot" },
        ],
      },
      {
        slug: "builds",
        title: "Builds",
        sections: [
          { id: "detection", title: "Dockerfile or Railpack" },
          { id: "railpack", title: "Railpack" },
          { id: "builder", title: "The shed builder" },
          { id: "build-secrets", title: "Build secrets" },
          { id: "disk-guard", title: "Disk space guard" },
          { id: "images", title: "Images and ports" },
        ],
      },
      {
        slug: "github",
        title: "GitHub",
        sections: [
          { id: "github-app", title: "One GitHub App" },
          { id: "setup", title: "First-run setup" },
          { id: "sign-in", title: "Sign-in and access" },
          { id: "webhooks", title: "Push webhooks" },
          { id: "wait-for-ci", title: "Waiting for CI" },
        ],
      },
    ],
  },
  {
    title: "Running",
    pages: [
      {
        slug: "networking",
        title: "Networking",
        sections: [
          { id: "private-network", title: "Private network" },
          { id: "proxy", title: "The proxy" },
          { id: "domains", title: "Custom domains" },
          { id: "generated-domains", title: "Generated domains" },
          { id: "public-port", title: "Public TCP ports" },
          { id: "healthchecks", title: "Health checks" },
        ],
      },
      {
        slug: "variables",
        title: "Variables",
        sections: [
          { id: "references", title: "References" },
          { id: "injected", title: "Injected variables" },
          { id: "templates", title: "Database templates" },
          { id: "playground", title: "Resolver playground" },
          { id: "resolution", title: "How resolution works" },
          { id: "limits", title: "Limits" },
        ],
      },
      {
        slug: "resources",
        title: "Volumes and resources",
        sections: [
          { id: "volumes", title: "Volumes" },
          { id: "limits", title: "CPU and memory limits" },
          { id: "storage-safety", title: "Replacement storage safety" },
        ],
      },
      {
        slug: "metrics",
        title: "Metrics",
        sections: [
          { id: "sampling", title: "Container sampling" },
          { id: "host", title: "Host metrics" },
          { id: "buckets", title: "Ranges and buckets" },
        ],
      },
      {
        slug: "logs",
        title: "Logs",
        sections: [
          { id: "build-logs", title: "Build logs" },
          { id: "runtime-logs", title: "Runtime logs" },
          { id: "shed-log", title: "shed's own log" },
          { id: "sse", title: "Streaming over SSE" },
          { id: "redaction", title: "Secret redaction" },
        ],
      },
    ],
  },
  {
    title: "Data",
    pages: [
      {
        slug: "backups",
        title: "Backups",
        sections: [
          { id: "methods", title: "Backup methods" },
          { id: "pipeline", title: "Archive pipeline" },
          { id: "compression", title: "Compression" },
          { id: "encryption", title: "Encryption" },
          { id: "s3", title: "S3 storage" },
          { id: "schedule", title: "Schedules" },
          { id: "retention", title: "Retention" },
          { id: "downloads", title: "Downloads and manual recovery" },
        ],
      },
      {
        slug: "restores",
        title: "Restores",
        sections: [
          { id: "flow", title: "How a restore runs" },
          { id: "volume-restores", title: "Volume restores" },
          { id: "dump-restores", title: "Dump restores" },
          { id: "fences", title: "Restore fences" },
          { id: "recovery", title: "Crash recovery" },
          { id: "shed-db", title: "Recovering shed.db" },
        ],
      },
    ],
  },
  {
    title: "Operating",
    pages: [
      {
        slug: "configuration",
        title: "Configuration",
        sections: [
          { id: "requirements", title: "Host requirements" },
          { id: "config-file", title: "Config file" },
          { id: "env-overrides", title: "Environment overrides" },
          { id: "data-dir", title: "Data directory" },
          { id: "build-limits", title: "Build limits" },
        ],
      },
      {
        slug: "security",
        title: "Security model",
        sections: [
          { id: "authentication", title: "Authentication" },
          { id: "sessions", title: "Sessions" },
          { id: "requests", title: "Request protections" },
          { id: "secrets", title: "Where secrets live" },
          { id: "webhook-limits", title: "Webhook limits" },
        ],
      },
      {
        slug: "api",
        title: "HTTP API",
        sections: [
          { id: "conventions", title: "Conventions" },
          { id: "explorer", title: "Try it" },
          { id: "endpoints", title: "Endpoints" },
          { id: "types", title: "Types" },
        ],
      },
    ],
  },
  {
    title: "Developing",
    pages: [
      {
        slug: "codebase",
        title: "Codebase",
        sections: [
          { id: "layout", title: "Repository layout" },
          { id: "packages", title: "Packages" },
          { id: "principles", title: "Design principles" },
          { id: "wiring", title: "Wiring in cmd/shed" },
        ],
      },
      {
        slug: "internals",
        title: "Internals",
        sections: [
          { id: "http", title: "HTTP handling" },
          { id: "deployer", title: "The deployer" },
          { id: "resolver", title: "Variable resolver" },
          { id: "builds", title: "Build runner" },
          { id: "logging", title: "Logs and redaction" },
          { id: "metrics", title: "Metrics collector" },
          { id: "backups", title: "Backup manager" },
        ],
      },
      {
        slug: "local-development",
        title: "Local development",
        sections: [
          { id: "prerequisites", title: "Prerequisites" },
          { id: "running", title: "Running shed locally" },
          { id: "testing", title: "Tests and checks" },
          { id: "conventions", title: "Conventions" },
        ],
      },
      {
        slug: "dashboard",
        title: "Dashboard",
        sections: [
          { id: "stack", title: "Stack" },
          { id: "structure", title: "Structure" },
          { id: "styling", title: "Styling" },
          { id: "writing-docs", title: "Writing these docs" },
        ],
      },
    ],
  },
] as const;

type Group = (typeof docGroups)[number];
type Page = Group["pages"][number];

/** DocSlug names a docs page, e.g. "deployments". */
export type DocSlug = Page["slug"];

/** DocSection names an anchor on page S. */
export type DocSection<S extends DocSlug> = Extract<Page, { slug: S }>["sections"][number]["id"];

/** DocPage is one page's metadata. */
export type DocPage = {
  slug: DocSlug;
  title: string;
  sections: readonly { id: string; title: string }[];
};

export const docPages: readonly DocPage[] = docGroups.flatMap((g): readonly DocPage[] => g.pages);

/** isDocSlug reports whether s names a docs page. */
export function isDocSlug(s: string): s is DocSlug {
  return docPages.some((p) => p.slug === s);
}

/** docPage returns the metadata of page slug. */
export function docPage(slug: DocSlug): DocPage {
  return docPages.find((p) => p.slug === slug)!;
}
