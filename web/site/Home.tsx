import { LayerCard, LinkButton, Switch, Tabs } from "@cloudflare/kumo";
import { ArrowRightIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useEffect, useState, type ReactNode } from "react";
import { StatusBadge } from "../src/components/Badge";
import { CopyButton, GitHubIcon } from "../src/components/Misc";
import { Pill } from "../src/components/Pill";
import {
  RecordId,
  RecordList,
  RecordMessage,
  RecordMeta,
  RecordRow,
} from "../src/components/RecordList";
import { Rows, SettingRow, Well } from "../src/components/Settings";
import { StatStrip } from "../src/features/metrics/StatStrip";
import { AgentPrompt } from "../src/features/docs/AgentPrompt";
import { ProjectDiagram } from "../src/features/projects/ProjectDiagram";
import { VarValue } from "../src/features/variables/VarValue";
import { installCommand, repoUrl } from "./links";
import {
  deployments,
  edges,
  resolved,
  services,
  stats,
  statsStart,
  statsStep,
  variables,
} from "./samples";
import styles from "./Home.module.css";

/** Home is the landing page: an essay to the developer with a server, with live figures. */
export function Home() {
  return (
    <article className={styles.page}>
      <header className={styles.hero}>
        <h1 className={styles.title}>Push-to-deploy on the server you already have.</h1>
        <p className={styles.lede}>
          You have a Linux box and a few side projects. Maybe a site, an API, and a Postgres
          database you'd rather not think about. shed turns that box into a small deployment
          platform: push to GitHub, and your app is built, started next to its database, put behind
          HTTPS, and backed up.
        </p>
        <p>It's one binary and it runs on hardware you control. Here's the whole install:</p>
        <Install id="install" />
        <p className={styles.note}>
          On Linux (amd64 or arm64) with systemd, and ports 80 and 443 free. The script installs
          Docker, git, and Railpack if they're missing, asks for your domain, and starts shed.
        </p>
        <nav className={styles.buttons} aria-label="Next steps">
          <LinkButton variant="primary" size="lg" href="/docs/overview">
            Read the docs
          </LinkButton>
          <LinkButton variant="secondary" size="lg" href={repoUrl} icon={<GitHubIcon size={15} />}>
            Source on GitHub
          </LinkButton>
          <AgentPrompt size="lg" />
        </nav>
      </header>

      <Figure
        wide
        caption="A sample project. Two apps on their own domains, a worker, and the Postgres and Redis they share, all on one private network."
      >
        <ProjectDiagram services={services} edges={edges} />
      </Figure>

      <Section title="Push, and it's live">
        <p>
          Connect a repository and pick a branch. Every push builds a new image: from your
          Dockerfile if there is one, or with Railpack if there isn't. If you'd like, shed waits for
          the commit's CI to pass first.
        </p>
        <p>
          The new container starts beside the old one. shed checks that it's healthy, moves the
          traffic over, and only then retires the old one. If the build breaks or the health check
          never passes, nothing changes for your visitors. The old container keeps serving, and the
          failed attempt stays in the history with its full log.
        </p>
      </Section>

      <Figure
        wide
        caption="The deployment history of web. The router upgrade failed and never went live; the previous build kept serving until the next push."
      >
        <History />
      </Figure>

      <Section>
        <p>
          Rolling back is redeploying an older build. It reuses the image, so there's no rebuild and
          no wait for CI. One caveat: a redeploy starts with today's variables, so rolling back code
          doesn't roll back settings.{" "}
          <DocLink slug="deployments" anchor="redeploys-and-rollbacks">
            How deployments work
          </DocLink>
        </p>
      </Section>

      <Section title="Your database lives next door">
        <p>
          Add Postgres, MySQL, MongoDB, or Redis to a project and it runs on a persistent volume,
          with a generated password and a connection string ready to use. Services in a project
          share a private network and find each other by name, like <code>postgres:5432</code>.
          Nothing is published to the internet unless you ask.
        </p>
        <p>
          You never copy that password into your app. You point at it, and shed fills it in when the
          deployment starts:
        </p>
      </Section>

      <Figure caption="Variables on web. References are resolved in the service they point at, and stored values are masked in build and runtime logs.">
        <Variables />
      </Figure>

      <Section title="HTTPS without the yak shaving">
        <p>
          Caddy is built into shed, so there's no proxy to configure. Point a domain at the server,
          add it to a service, and the certificate is issued and renewed for you. Set a wildcard
          base domain and every new app can get its own address, like{" "}
          <code>web-acme.myshed.com</code>, without a trip to your DNS provider.
        </p>
      </Section>

      <Section title="Backups you can restore without shed">
        <p>
          Every service with a volume is backed up on a schedule, and so is shed's own database.
          Databases are dumped with their own tools, like <code>pg_dumpall</code> and{" "}
          <code>mysqldump</code>; anything else has its volume archived.
        </p>
      </Section>

      <Figure caption="The default policy: daily at 03:00 UTC, seven copies kept on disk and thirty in S3. The switches work; it's the dashboard's own form.">
        <BackupPolicy />
      </Figure>

      <Section>
        <p>
          Archives are compressed with zstd, can be encrypted with age, and are kept on disk and, if
          you add one, in any S3-compatible bucket. Restoring is a button in the dashboard. But the
          formats are standard on purpose, so if the server is ever gone, the tools you already know
          are enough:
        </p>
        <pre className={styles.code}>
          <code>age -d -i key.txt postgres.sql.zst.age | zstd -d | psql -U postgres</code>
        </pre>
      </Section>

      <Section title="Knowing when something breaks">
        <p>
          Build logs and live runtime logs are in the dashboard, with your secrets masked. Every
          service gets CPU, memory, network, and disk charts, sampled every ten seconds and kept for
          a week.
        </p>
      </Section>

      <Figure wide caption="An hour of web, as its service page shows it.">
        <Stats />
      </Figure>

      <Section>
        <p>
          When a deploy, backup, or restore fails, or a service runs out of memory or starts
          crashing in a loop, shed can let you know.
        </p>
        <p>
          If you want more, turn on telemetry. shed runs Grafana with VictoriaMetrics, VictoriaLogs,
          and VictoriaTraces on the same server, and accepts OpenTelemetry from your apps.
        </p>
        <p>
          There's also a read-only MCP server, so you can ask a coding agent why the last deploy
          failed. It reads the deployment, the build log, the runtime log, and the metrics, then
          tells you. It can't deploy, restart, or delete anything, and it never sees a variable's
          value.
        </p>
      </Section>

      <Section title="What shed isn't">
        <p>
          shed is small on purpose, and it's better that you hear the limits from us than find them
          later.
        </p>
        <ul className={styles.limits}>
          <Limit title="It's one server.">
            Everything runs on one machine. There's no cluster, no failover, and no scaling out. If
            the server goes down, so do your apps. When it boots again, Docker brings the containers
            back and shed checks its books.
          </Limit>
          <Limit title="There are no environments.">
            A project is a set of services that run together. For staging, make a second project.
          </Limit>
          <Limit title="Databases restart to deploy.">
            Two containers can't share one data directory, so a service with a volume stops its old
            container before the new one starts. Expect a short outage there.
          </Limit>
          <Limit title="Updates restart the proxy.">
            Caddy lives inside shed, so while shed updates itself your domains are unreachable,
            usually for a few seconds. The containers keep running.
          </Limit>
          <Limit title="Builds take turns.">
            One build runs at a time across the whole server, so pushes to several apps at once
            queue up behind each other.
          </Limit>
          <Limit title="It isn't Kubernetes.">
            Underneath, it's Docker, systemd, and one SQLite file, and you can look at each of them
            with the tools you already have.
          </Limit>
        </ul>
      </Section>

      <Section title="Yours to keep">
        <p>
          All of shed's state is in that one SQLite file, which it backs up like everything else,
          with secrets encrypted. The GitHub App asks only for read access, so it can never write to
          your repositories. Every change and sign-in goes into an audit log.
        </p>
        <p>
          Updates are signature-checked before they run, and the previous binary stays next to the
          new one in case you want it back. And the whole thing is free and{" "}
          <a href={repoUrl} className={styles.inline}>
            open source
          </a>
          .
        </p>
      </Section>

      <Section title="Try it on a spare server">
        <p>
          Run this, open the setup link it prints, and let shed create its GitHub App in a couple of
          clicks. Then add a repository and push.
        </p>
        <Install />
        <nav className={styles.buttons} aria-label="Docs">
          <LinkButton variant="primary" href="/docs/installation">
            Installation guide
          </LinkButton>
          <LinkButton variant="secondary" href="/docs/concepts">
            Projects and services
          </LinkButton>
          <LinkButton variant="secondary" href="/docs/github">
            The GitHub App
          </LinkButton>
        </nav>
      </Section>
    </article>
  );
}

