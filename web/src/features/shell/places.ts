/** Place is what a dashboard location points at, parsed from its path. */
export type Place =
  | { kind: "home" }
  | { kind: "project"; projectId: string; section?: string }
  | { kind: "service"; projectId: string; serviceId: string; section?: string }
  | { kind: "server"; section?: string }
  | { kind: "docs"; slug?: string }
  | { kind: "other"; path: string };

/** parsePlace maps an href (path, search, and hash) to the place it shows. */
export function parsePlace(href: string): Place {
  const path = (href.split(/[?#]/)[0] ?? "").replace(/\/+$/, "") || "/";
  const parts = path.split("/").filter(Boolean).map(decodeURIComponent);
  const [root, a, b, c, d] = parts;
  if (parts.length === 0) return { kind: "home" };
  if (root === "projects" && a) {
    if (b === "services" && c) return { kind: "service", projectId: a, serviceId: c, section: d };
    return { kind: "project", projectId: a, section: b };
  }
  if (root === "server") return { kind: "server", section: a };
  if (root === "docs") return { kind: "docs", slug: a };
  return { kind: "other", path };
}

/** sectionLabel turns a path segment such as "env-vars" into "Env vars". */
export function sectionLabel(section: string): string {
  const words = section.replace(/-/g, " ");
  return words.charAt(0).toUpperCase() + words.slice(1);
}
