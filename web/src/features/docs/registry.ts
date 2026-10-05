import { pages } from "virtual:docs";
import type { DocMeta } from "../../../docs-plugin";

export type { DocMeta };

/**
 * The docs table of contents, read at build time from the frontmatter and
 * h2 headings of content/*.mdx (see docs-plugin.ts).
 */
export const docPages: readonly DocMeta[] = pages;

/** DocGroup is a sidebar group and its pages, in order. */
export type DocGroup = { title: string; pages: DocMeta[] };

export const docGroups: readonly DocGroup[] = pages.reduce<DocGroup[]>((groups, page) => {
  const group = groups.find((g) => g.title === page.group);
  if (group) group.pages.push(page);
  else groups.push({ title: page.group, pages: [page] });
  return groups;
}, []);

/** isDocSlug reports whether s names a docs page. */
export function isDocSlug(s: string): boolean {
  return pages.some((p) => p.slug === s);
}

/** docPage returns the metadata of page slug, if it exists. */
export function docPage(slug: string): DocMeta | undefined {
  return pages.find((p) => p.slug === slug);
}
