import { describe, expect, it } from "vite-plus/test";
import { importedBy, packages } from "./graph";

describe("package graph", () => {
  it("lists importers", () => {
    expect(importedBy("store")).toEqual(["api", "backup", "control", "deploy", "metrics"]);
    expect(importedBy("control")).toEqual(["api", "mcp"]);
    expect(importedBy("mcp")).toEqual([]);
    expect(importedBy("api")).toEqual([]);
  });

  it("only points at known packages", () => {
    const names = new Set(packages.map((p) => p.name));
    for (const p of packages) for (const i of p.imports) expect(names.has(i)).toBe(true);
  });
});
