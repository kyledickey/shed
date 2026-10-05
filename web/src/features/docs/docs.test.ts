/// <reference types="node" />
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vite-plus/test";
import { readAllMeta } from "../../../docs-plugin";
import { docTopics } from "./topics";

const dir = fileURLToPath(new URL("./content", import.meta.url));
const pages = readAllMeta(dir);
const anchors = new Map(pages.map((p) => [p.slug, new Set(p.sections.map((s) => s.id))]));

/** exists reports whether /docs/<slug>#<id> points at a real page and h2. */
function exists(slug: string, id?: string): boolean {
  const ids = anchors.get(slug);
  return !!ids && (!id || ids.has(id));
}

describe("docs", () => {
  it("has unique, ordered pages", () => {
    const orders = pages.map((p) => p.order);
    expect(new Set(orders).size).toBe(orders.length);
  });

  it.each(Object.entries(docTopics))("topic %s points at a heading", (_, t) => {
    expect(exists(t.page, t.section), `${t.page}#${t.section}`).toBe(true);
  });

  const links = readdirSync(dir)
    .filter((f) => f.endsWith(".mdx"))
    .flatMap((f) =>
      [...readFileSync(join(dir, f), "utf8").matchAll(/\]\(\/docs\/([\w-]+)(?:#([\w-]+))?\)/g)].map(
        (m) => [f, m[1] ?? "", m[2]] as const,
      ),
    );

  it.each(links)("%s links to /docs/%s#%s", (_, slug, id) => {
    expect(exists(slug, id)).toBe(true);
  });
});
