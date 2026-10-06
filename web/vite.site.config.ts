/// <reference types="node" />
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vite-plus";
import { docsPlugins } from "./docs-plugin";

const path = (p: string) => fileURLToPath(new URL(p, import.meta.url));

// The footer shows the newest release in CHANGELOG.md, which every release
// adds a section to before it's tagged.
const version = readFileSync(path("../CHANGELOG.md"), "utf8").match(/^## (v\S+)/m)?.[1];
if (!version) throw new Error("site: CHANGELOG.md has no release section");

/**
 * The public site: the home page and the docs. The client build goes to
 * site-dist/client and the server build, used only to prerender every page
 * to HTML (site/prerender.ts), to site-dist/server.
 */
export default defineConfig(({ isSsrBuild }) => ({
  root: path("./site"),
  publicDir: path("./public"),
  plugins: [
    ...docsPlugins(path("./src/features/docs/content")),
    tanstackRouter({
      target: "react",
      routesDirectory: path("./site/routes"),
      generatedRouteTree: path("./site/routeTree.gen.ts"),
      // The prerenderer needs every route in one bundle.
      autoCodeSplitting: !isSsrBuild,
    }),
    react({ include: /\.(mdx|tsx?)$/ }),
  ],
  define: { __SHED_VERSION__: JSON.stringify(version) },
  build: {
    // One stylesheet, linked from every prerendered page, so a page's CSS
    // doesn't wait for its route chunk and the HTML never paints unstyled.
    cssCodeSplit: false,
    outDir: path(isSsrBuild ? "./site-dist/server" : "./site-dist/client"),
    emptyOutDir: true,
  },
}));
