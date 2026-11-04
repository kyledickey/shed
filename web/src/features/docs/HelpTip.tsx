import { Tooltip } from "@cloudflare/kumo";
import { ArrowRightIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import styles from "./HelpTip.module.css";
import { docTopics, type DocTopic } from "./topics";

/**
 * HelpTip marks a word or label as explained in the docs: the text gets a
 * dashed underline, hovering or focusing it summarizes the concept, and
 * clicking it opens the docs section.
 */
export function HelpTip({ topic, children }: { topic: DocTopic; children: ReactNode }) {
  const t = docTopics[topic];
  const docs = { to: "/docs/$slug", params: { slug: t.page }, hash: t.section } as const;
  return (
    <Tooltip
      content={
        <span className={styles.content}>
          {t.summary}
          <Link {...docs} className={styles.more} onClick={(e) => e.stopPropagation()}>
            Read the docs <ArrowRightIcon size={12} />
          </Link>
        </span>
      }
      render={
        <Link {...docs} className={styles.tip} onClick={(e) => e.stopPropagation()}>
          {children}
        </Link>
      }
    />
  );
}