function Section({ title, children }: { title?: string; children: ReactNode }) {
  return (
    <section className={title ? styles.section : styles.continued}>
      {title && <h2 className={styles.h2}>{title}</h2>}
      {children}
    </section>
  );
}

function Figure({
  caption,
  wide,
  children,
}: {
  caption: string;
  wide?: boolean;
  children: ReactNode;
}) {
  return (
    <figure className={wide ? styles.wide : styles.figure}>
      {children}
      <figcaption className={styles.caption}>{caption}</figcaption>
    </figure>
  );
}

function DocLink({
  slug,
  anchor,
  children,
}: {
  slug: string;
  anchor?: string;
  children: ReactNode;
}) {
  return (
    <Link to="/docs/$slug" params={{ slug }} hash={anchor} className={styles.link}>
      {children}
      <ArrowRightIcon size={14} aria-hidden />
    </Link>
  );
}

/** pipe splits the install command so narrow screens wrap it before "| sudo bash". */
const pipe = installCommand.indexOf(" | ");

function Install({ id }: { id?: string }) {
  return (
    <div id={id} className={styles.install}>
      <code className={styles.command}>
        <span className={styles.prompt} aria-hidden>
          ${" "}
        </span>
        {installCommand.slice(0, pipe)}{" "}
        <span className={styles.keep}>{installCommand.slice(pipe + 1)}</span>
      </code>
      <CopyButton value={installCommand} label="Copy install command" />
    </div>
  );
}

