/// <reference types="node" />
import mdx from "@mdx-js/rollup";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";
import remarkFrontmatter from "remark-frontmatter";
import remarkGfm from "remark-gfm";
import { defineConfig } from "vite-plus";
import { docsMeta, remarkDocs } from "./docs-plugin";

const docsDir = fileURLToPath(new URL("./src/features/docs/content", import.meta.url));

export default defineConfig({
  plugins: [
    docsMeta(docsDir),
    {
      enforce: "pre",
      ...mdx({ remarkPlugins: [remarkFrontmatter, remarkGfm, remarkDocs] }),
    },
    tanstackRouter({ target: "react", autoCodeSplitting: true }),
    react({ include: /\.(mdx|tsx?)$/ }),
  ],
  server: {
    proxy: {
      "/api": "http://127.0.0.1:3000",
    },
  },
  fmt: {
    ignorePatterns: ["dist/**", "src/routeTree.gen.ts"],
  },
  lint: {
    ignorePatterns: ["dist/**", "src/routeTree.gen.ts"],
    jsPlugins: [{ name: "vite-plus", specifier: "vite-plus/oxlint-plugin" }],
    rules: { "vite-plus/prefer-vite-plus-imports": "error" },
    options: { typeAware: true, typeCheck: true },
  },
});
