import { describe, expect, it } from "vite-plus/test";
import {
  buildScope,
  injectedVars,
  MAX_REFERENCE_DEPTH,
  MAX_RESOLVED_BYTES,
  MAX_VALUE_BYTES,
  parseTarget,
  resolve,
  resolveKey,
  splitRefs,
  type Scope,
  type ServiceFacts,
} from "./vars";

describe("resolve", () => {
  const tests: { name: string; self: string; all: Scope; want: Record<string, string> }[] = [
    {
      name: "plain values untouched",
      self: "web",
      all: { web: { A: "hello", B: "$HOME ${A} {{A}} $${{" } },
      want: { A: "hello", B: "$HOME ${A} {{A}} $${{" },
    },
    {
      name: "same service",
      self: "web",
      all: { web: { A: "x", B: "${{A}}-y" } },
      want: { A: "x", B: "x-y" },
    },
    {
      name: "whitespace optional",
      self: "web",
      all: { web: { A: "x", B: "${{ A }}${{A}}${{   A}}${{A  }}" } },
      want: { A: "x", B: "xxxx" },
    },
    {
      name: "other service",
      self: "web",
      all: { web: { DB: "${{ db.URL }}" }, db: { URL: "pg://host" } },
      want: { DB: "pg://host" },
    },
    {
      name: "other service resolved in its own scope",
      self: "web",
      all: {
        web: { DB: "${{ db.URL }}", PASSWORD: "wrong" },
        db: { URL: "pg://u:${{PASSWORD}}@${{ HOST }}", PASSWORD: "secret", HOST: "db" },
      },
      want: { DB: "pg://u:secret@db", PASSWORD: "wrong" },
    },
    {
      name: "chain across services",
      self: "a",
      all: { a: { X: "${{ b.X }}" }, b: { X: "${{ c.X }}!" }, c: { X: "end" } },
      want: { X: "end!" },
    },
    {
      name: "multiple references in one value",
      self: "web",
      all: {
        web: { URL: "${{ u }}:${{ p }}@${{ db.HOST }}:${{ db.PORT }}", u: "me", p: "pw" },
        db: { HOST: "db", PORT: "5432" },
      },
      want: { URL: "me:pw@db:5432", u: "me", p: "pw" },
    },
    {
      name: "same reference used twice",
      self: "web",
      all: { web: { A: "1", B: "${{A}}${{A}}" } },
      want: { A: "1", B: "11" },
    },
    {
      name: "missing variable",
      self: "web",
      all: { web: { A: "a${{ NOPE }}b" } },
      want: { A: "ab" },
    },
    {
      name: "missing service",
      self: "web",
      all: { web: { A: "a${{ ghost.KEY }}b" } },
      want: { A: "ab" },
    },
    {
      name: "malformed references untouched",
      self: "web",
      all: { web: { A: "${{}} ${{ }} ${{ a b }} ${{A" } },
      want: { A: "${{}} ${{ }} ${{ a b }} ${{A" },
    },
    {
      name: "service split at first dot",
      self: "web",
      all: { web: { A: "${{ svc.a.b }}" }, svc: { "a.b": "no", b: "yes" } },
      want: { A: "no" },
    },
    { name: "unknown self", self: "ghost", all: { web: { A: "1" } }, want: {} },
    {
      name: "diamond is not a cycle",
      self: "web",
      all: { web: { A: "${{ B }}${{ C }}", B: "${{ D }}", C: "${{ D }}", D: "d" } },
      want: { A: "dd", B: "d", C: "d", D: "d" },
    },
    {
      name: "mutual references between services without cycle",
      self: "a",
      all: { a: { X: "${{ b.Y }}", Z: "z" }, b: { Y: "${{ a.Z }}" } },
      want: { X: "z", Z: "z" },
    },
    { name: "empty", self: "web", all: { web: {} }, want: {} },
  ];
  for (const tt of tests) {
    it(tt.name, () => {
      expect(resolve(tt.self, tt.all)).toEqual({ ok: true, value: tt.want });
    });
  }

  it("does not mutate its input", () => {
    const all: Scope = { web: { A: "x", B: "${{A}}" } };
    resolve("web", all);
    expect(all.web!.B).toBe("${{A}}");
  });
});

describe("cycles", () => {
  const tests: { name: string; self: string; all: Scope; want: string }[] = [
    {
      name: "self reference",
      self: "web",
      all: { web: { A: "${{ A }}" } },
      want: "web.A -> web.A",
    },
    {
      name: "two variables",
      self: "web",
      all: { web: { A: "${{ B }}", B: "${{ A }}" } },
      want: "web.A -> web.B -> web.A",
    },
    {
      name: "across services",
      self: "a",
      all: { a: { X: "${{ b.Y }}" }, b: { Y: "${{ a.X }}" } },
      want: "a.X -> b.Y -> a.X",
    },
    {
      name: "cycle reachable through a prefix",
      self: "a",
      all: { a: { X: "${{ b.P }}" }, b: { P: "${{ Q }}", Q: "${{ R }}", R: "${{ Q }}" } },
      want: "b.Q -> b.R -> b.Q",
    },
  ];
  for (const tt of tests) {
    it(tt.name, () => {
      const got = resolve(tt.self, tt.all);
      expect(got.ok).toBe(false);
      if (!got.ok) expect(got.error).toBe(`vars: reference cycle: ${tt.want}`);
    });
  }
});