function Limit({ title, children }: { title: string; children: ReactNode }) {
  return (
    <li className={styles.limit}>
      <strong>{title}</strong> {children}
    </li>
  );
}

const COLUMNS = "64px minmax(0, 1fr) 92px 112px 72px 84px";
const NARROW = "60px minmax(0, 1fr) auto";

function History() {
  return (
    <RecordList title="History">
      {deployments.map((d) => (
        <RecordRow key={d.sha} columns={COLUMNS} narrowColumns={NARROW}>
          <RecordId>{d.sha}</RecordId>
          <RecordMessage title={d.message}>{d.message}</RecordMessage>
          <span className={styles.status}>
            <StatusBadge kind="deployment" status={d.status} />
          </span>
          <RecordMeta optional>
            <Pill>{d.trigger}</Pill>
          </RecordMeta>
          <RecordMeta optional>by {d.author}</RecordMeta>
          <RecordMeta optional className={styles.when}>
            {d.when}
          </RecordMeta>
        </RecordRow>
      ))}
    </RecordList>
  );
}

function Variables() {
  return (
    <LayerCard className={styles.card}>
      <LayerCard.Secondary className={styles.cardHead}>
        <span>Variables</span>
        <span className={styles.cardMeta}>web</span>
      </LayerCard.Secondary>
      <LayerCard.Primary className={styles.cardBody}>
        {variables.map((v) => (
          <div key={v.key} className={styles.var}>
            <span className={styles.varKey}>{v.key}</span>
            <span className={styles.varValue}>
              <VarValue value={v.value} />
            </span>
          </div>
        ))}
        <div className={styles.resolved}>
          <span className={styles.resolvedLabel}>At deploy, {resolved.key} becomes</span>
          <code className={styles.resolvedValue}>
            {resolved.before}
            <span className={styles.masked}>{resolved.secret}</span>
            {resolved.after}
          </code>
        </div>
      </LayerCard.Primary>
    </LayerCard>
  );
}

const presets = [
  { value: "hourly", label: "Hourly" },
  { value: "daily", label: "Daily" },
  { value: "weekly", label: "Weekly" },
];

function BackupPolicy() {
  const [scheduled, setScheduled] = useState(true);
  const [preset, setPreset] = useState("daily");
  const [upload, setUpload] = useState(true);
  const when = {
    hourly: "Every hour, on the hour",
    daily: "Every day at 03:00 UTC",
    weekly: "Every Sunday at 03:00 UTC",
  }[preset];
  return (
    <Rows>
      <SettingRow
        label="Scheduled backups"
        description="Back up automatically on a schedule."
        control={
          <Switch
            checked={scheduled}
            onCheckedChange={setScheduled}
            aria-label="Scheduled backups"
          />
        }
      >
        {scheduled && (
          <Well>
            <div className={styles.nested}>
              <div className={styles.nestedText}>
                <span className={styles.nestedLabel}>Schedule</span>
                <span className={styles.hint}>{when}</span>
              </div>
              <Tabs
                variant="segmented"
                size="sm"
                value={preset}
                onValueChange={(v) => setPreset(v as string)}
                tabs={presets}
              />
            </div>
          </Well>
        )}
      </SettingRow>
      <SettingRow
        label="Upload to S3"
        description={
          upload ? "Keep 7 on disk and 30 in the bucket." : "Keep the newest 7 on disk only."
        }
        control={<Switch checked={upload} onCheckedChange={setUpload} aria-label="Upload to S3" />}
      />
    </Rows>
  );
}

/**
 * Stats draws the dashboard's stat strip once mounted. Its charts measure
 * the page, so the prerendered page holds a quiet frame of the same height.
 */
function Stats() {
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);
  if (!mounted) return <div className={styles.statsHold} aria-hidden />;
  return <StatStrip stats={stats} start={statsStart} step={statsStep} />;
}
