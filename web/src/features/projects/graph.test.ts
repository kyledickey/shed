import { describe, expect, it } from "vite-plus/test";
import { lanes, layers, parseRefs, projectStatus, serviceEdges } from "./graph";

describe("parseRefs", () => {
  it.each([
    ["postgres://${{ postgres.USER }}@${{postgres.HOST}}", ["postgres", "postgres"]],
    ["${{ SHED_PUBLIC_DOMAIN }}", []],
    ["https://${{ api.SHED_PUBLIC_DOMAIN }}/v1", ["api"]],
    ["plain", []],
    ["${{ .KEY }}", []],
  ])("%s", (value, want) => {
    expect(parseRefs(value)).toEqual(want);
  });
});

describe("serviceEdges", () => {
  const services = [
    { id: "1", name: "api" },
    { id: "2", name: "postgres" },
    { id: "3", name: "web" },
  ];

  it("dedupes pairs", () => {
    const edges = serviceEdges(services, [
      { DB_URL: "${{ postgres.DATABASE_URL }}", DB_HOST: "${{ postgres.HOST }}", PORT: "3000" },
      { POSTGRES_USER: "app" },
      { API_URL: "https://${{ api.SHED_PUBLIC_DOMAIN }}", SELF: "${{ web.PORT }}" },
    ]);
    expect(edges).toEqual([
      { from: "1", to: "2" },
      { from: "3", to: "1" },
    ]);
  });

  it("ignores unknown services and missing variables", () => {
    expect(serviceEdges(services, [{ X: "${{ nope.KEY }}" }, undefined, undefined])).toEqual([]);
  });
});

describe("layers", () => {
  it.each([
    [
      "chain",
      ["a", "b", "c"],
      [
        ["a", "b"],
        ["b", "c"],
      ],
      [["a"], ["b"], ["c"]],
    ],
    [
      "shared database",
      ["api", "worker", "db"],
      [
        ["api", "db"],
        ["worker", "db"],
      ],
      [["api", "worker"], ["db"]],
    ],
    ["no edges", ["a", "b"], [], [["a", "b"]]],
    [
      "cycle stays bounded",
      ["a", "b"],
      [
        ["a", "b"],
        ["b", "a"],
      ],
      [["a"], ["b"]],
    ],
  ] as const)("%s", (_, ids, pairs, want) => {
    const edges = pairs.map(([from, to]) => ({ from, to }));
    expect(layers([...ids], edges)).toEqual(want);
  });
});

describe("projectStatus", () => {
  it.each([
    [["active", "deploying", "crashed"], "deploying"],
    [["active", "failed"], "failed"],
    [["active", "offline"], "active"],
    [["offline"], "offline"],
    [[], "offline"],
  ] as const)("%j → %s", (statuses, want) => {
    expect(projectStatus(statuses.map((status) => ({ status })))).toBe(want);
  });
});

describe("lanes", () => {
  it.each([
    ["none", [], {}],
    [
      "end to end",
      [
        { key: "a", lo: 0, hi: 1 },
        { key: "b", lo: 1, hi: 4 },
      ],
      { a: 0, b: 0 },
    ],
    [
      "overlapping",
      [
        { key: "a", lo: 0, hi: 3 },
        { key: "b", lo: 1, hi: 2 },
        { key: "c", lo: 2.5, hi: 4 },
      ],
      { a: 0, b: 1, c: 1 },
    ],
  ])("%s", (_, spans, want) => {
    expect(Object.fromEntries(lanes(spans))).toEqual(want);
  });
});
