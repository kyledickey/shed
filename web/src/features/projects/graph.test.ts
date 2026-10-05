import { describe, expect, it } from "vite-plus/test";
import { parseRefs, projectStatus, serviceEdges } from "./graph";

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

  it("dedupes pairs and joins keys", () => {
    const edges = serviceEdges(services, [
      { DB_URL: "${{ postgres.DATABASE_URL }}", DB_HOST: "${{ postgres.HOST }}", PORT: "3000" },
      { POSTGRES_USER: "app" },
      { API_URL: "https://${{ api.SHED_PUBLIC_DOMAIN }}", SELF: "${{ web.PORT }}" },
    ]);
    expect(edges).toEqual([
      { from: "1", to: "2", label: "DB_HOST, DB_URL" },
      { from: "3", to: "1", label: "API_URL" },
    ]);
  });

  it("ignores unknown services and missing variables", () => {
    expect(serviceEdges(services, [{ X: "${{ nope.KEY }}" }, undefined, undefined])).toEqual([]);
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
