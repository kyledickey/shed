import { Fragment, type ReactNode } from "react";
import styles from "./ReleaseNotes.module.css";

type Block =
  | { kind: "heading"; level: number; text: string }
  | { kind: "list"; items: string[] }
  | { kind: "paragraph"; text: string };

/** parse splits Markdown release notes into headings, bullet lists, and paragraphs. */
function parse(markdown: string): Block[] {
  const blocks: Block[] = [];
  let open: Block | undefined; // the block an unmarked line continues
  for (const raw of markdown.split(/\r?\n/)) {
    const line = raw.trim();
    const heading = /^(#{1,6})\s+(.*)$/.exec(line);
    const bullet = /^[-*+]\s+(.*)$/.exec(line);
    if (!line) {
      open = undefined;
    } else if (heading) {
      const [, hashes = "", text = ""] = heading;
      blocks.push({ kind: "heading", level: hashes.length, text });
      open = undefined;
    } else if (bullet) {
      if (open?.kind !== "list") {
        open = { kind: "list", items: [] };
        blocks.push(open);
      }
      open.items.push(bullet[1] ?? "");
    } else if (open?.kind === "list") {
      open.items[open.items.length - 1] += ` ${line}`;
    } else if (open?.kind === "paragraph") {
      open.text += ` ${line}`;
    } else {
      open = { kind: "paragraph", text: line };
      blocks.push(open);
    }
  }
  return blocks;
}

const INLINE = /`([^`]+)`|\*\*(.+?)\*\*|\[([^\]]+)\]\(([^)\s]+)\)/g;

/** inline renders `code`, **bold**, and [links](https://…) in a line of text. */
function inline(text: string): ReactNode[] {
  const out: ReactNode[] = [];
  let at = 0;
  for (const m of text.matchAll(INLINE)) {
    const [whole, code, bold, label = "", url = ""] = m;
    if (m.index > at) out.push(text.slice(at, m.index));
    if (code !== undefined) {
      out.push(<code key={m.index}>{code}</code>);
    } else if (bold !== undefined) {
      out.push(<strong key={m.index}>{inline(bold)}</strong>);
    } else if (/^https?:\/\//i.test(url)) {
      out.push(
        <a key={m.index} href={url} target="_blank" rel="noreferrer noopener">
          {inline(label)}
        </a>,
      );
    } else {
      out.push(<Fragment key={m.index}>{inline(label)}</Fragment>);
    }
    at = m.index + whole.length;
  }
  if (at < text.length) out.push(text.slice(at));
  return out;
}

/** ReleaseNotes renders a release's Markdown notes: headings, bullets, bold, code, and links. */
export function ReleaseNotes({ notes }: { notes: string }) {
  return (
    <div className={styles.notes}>
      {parse(notes).map((b, i) => {
        switch (b.kind) {
          case "heading":
            return b.level <= 3 ? (
              <h4 key={i}>{inline(b.text)}</h4>
            ) : (
              <h5 key={i}>{inline(b.text)}</h5>
            );
          case "list":
            return (
              <ul key={i}>
                {b.items.map((item, j) => (
                  <li key={j}>{inline(item)}</li>
                ))}
              </ul>
            );
          case "paragraph":
            return <p key={i}>{inline(b.text)}</p>;
        }
      })}
    </div>
  );
}
