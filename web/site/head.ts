import { docPage } from "../src/features/docs/registry";

/** Head is a page's title and meta description. */
export type Head = { title: string; description: string };

const home: Head = {
  title: "shed: a self-hosted deployment platform for one server",
  description:
    "shed builds your GitHub repos, runs them in Docker next to their databases, puts them behind HTTPS, and backs them up. One Go binary on a Linux server you control.",
};

/** headFor returns the head of the page at pathname. */
export function headFor(pathname: string): Head {
  const slug = /^\/docs\/([^/]+)\/?$/.exec(pathname)?.[1];
  const page = slug ? docPage(slug) : undefined;
  if (!page) return home;
  return { title: `${page.title} · shed docs`, description: page.description };
}
