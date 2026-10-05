import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot, hydrateRoot } from "react-dom/client";
import "../src/styles/global.css";
import { createSiteRouter } from "./router";

const router = createSiteRouter();
const root = document.getElementById("root")!;
const app = (
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>
);

// Prerendered pages hydrate once their route code and doc have loaded.
// Marking the router as server-rendered makes it render the same tree as
// the prerenderer, without the Suspense boundary of a client-only app.
if (root.hasChildNodes()) {
  router.ssr = { manifest: undefined };
  void router.load().then(() => hydrateRoot(root, app));
} else {
  createRoot(root).render(app);
}
