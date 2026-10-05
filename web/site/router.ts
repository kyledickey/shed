import { createRouter, type RouterHistory } from "@tanstack/react-router";
import { routeTree } from "./routeTree.gen";

/** createSiteRouter builds the site's router on history. */
export function createSiteRouter(history?: RouterHistory) {
  // Scroll restoration renders an inline script on the server that the client
  // doesn't, which would break hydration; it's only needed in the browser.
  return createRouter({ routeTree, history, scrollRestoration: typeof window !== "undefined" });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createSiteRouter>;
  }
}