describe("limits", () => {
  const depth: Record<string, string> = {};
  for (let i = 0; i <= MAX_REFERENCE_DEPTH; i++) {
    depth[`V${String(i).padStart(3, "0")}`] = `\${{V${String(i + 1).padStart(3, "0")}}}`;
  }
  const total: Record<string, string> = {};
  for (let i = 0; i <= Math.floor(MAX_RESOLVED_BYTES / MAX_VALUE_BYTES); i++) {
    total[String(i)] = "x".repeat(MAX_VALUE_BYTES);
  }
  const tests: { name: string; values: Record<string, string> }[] = [
    { name: "raw value", values: { A: "x".repeat(MAX_VALUE_BYTES + 1) } },
    {
      name: "expansion",
      values: { A: "x".repeat(MAX_VALUE_BYTES / 2 + 1), B: "${{A}}${{A}}" },
    },
    { name: "depth", values: depth },
    { name: "total", values: total },
  ];
  for (const tt of tests) {
    it(`rejects ${tt.name}`, () => {
      expect(resolve("web", { web: tt.values }).ok).toBe(false);
    });
  }

  it("accepts a value of exactly the limit", () => {
    expect(resolve("web", { web: { A: "x".repeat(MAX_VALUE_BYTES) } }).ok).toBe(true);
  });

  it("counts bytes, not characters", () => {
    const wide = "é".repeat(MAX_VALUE_BYTES / 2 + 1);
    expect(resolve("web", { web: { A: wide } }).ok).toBe(false);
  });
});

describe("resolveKey", () => {
  it("resolves one key while another cycles", () => {
    const all: Scope = { web: { A: "${{ B }}", B: "${{ A }}", C: "ok" } };
    expect(resolveKey("web", "C", all)).toEqual({ ok: true, value: "ok" });
    expect(resolveKey("web", "A", all).ok).toBe(false);
  });
});

describe("splitRefs", () => {
  it("separates text and references", () => {
    expect(splitRefs("pg://${{ db.HOST }}:${{PORT}}/x", "web")).toEqual([
      { kind: "text", text: "pg://" },
      { kind: "ref", text: "${{ db.HOST }}", target: { service: "db", key: "HOST" } },
      { kind: "text", text: ":" },
      { kind: "ref", text: "${{PORT}}", target: { service: "web", key: "PORT" } },
      { kind: "text", text: "/x" },
    ]);
  });

  it("leaves malformed references as text", () => {
    expect(splitRefs("${{ a b }}", "web")).toEqual([{ kind: "text", text: "${{ a b }}" }]);
  });
});

describe("parseTarget", () => {
  it("splits at the first dot", () => {
    expect(parseTarget("svc.a.b", "web")).toEqual({ service: "svc", key: "a.b" });
    expect(parseTarget("KEY", "web")).toEqual({ service: "web", key: "KEY" });
  });
});

describe("injected variables", () => {
  const app: ServiceFacts = {
    name: "web",
    kind: "app",
    port: 8080,
    branch: "main",
    domains: ["web-demo.example.com", "www.example.com"],
    own: {},
  };

  it("provides the full set to an app", () => {
    expect(injectedVars("Demo", app, "abc123")).toEqual({
      SHED_PROJECT_NAME: "Demo",
      SHED_SERVICE_NAME: "web",
      SHED_PRIVATE_DOMAIN: "web",
      PORT: "8080",
      SHED_PUBLIC_DOMAIN: "web-demo.example.com",
      SHED_GIT_COMMIT_SHA: "abc123",
      SHED_GIT_BRANCH: "main",
    });
  });

  it("omits what does not apply", () => {
    const db: ServiceFacts = { ...app, kind: "postgres", branch: "", domains: [] };
    expect(injectedVars("Demo", db, "")).toEqual({
      SHED_PROJECT_NAME: "Demo",
      SHED_SERVICE_NAME: "web",
      SHED_PRIVATE_DOMAIN: "web",
    });
  });

  it("injects PORT only for apps with a port", () => {
    expect(injectedVars("p", { ...app, port: 0 }, "").PORT).toBeUndefined();
  });
});

describe("buildScope", () => {
  const db: ServiceFacts = {
    name: "db",
    kind: "postgres",
    port: 5432,
    branch: "",
    domains: [],
    own: { URL: "pg://${{ SHED_PRIVATE_DOMAIN }}:5432" },
  };
  const web: ServiceFacts = {
    name: "web",
    kind: "app",
    port: 3000,
    branch: "main",
    domains: [],
    own: { DATABASE_URL: "${{ db.URL }}", SHED_GIT_BRANCH: "override" },
  };

  it("lets stored variables override injected ones", () => {
    const scope = buildScope("Demo", [web, db], "web", "sha1");
    expect(scope.web!.SHED_GIT_BRANCH).toBe("override");
  });

  it("gives the commit only to the selected service", () => {
    const scope = buildScope("Demo", [web, db], "web", "sha1");
    expect(scope.web!.SHED_GIT_COMMIT_SHA).toBe("sha1");
    expect(scope.db!.SHED_GIT_COMMIT_SHA).toBeUndefined();
  });

  it("resolves references in the scope of the service that owns them", () => {
    const scope = buildScope("Demo", [web, db], "web", "");
    expect(resolve("web", scope)).toMatchObject({
      ok: true,
      value: { DATABASE_URL: "pg://db:5432" },
    });
  });
});
