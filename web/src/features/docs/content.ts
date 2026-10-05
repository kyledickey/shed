import type { MDXContent } from "mdx/types";

const modules = import.meta.glob<{ default: MDXContent }>("./content/*.mdx");

/** loadDoc imports the compiled page for slug, or undefined if there is none. */
export async function loadDoc(slug: string): Promise<MDXContent | undefined> {
  const load = modules[`./content/${slug}.mdx`];
  return load ? (await load()).default : undefined;
}
