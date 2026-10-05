/// <reference types="node" />
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vite-plus";
import { docsPlugins } from "./docs-plugin";

const docsDir = fileURLToPath(new URL("./src/features/docs/content", import.meta.url));

export default defineConfig({
  plugins: [
    ...docsPlugins(docsDir),
    tanstackRouter({ target: "react", autoCodeSplitting: true }),
    react({ include: /\.(mdx|tsx?)$/ }),
  ],
  server: {
    proxy: {
      "/api": "http://127.0.0.1:3000",
    },
  },
  fmt: {
    ignorePatterns: ["dist/**", "site-dist/**", "src/routeTree.gen.ts", "site/routeTree.gen.ts"],
  },
  lint: {
    ignorePatterns: ["dist/**", "site-dist/**", "src/routeTree.gen.ts", "site/routeTree.gen.ts"],
    jsPlugins: [{ name: "vite-plus", specifier: "vite-plus/oxlint-plugin" }],
    rules: { "vite-plus/prefer-vite-plus-imports": "error" },
    options: { typeAware: true, typeCheck: true },
  },
});
