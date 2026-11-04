import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { hostMetricsQuery } from "../../api/metrics";
import { updateQuery } from "../../api/update";
import { formatBytes } from "../../lib/format";
import { Suspense } from "react";
import { Skeleton } from "../../components/Misc";
import { cx } from "../../lib/cx";
import { serverSections, type SectionProps, type ServerSection } from "./sections";
import styles from "./ServerPage.module.css";

const groups = [
  { label: "Monitor", sections: serverSections.filter((s) => s.group === "monitor") },
  { label: "Configure", sections: serverSections.filter((s) => s.group === "configure") },
];

/**
 * ServerPage is the machine shed runs on and shed itself: a list of sections
 * on the left, the chosen one on the right.
 */
export function ServerPage({ section, ...props }: { section: ServerSection } & SectionProps) {
  const { Component } = section;
  return (
    <div className={styles.layout}>
      <nav className={styles.nav} aria-label="Server sections">
        <div className={styles.identity}>
          <h1 className={styles.title}>Server</h1>
          <ServerFacts />
        </div>
        {groups.map((g) => (
          <div key={g.label} className={styles.group}>
            <span className={styles.groupLabel}>{g.label}</span>
            {g.sections.map((s) => (
              <Link
                key={s.id}
                to="/server/$section"
                params={{ section: s.id }}
                className={cx(styles.item, s.id === section.id && styles.active)}
                aria-current={s.id === section.id ? "page" : undefined}
              >
                {s.title}
              </Link>
            ))}
          </div>
        ))}
      </nav>
      <div className={styles.content}>
        <div className={styles.head}>
          <h2 className={styles.sectionTitle}>{section.title}</h2>
          <p className={styles.description}>{section.description}</p>
        </div>
        <Suspense fallback={<Skeleton height={240} radius={8} />}>
          <Component {...props} />
        </Suspense>
      </div>
    </div>
  );
}

/**
 * ServerFacts are quiet lines under the title about this machine. Each line
 * appears once its data loads, so new facts can join without layout work.
 */
function ServerFacts() {
  const update = useQuery({ ...updateQuery, retry: false });
  const host = useQuery(hostMetricsQuery("1h"));
  const facts = [
    update.data && `shed ${update.data.current}`,
    host.data && `${host.data.cpus} vCPU · ${formatBytes(host.data.memoryTotal)} memory`,
    host.data && `${formatBytes(host.data.diskTotal)} disk`,
  ].filter(Boolean);
  if (facts.length === 0) return null;
  return (
    <ul className={styles.facts}>
      {facts.map((f) => (
        <li key={String(f)}>{f}</li>
      ))}
    </ul>
  );
}
