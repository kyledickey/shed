/** Package is one Go package under internal/ and the internal packages it imports. */
export type Package = { name: string; imports: string[] };

/** packages mirrors the imports `go list` reports for ./internal/... (cmd/shed imports all of them). */
export const packages: Package[] = [
  {
    name: "api",
    imports: ["auth", "backup", "catalog", "deploy", "github", "logtail", "metrics", "store"],
  },
  { name: "auth", imports: ["github"] },
  { name: "backup", imports: ["docker", "store"] },
  { name: "build", imports: [] },
  { name: "catalog", imports: [] },
  { name: "config", imports: [] },
  { name: "deploy", imports: ["build", "catalog", "docker", "github", "proxy", "store", "vars"] },
  { name: "docker", imports: [] },
  { name: "github", imports: [] },
  { name: "host", imports: [] },
  { name: "logtail", imports: [] },
  { name: "metrics", imports: ["docker", "host", "store"] },
  { name: "proxy", imports: [] },
  { name: "s3", imports: [] },
  { name: "store", imports: [] },
  { name: "vars", imports: [] },
];

/** importedBy returns the internal packages that import name. */
export function importedBy(name: string): string[] {
  return packages.filter((p) => p.imports.includes(name)).map((p) => p.name);
}
