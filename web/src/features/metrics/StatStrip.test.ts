import { describe, expect, it } from "vite-plus/test";
import { columns } from "./StatStrip";

describe("columns", () => {
  it.each([
    [4, 1200, 4],
    [4, 800, 2],
    [4, 400, 1],
    [3, 700, 3],
    [3, 500, 1],
    [2, 500, 2],
    [6, 700, 3],
    [1, 1200, 1],
  ])("%i stats in %ipx", (n, width, want) => {
    expect(columns(n, width)).toBe(want);
  });
});
