import { describe, expect, it } from "vite-plus/test";
import { parseScope, traceResolve } from "./trace";

const sample = parseScope(`web.URL=pg://\${{ db.HOST }}/\${{ NAME }}
web.NAME=app
db.HOST=\${{ SHED_PRIVATE_DOMAIN }}
db.SHED_PRIVATE_DOMAIN=db`);

describe("traceResolve", () => {
  it("walks references depth first and memoizes", () => {
    const steps = traceResolve("web", sample);
    expect(steps.map((s) => `${s.kind}:${s.ref}`)).toEqual([
      "enter:web.NAME",
      "done:web.NAME",
      "enter:web.URL",
      "enter:db.HOST",
      "enter:db.SHED_PRIVATE_DOMAIN",
      "done:db.SHED_PRIVATE_DOMAIN",
      "done:db.HOST",
      "memo:web.NAME",
      "done:web.URL",
    ]);
    expect(steps.at(-1)?.memo).toContainEqual(["web.URL", "pg://db/app"]);
  });

  it("reports a cycle with the stack", () => {
    const steps = traceResolve("web", parseScope("web.A=${{ B }}\nweb.B=${{ A }}"));
    expect(steps.at(-1)).toMatchObject({
      kind: "cycle",
      note: "reference cycle: web.A -> web.B -> web.A",
    });
  });

  it("expands a missing variable to nothing", () => {
    const steps = traceResolve("web", parseScope("web.A=x${{ NOPE }}y"));
    expect(steps.map((s) => s.kind)).toEqual(["enter", "missing", "done"]);
    expect(steps.at(-1)?.memo).toEqual([["web.A", "xy"]]);
  });
});
