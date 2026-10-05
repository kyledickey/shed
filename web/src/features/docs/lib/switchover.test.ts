import { describe, expect, it } from "vite-plus/test";
import { switchoverFrames, type SwitchMode } from "./switchover";

describe("switchoverFrames", () => {
  const modes: SwitchMode[] = ["overlap", "stop-first"];

  it.each(modes)("%s starts with the old container serving and ends with the new one", (mode) => {
    const frames = switchoverFrames(mode);
    expect(frames[0]).toMatchObject({ routes: "old", old: { state: "running" } });
    expect(frames.at(-1)).toMatchObject({ routes: "new", old: { state: "absent" } });
  });

  it.each(modes)("%s never routes to a container that is not running", (mode) => {
    for (const f of switchoverFrames(mode)) {
      if (f.routes === "old") expect(f.old.state).toBe("running");
      if (f.routes === "new") expect(f.next.state).toBe("running");
    }
  });

  it.each(modes)("%s routes to the new container only after it holds the alias", (mode) => {
    for (const f of switchoverFrames(mode)) {
      if (f.routes === "new") expect(f.next.alias).toBe(true);
    }
  });

  it("overlap keeps the old container running until it is retired", () => {
    const frames = switchoverFrames("overlap");
    expect(frames.slice(0, -1).every((f) => f.old.state === "running")).toBe(true);
    expect(frames.some((f) => f.routes === "none")).toBe(false);
  });

  it("stop-first has a window with no routes and a stopped old container", () => {
    const gap = switchoverFrames("stop-first").filter((f) => f.routes === "none");
    expect(gap.length).toBeGreaterThan(0);
    expect(gap.every((f) => f.old.state === "stopped")).toBe(true);
  });
});
