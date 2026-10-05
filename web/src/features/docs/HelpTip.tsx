import { Link } from "@tanstack/react-router";
import { CircleHelp } from "lucide-react";
import { Tooltip } from "../../components/Overlay";
import styles from "./HelpTip.module.css";
import { docTopics, type DocTopic } from "./topics";

/**
 * HelpTip is a small ? that summarizes a concept on hover and opens its docs
 * section on click. Place it right after a title or label.
 */
export function HelpTip({ topic }: { topic: DocTopic }) {
  const t = docTopics[topic];
  return (
    <Tooltip
      content={
        <span className={styles.content}>
          {t.summary}
          <span className={styles.more}>Click to read the docs</span>
        </span>
      }
    >
      <Link
        to="/docs/$slug"
        params={{ slug: t.page }}
        hash={t.section}
        className={styles.tip}
        aria-label={`Docs: ${t.summary}`}
        onClick={(e) => e.stopPropagation()}
      >
        <CircleHelp size={13} />
      </Link>
    </Tooltip>
  );
}
