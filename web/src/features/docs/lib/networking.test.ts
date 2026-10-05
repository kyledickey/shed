import { describe, expect, it } from "vite-plus/test";
import { generatedHost, simulateHealth, slug } from "./networking";

describe("slug", () => {
  const tests: [string, string][] = [
    ["My Project", "my-project"],
    ["  Hello,  World!! ", "hello-world"],
    ["Éclair 2", "clair-2"],
    ["---", ""],
    ["a_b.c", "a-b-c"],
  ];
  for (const [name, want] of tests) {
    it(JSON.stringify(name), () => expect(slug(name)).toBe(want));
  }
});

describe("generatedHost", () => {
  it("joins service, project slug, and base domain", () => {
    expect(generatedHost("web", "My Project", "apps.example.com")).toBe(
      "web-my-project.apps.example.com",
    );
  });

  it("drops an empty project slug", () => {
    expect(generatedHost("web", "!!!", "apps.example.com")).toBe("web.apps.example.com");
  });

  it("caps the label at 63 characters without a trailing hyphen", () => {
    const host = generatedHost("web", `${"a".repeat(58)} tail`, "x.io");
    const label = host.split(".")[0]!;
    expect(label.length).toBeLessThanOrEqual(63);
    expect(label.endsWith("-")).toBe(false);
  });
});

describe("simulateHealth", () => {
  it("passes at once when the app is ready", () => {
    const run = simulateHealth({ name: "web", port: 8080, path: "", readyAfter: 0 });
    expect(run.healthy).toBe(true);
    expect(run.lines.some((l) => l.text.startsWith("Not ready yet"))).toBe(false);
  });

  it("reports not ready every five seconds", () => {
    const run = simulateHealth({ name: "web", port: 8080, path: "/health", readyAfter: 12 });
    const reports = run.lines.filter((l) => l.text.startsWith("Not ready yet")).map((l) => l.at);
    expect(reports).toEqual([5, 10]);
    expect(run.healthy).toBe(true);
  });

  it("fails when the app is not ready within two minutes", () => {
    const run = simulateHealth({ name: "web", port: 8080, path: "", readyAfter: 130 });
    expect(run.healthy).toBe(false);
    expect(run.lines.at(-1)?.text).toContain("health check timed out after 2m0s");
  });

  it("succeeds on the last probe before the deadline", () => {
    expect(simulateHealth({ name: "web", port: 80, path: "", readyAfter: 119 }).healthy).toBe(true);
  });

  it("only watches a service without a port", () => {
    const run = simulateHealth({ name: "web", port: 0, path: "", readyAfter: 50 });
    expect(run.healthy).toBe(true);
    expect(run.lines.at(-1)?.text).toBe("Still running after 3s");
  });
});

describe("simulateHealth output", () => {
  it("formats durations like Go", () => {
    const run = simulateHealth({ name: "web", port: 80, path: "", readyAfter: 65 });
    expect(run.lines.map((l) => l.text)).toContain("Healthy after 1m5s");
    expect(run.lines.at(-1)?.text).toBe("Private host web resolves to the new container");
  });
});
