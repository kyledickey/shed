import { LinkSimpleIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import type { MDXComponents, MDXContent } from "mdx/types";
import { isValidElement, type ComponentProps, type ReactNode } from "react";
import { AgentPrompt } from "./AgentPrompt";
import { HelpTip } from "./HelpTip";
import { Callout, CodeBlock, Demo, Figure, Term } from "./kit";
import kit from "./kit.module.css";
import styles from "./mdx.module.css";
import type { DocMeta } from "./registry";

/** Anchor is the link to a heading that appears beside it on hover. */
function Anchor({ id }: { id?: string }) {
  return (
    <a href={`#${id}`} className={styles.anchor} aria-label="Link to this section">
      <LinkSimpleIcon size={16} />
    </a>
  );
}

/** Heading2 is a section heading with a hover anchor link. */
function Heading2({ id, children }: ComponentProps<"h2">) {
  return (
    <h2 id={id} className={styles.h2}>
      {children}
      <Anchor id={id} />
    </h2>
  );
}

/** Heading3 is a subsection heading with a hover anchor link. */
function Heading3({ id, children }: ComponentProps<"h3">) {
  return (
    <h3 id={id} className={styles.h3}>
      {children}
      <Anchor id={id} />
    </h3>
  );
}

/** DocLink routes /docs/... and other in-app links client-side and opens the rest in a new tab. */
function DocLink({ href = "", children }: ComponentProps<"a">) {
  if (href.startsWith("#")) return <a href={href}>{children}</a>;
  if (href.startsWith("/")) {
    const [path = "", hash] = href.split("#");
    return (
      <Link to={path} hash={hash}>
        {children}
      </Link>
    );
  }
  return (
    <a href={href} target="_blank" rel="noreferrer">
      {children}
    </a>
  );
}

/** metaTitle reads title="..." from a fenced code block's meta string. */
function metaTitle(meta: unknown): string | undefined {
  return typeof meta === "string" ? /title="([^"]*)"/.exec(meta)?.[1] : undefined;
}

/** Pre renders a fenced code block through CodeBlock. */
function Pre({ children }: { children?: ReactNode }) {
  if (
    !isValidElement<{ className?: string; children?: ReactNode; "data-meta"?: string }>(children)
  ) {
    return <pre>{children}</pre>;
  }
  const { className = "", children: code, "data-meta": meta } = children.props;
  if (typeof code !== "string") return <pre>{children}</pre>;
  const lang = /language-(\S+)/.exec(className)?.[1];
  return (
    <CodeBlock title={metaTitle(meta)} lang={lang}>
      {code}
    </CodeBlock>
  );
}

/** Table wraps a Markdown table in the docs table frame. */
function Table(props: ComponentProps<"table">) {
  return (
    <div className={kit.tableWrap}>
      <table className={kit.table} {...props} />
    </div>
  );
}

/**
 * mdxComponents are available in every page without an import: the
 * Markdown element overrides plus the shared building blocks.
 */
const mdxComponents: MDXComponents = {
  h2: Heading2,
  h3: Heading3,
  a: DocLink,
  pre: Pre,
  table: Table,
  AgentPrompt,
  Callout,
  CodeBlock,
  Demo,
  Figure,
  HelpTip,
  Term,
};

/** MdxPage renders a docs page: title, lede, then the MDX body. */
export function MdxPage({ meta, Content }: { meta: DocMeta; Content: MDXContent }) {
  return (
    <article className={styles.doc}>
      <header className={styles.head}>
        <h1 className={styles.title}>{meta.title}</h1>
        {meta.description && <p className={styles.lede}>{meta.description}</p>}
      </header>
      <div className={styles.body}>
        <Content components={mdxComponents} />
      </div>
    </article>
  );
}
