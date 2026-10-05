/// <reference types="node" />
/**
 * prerender writes every page of the site to static HTML so crawlers see the
 * content without running JavaScript, along with sitemap.xml and robots.txt.
 * It runs after both Vite builds (see the site:build script).
 */
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { siteUrl } from "./links";

type Server = typeof import("./server");

const outDir = fileURLToPath(new URL("../site-dist/client", import.meta.url));
const serverEntry = fileURLToPath(new URL("../site-dist/server/server.js", import.meta.url));

const escape = (s: string) =>
  s.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

const { paths, render } = (await import(serverEntry)) as Server;
const template = readFileSync(join(outDir, "index.html"), "utf8");

for (const path of paths) {
  const { html, head } = await render(path);
  const tags = [
    `<meta property="og:title" content="${escape(head.title)}" />`,
    `<meta property="og:description" content="${escape(head.description)}" />`,
    `<meta property="og:type" content="website" />`,
    `<meta property="og:url" content="${siteUrl}${path}" />`,
    `<link rel="canonical" href="${siteUrl}${path}" />`,
  ];
  const page = template
    .replace(/<title>.*<\/title>/, `<title>${escape(head.title)}</title>`)
    .replace(
      '<meta name="description" content="" />',
      `<meta name="description" content="${escape(head.description)}" />`,
    )
    .replace("<!--head-->", tags.join("\n    "))
    .replace("<!--app-->", html);
  // /docs/x is served from /docs/x.html; / is index.html.
  const file = join(outDir, path === "/" ? "index.html" : `${path}.html`);
  mkdirSync(dirname(file), { recursive: true });
  writeFileSync(file, page);
}

const urls = paths.map((p) => `  <url><loc>${siteUrl}${p}</loc></url>`).join("\n");
writeFileSync(
  join(outDir, "sitemap.xml"),
  `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${urls}\n</urlset>\n`,
);
writeFileSync(
  join(outDir, "robots.txt"),
  `User-agent: *\nAllow: /\nSitemap: ${siteUrl}/sitemap.xml\n`,
);
console.log(`prerender: wrote ${paths.length} pages`);
