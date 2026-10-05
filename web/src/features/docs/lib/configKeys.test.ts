import { describe, expect, it } from "vite-plus/test";
import { configKeys, envToKey, keyToEnv, resolveEnv } from "./configKeys";

describe("envToKey", () => {
  const cases = [
    ["SHED_SERVER_URL", "server.url"],
    ["SHED_PROXY_ACME_EMAIL", "proxy.acme_email"],
    ["SHED_BUILD_MIN_FREE_MB", "build.min_free_mb"],
    ["SHED_AUTH_ALLOWED_USERS", "auth.allowed_users"],
    ["  SHED_DATA_DIR ", "data.dir"],
    ["PROXY_ACME_EMAIL", null],
    ["shed_server_url", null],
  ] as const;
  it.each(cases)("%s -> %s", (name, want) => {
    expect(envToKey(name)).toBe(want);
  });
});

describe("keyToEnv", () => {
  it("round-trips every known key", () => {
    for (const { key } of configKeys) {
      expect(envToKey(keyToEnv(key))).toBe(key);
    }
  });
  it("uppercases and swaps the first dot", () => {
    expect(keyToEnv("deployments.log_max_mb")).toBe("SHED_DEPLOYMENTS_LOG_MAX_MB");
  });
});

describe("resolveEnv", () => {
  it("finds known keys", () => {
    expect(resolveEnv("SHED_LOG_LEVEL")).toMatchObject({ kind: "key", key: { key: "log.level" } });
  });
  it("flags unknown keys and suggests the near miss", () => {
    expect(resolveEnv("SHED_PROXYACME_EMAIL")).toMatchObject({
      kind: "unknown",
      key: "proxyacme.email",
      suggestion: { key: "proxy.acme_email" },
    });
    expect(resolveEnv("SHED_NOPE")).toEqual({ kind: "unknown", key: "nope", suggestion: null });
  });
  it("ignores names without the prefix", () => {
    expect(resolveEnv("ACME_EMAIL")).toEqual({ kind: "ignored" });
  });
});
