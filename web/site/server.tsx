import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { renderToString } from "react-dom/server";
import { docPages } from "../src/features/docs/registry";
import { headFor, type Head } from "./head";
import { createSiteRouter } from "./router";

/** paths lists every page to prerender. */
export const paths: string[] = ["/", ...docPages.map((p) => `/docs/${p.slug}`)];

/** render renders the page at path to the HTML that goes inside #root. */
export async function render(path: string): Promise<{ html: string; head: Head }> {
  const router = createSiteRouter(createMemoryHistory({ initialEntries: [path] }));
  await router.load();
  const leaf = router.state.matches.at(-1);
  if (!leaf || leaf.routeId === "__root__" || leaf.status !== "success") {
    throw new Error(`site: render ${path}: ${leaf?.status ?? "no match"}`);
  }
  return { html: renderToString(<RouterProvider router={router} />), head: headFor(path) };
}
