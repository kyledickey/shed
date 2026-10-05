/// <reference types="node" />
/**
 * Build support for the in-app docs: a remark plugin that gives headings
 * stable ids, and a Vite plugin that serves every page's frontmatter and
 * sections as the virtual module "virtual:docs".
 */
import { createProcessor } from "@mdx-js/mdx";
import GithubSlugger from "github-slugger";
import type { Heading, Nodes, Root } from "mdast";
import { toString } from "mdast-util-to-string";
import { readdirSync, readFileSync } from "node:fs";
import { basename, join } from "node:path";
import remarkFrontmatter from "remark-frontmatter";
import type { Plugin } from "vite";
import { parse as parseYaml } from "yaml";

/** DocSection is an h2 of a page and its anchor id. */
export type DocSection = { id: string; title: string };

/** DocMeta describes one docs page. */
export type DocMeta = {
  slug: string;
  title: string;
  group: string;
  /** Global sort key; groups appear in the order of their first page. */
  order: number;
  /** The lede under the title. */
  description: string;
  sections: DocSection[];
};

/** headingIds assigns each heading of tree its id, in document order. */
function headingIds(tree: Root): [Heading, string][] {
  const slugger = new GithubSlugger();
  const out: [Heading, string][] = [];
  const walk = (node: Nodes) => {
    if (node.type === "heading") out.push([node, slugger.slug(toString(node))]);
    if ("children" in node) for (const child of node.children) walk(child);
  };
  walk(tree);
  return out;
}

/**
 * remarkDocs sets an id on every heading and passes a fenced code block's
 * meta string (```toml title="shed.toml") to the renderer as data-meta.
 */
export function remarkDocs() {
  return (tree: Root) => {
    for (const [node, id] of headingIds(tree)) {
      node.data = { ...node.data, hProperties: { ...node.data?.hProperties, id } };
    }
    const walk = (node: Nodes) => {
      if (node.type === "code" && node.meta) {
        node.data = { ...node.data, hProperties: { "data-meta": node.meta } };
      }
      if ("children" in node) for (const child of node.children) walk(child);
    };
    walk(tree);
  };
}

const parser = createProcessor({ remarkPlugins: [remarkFrontmatter] });

/** readMeta parses one .mdx page into its metadata. */
export function readMeta(file: string): DocMeta {
  const tree = parser.parse(readFileSync(file, "utf8")) as Root;
  const yaml = tree.children.find((n) => n.type === "yaml");
  const slug = basename(file, ".mdx");
  let front: Partial<DocMeta>;
  try {
    front = (yaml ? parseYaml(yaml.value) : {}) as Partial<DocMeta>;
  } catch (err) {
    throw new Error(
      `docs: ${slug}.mdx: bad frontmatter (quote values that contain a colon): ${String(err)}`,
    );
  }
  for (const key of ["title", "group", "order"] as const) {
    if (front[key] === undefined) throw new Error(`docs: ${slug}.mdx: frontmatter needs ${key}`);
  }
  return {
    slug,
    title: String(front.title),
    group: String(front.group),
    order: Number(front.order),
    description: String(front.description ?? ""),
    sections: headingIds(tree)
      .filter(([node]) => node.depth === 2)
      .map(([node, id]) => ({ id, title: toString(node) })),
  };
}

/** readAllMeta reads every page in dir, sorted by order. */
export function readAllMeta(dir: string): DocMeta[] {
  return readdirSync(dir)
    .filter((f) => f.endsWith(".mdx"))
    .map((f) => readMeta(join(dir, f)))
    .sort((a, b) => a.order - b.order);
}

const virtualId = "virtual:docs";
const resolvedId = `\0${virtualId}`;

/** docsMeta serves `import { pages } from "virtual:docs"` from the pages in dir. */
export function docsMeta(dir: string): Plugin {
  return {
    name: "shed-docs-meta",
    resolveId: (id) => (id === virtualId ? resolvedId : undefined),
    load(id) {
      if (id !== resolvedId) return;
      for (const f of readdirSync(dir)) this.addWatchFile(join(dir, f));
      return `export const pages = ${JSON.stringify(readAllMeta(dir))};`;
    },
    configureServer(server) {
      const reload = (file: string) => {
        if (!file.startsWith(dir) || !file.endsWith(".mdx")) return;
        const mod = server.moduleGraph.getModuleById(resolvedId);
        if (mod) server.moduleGraph.invalidateModule(mod);
        server.ws.send({ type: "full-reload" });
      };
      server.watcher.on("add", reload);
      server.watcher.on("change", reload);
      server.watcher.on("unlink", reload);
    },
  };
}
