/// <reference types="node" />
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vite-plus";
import { docsPlugins } from "./docs-plugin";

const path = (p: string) => fileURLToPath(new URL(p, import.meta.url));

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
  build: {
    outDir: path(isSsrBuild ? "./site-dist/server" : "./site-dist/client"),
    emptyOutDir: true,
  },
}));
