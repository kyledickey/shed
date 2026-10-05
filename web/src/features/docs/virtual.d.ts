declare module "virtual:docs" {
  /** Every docs page, sorted by frontmatter order. */
  export const pages: import("../../../docs-plugin").DocMeta[];
}
