import { Link } from "@tanstack/react-router";
import {
  Archive,
  ArrowRight,
  Braces,
  Database,
  GitBranch,
  LockKeyhole,
  ScrollText,
} from "lucide-react";
import type { ReactNode } from "react";
import { buttonClass } from "../src/components/Button";
import { CopyButton, GitHubIcon } from "../src/components/Misc";
import type { Edge } from "../src/features/projects/graph";
import { ProjectDiagram, type DiagramService } from "../src/features/projects/ProjectDiagram";
import { docGroups } from "../src/features/docs/registry";
import styles from "./Home.module.css";
import { installCommand, repoUrl } from "./links";

/** Sample project for the hero; never live data. */
const sample = (
  name: string,
  kind: DiagramService["kind"],
  extra: Partial<DiagramService> = {},
): DiagramService => ({
  id: name,
  projectId: "acme",
  name,
  kind,
  status: "active",
  repo: "",
  branch: "main",
  image: "",
  privateHost: name,
  port: 0,
  domains: [],
  volumes: kind === "app" ? [] : [{}],
  latestDeployment: null,
  ...extra,
});

const hourAgo = new Date(Date.now() - 3_600_000).toISOString();

const services: DiagramService[] = [
  sample("web", "app", {
    repo: "acme/web",
    port: 3000,
    domains: [{ id: "d1", host: "acme.dev" }],
    latestDeployment: { status: "active", createdAt: hourAgo },
  }),
  sample("api", "app", {
    repo: "acme/api",
    port: 8080,
    domains: [{ id: "d2", host: "api.acme.dev" }],
    latestDeployment: { status: "active", createdAt: hourAgo },
  }),
  sample("worker", "app", {
    repo: "acme/api",
    status: "deploying",
    latestDeployment: { status: "building", createdAt: new Date().toISOString() },
  }),
  sample("postgres", "postgres", { port: 5432 }),
  sample("redis", "redis", { port: 6379 }),
];

const edges: Edge[] = [
  { from: "api", to: "postgres" },
  { from: "api", to: "redis" },
  { from: "worker", to: "postgres" },
  { from: "worker", to: "redis" },
];

type Feature = { icon: ReactNode; title: string; body: ReactNode; slug: string };

const features: Feature[] = [
  {
    icon: <GitBranch size={16} />,
    title: "Push to deploy",
    body: "Connect a repo and every push builds from your Dockerfile, or with Railpack when there isn't one. Optionally wait for CI to pass first.",
    slug: "deployments",
  },
  {
    icon: <Database size={16} />,
    title: "Databases in a click",
    body: "Postgres, MySQL, MongoDB, and Redis on persistent volumes. Services reach each other by name on a private project network.",
    slug: "concepts",
  },
  {
    icon: <LockKeyhole size={16} />,
    title: "HTTPS built in",
    body: "Caddy runs inside shed. Point a domain at the server and certificates are issued and renewed for you.",
    slug: "networking",
  },
  {
    icon: <Braces size={16} />,
    title: "Variables with references",
    body: (
      <>
        Wire services together with <code>{"${{ postgres.DATABASE_URL }}"}</code>. Values resolve at
        deploy time and are masked in logs.
      </>
    ),
    slug: "variables",
  },
  {
    icon: <Archive size={16} />,
    title: "Backups you can restore",
    body: "Scheduled dumps and volume archives, compressed with zstd, optionally encrypted with age, kept on disk and in S3.",
    slug: "backups",
  },
  {
    icon: <ScrollText size={16} />,
    title: "Logs, metrics, rollbacks",
    body: "Live build and runtime logs, CPU and memory per service, zero-downtime switchovers, and one-click redeploys of any past build.",
    slug: "logs",
  },
];

const steps = [
  {
    title: "Install",
    body: "Run the installer on a Linux server with ports 80 and 443 free. It sets up Docker and the systemd service, then prints a setup link.",
  },
  {
    title: "Connect GitHub",
    body: "shed creates its own GitHub App on first run. It handles sign-in, repo access, push webhooks, and CI status.",
  },
  {
    title: "Deploy",
    body: "Add a repo or a Docker image to a project, give it a domain, and push. Each push becomes a new deployment.",
  },
];

/** Home is the landing page: what shed is, how to install it, and where the docs are. */
export function Home() {
  return (
    <div className={styles.page}>
      <section className={styles.hero}>
        <h1 className={styles.title}>Your own deployment platform, on a server you control.</h1>
        <p className={styles.lede}>
          shed builds your GitHub repos, runs them in Docker next to their databases, puts them
          behind HTTPS, and backs everything up. One Go binary on a Linux box you own.
        </p>

        <div id="install" className={styles.install}>
          <div className={styles.installHead}>
            <span>Install on Linux</span>
            <CopyButton value={installCommand} label="Copy install command" />
          </div>
          <pre className={styles.command}>
            <span className={styles.prompt}>$ </span>
            {installCommand}
          </pre>
        </div>
        <p className={styles.installNote}>
          Installs Docker, git, and Railpack if they're missing, asks for your domain, and starts
          shed as a systemd service. Run it again to upgrade.
        </p>

        <div className={styles.actions}>
          <Link
            to="/docs/$slug"
            params={{ slug: "overview" }}
            className={buttonClass({ variant: "primary", size: "lg" })}
          >
            Read the docs
            <ArrowRight size={15} />
          </Link>
          <a href={repoUrl} className={buttonClass({ variant: "secondary", size: "lg" })}>
            <GitHubIcon size={15} />
            View on GitHub
          </a>
        </div>
      </section>

      <section className={styles.preview} aria-label="A project in the shed dashboard">
        <ProjectDiagram services={services} edges={edges} />
      </section>

      <section className={styles.section}>
        <h2 className={styles.h2}>Everything a small production setup needs</h2>
        <div className={styles.features}>
          {features.map((f) => (
            <Link
              key={f.title}
              to="/docs/$slug"
              params={{ slug: f.slug }}
              className={styles.feature}
            >
              <span className={styles.featureIcon}>{f.icon}</span>
              <h3 className={styles.featureTitle}>{f.title}</h3>
              <p className={styles.featureBody}>{f.body}</p>
            </Link>
          ))}
        </div>
      </section>

      <section className={styles.section}>
        <h2 className={styles.h2}>From a fresh server to a live app</h2>
        <ol className={styles.steps}>
          {steps.map((s, i) => (
            <li key={s.title} className={styles.step}>
              <span className={styles.stepNum}>{i + 1}</span>
              <h3 className={styles.featureTitle}>{s.title}</h3>
              <p className={styles.featureBody}>{s.body}</p>
            </li>
          ))}
        </ol>
      </section>

      <section className={styles.section}>
        <h2 className={styles.h2}>Documentation</h2>
        <div className={styles.docGroups}>
          {docGroups.map((g) => (
            <div key={g.title} className={styles.docGroup}>
              <div className={styles.docGroupTitle}>{g.title}</div>
              {g.pages.map((p) => (
                <Link
                  key={p.slug}
                  to="/docs/$slug"
                  params={{ slug: p.slug }}
                  className={styles.docLink}
                >
                  {p.title}
                </Link>
              ))}
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}
