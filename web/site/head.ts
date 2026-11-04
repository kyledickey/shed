import { docPage } from "../src/features/docs/registry";
import type { Card } from "./og/card";

/**
 * Head is a page's title and meta description, and what its link preview
 * says (null for the home card).
 */
export type Head = { title: string; description: string; card: Card | null };

const home: Head = {
  title: "shed: a self-hosted deployment platform for one server",
  description:
    "shed builds your GitHub repos, runs them in Docker next to their databases, puts them behind HTTPS, and backs them up. One Go binary on a Linux server you control.",
  card: null,
};

const changelog: Head = {
  title: "Changelog · shed",
  description: "What changed in each release of shed, newest first.",
  card: { kick: "Changelog", title: __SHED_VERSION__ },
};

/** headFor returns the head of the page at pathname. */
export function headFor(pathname: string): Head {
  if (/^\/changelog\/?$/.test(pathname)) return changelog;
  const slug = /^\/docs\/([^/]+)\/?$/.exec(pathname)?.[1];
  const page = slug ? docPage(slug) : undefined;
  if (!page) return home;
  return {
    title: `${page.title} · shed docs`,
    description: page.description,
    card: { kick: "Docs", title: page.title },
  };
}
