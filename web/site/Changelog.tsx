import { ReleaseNotes } from "../src/features/settings/ReleaseNotes";
import styles from "./Changelog.module.css";
import { repoUrl } from "./links";

type Release = { version: string; date: string; notes: string };

/** releases splits CHANGELOG.md into its release sections, newest first. */
export function releases(markdown: string): Release[] {
  return markdown
    .split(/^## /m)
    .slice(1)
    .flatMap((section) => {
      const [head = "", ...body] = section.split("\n");
      const m = /^(v\S+)\s+-\s+(\S+)/.exec(head.trim());
      return m ? [{ version: m[1]!, date: m[2]!, notes: body.join("\n").trim() }] : [];
    });
}

const formatDate = (iso: string) =>
  new Date(`${iso}T00:00:00Z`).toLocaleDateString("en-US", {
    year: "numeric",
    month: "long",
    day: "numeric",
    timeZone: "UTC",
  });

/** Changelog is every release in CHANGELOG.md, newest first. */
export function Changelog() {
  const list = releases(__SHED_CHANGELOG__);
  return (
    <div className={styles.page}>
      <header className={styles.head}>
        <h1 className={styles.title}>Changelog</h1>
      </header>
      <ol className={styles.list}>
        {list.map((r, i) => (
          <li key={r.version} id={r.version} className={styles.release}>
            <div className={styles.meta}>
              <a href={`${repoUrl}/releases/tag/${r.version}`} className={styles.version}>
                {r.version}
              </a>
              <time dateTime={r.date} className={styles.date}>
                {formatDate(r.date)}
              </time>
              {i === 0 && <span className={styles.latest}>Latest</span>}
            </div>
            <div className={styles.notes}>
              <ReleaseNotes notes={r.notes} page />
            </div>
          </li>
        ))}
      </ol>
    </div>
  );
}
