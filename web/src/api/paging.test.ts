import { describe, expect, it } from "vite-plus/test";
import { pagePath } from "./paging";

describe("pagePath", () => {
  it("first page", () => {
    expect(pagePath("/services/s/deployments", null)).toBe("/services/s/deployments?limit=10");
  });
  it("a later page", () => {
    expect(pagePath("/backups/system", "bak_1", 50)).toBe("/backups/system?limit=50&before=bak_1");
  });
});
