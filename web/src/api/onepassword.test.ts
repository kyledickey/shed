import { describe, expect, it } from "vite-plus/test";
import { noVaultsMessage, planConnect } from "./onepassword";

const a = { id: "a1", name: "Alpha" };
const b = { id: "b2", name: "Beta" };

describe("planConnect", () => {
  it("has nothing to connect to without vaults", () => {
    expect(planConnect([])).toEqual({ kind: "none" });
    expect(noVaultsMessage).toContain("can't access any vaults");
  });

  it("connects directly with a single vault's id", () => {
    expect(planConnect([a])).toEqual({ kind: "connect", vault: "a1" });
  });

  it("asks to choose among several, preselecting the first", () => {
    expect(planConnect([a, b])).toEqual({ kind: "choose", vaults: [a, b], vault: "a1" });
  });
});
