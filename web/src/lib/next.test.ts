import { describe, expect, it } from "vite-plus/test";
import { safeNext } from "./next";

describe("safeNext", () => {
  it.each([
    ["/authorize?request=abc", "/authorize?request=abc"],
    ["/oauth/authorize?client_id=x&state=y", "/oauth/authorize?client_id=x&state=y"],
    ["/", "/"],
    ["", undefined],
    ["authorize", undefined],
    ["//evil.example", undefined],
    ["/\\evil.example", undefined],
    ["/\t/evil.example", undefined],
    ["/\n/evil.example", undefined],
    ["https://evil.example/", undefined],
    ["javascript:alert(1)", undefined],
    [42, undefined],
    [undefined, undefined],
  ])("%j → %j", (input, want) => {
    expect(safeNext(input)).toBe(want);
  });
});
