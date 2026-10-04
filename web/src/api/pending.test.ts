import { describe, expect, it } from "vite-plus/test";
import { PendingLines } from "./pending";

const line = (text: string) => ({ text });

describe("PendingLines", () => {
  it("keeps lines under the limits", () => {
    const p = new PendingLines(3, 100);
    p.push(line("a"));
    p.push(line("b"));
    expect(p.take()).toEqual({ lines: [line("a"), line("b")], dropped: 0 });
    expect(p.take()).toEqual({ lines: [], dropped: 0 });
  });

  it("drops the oldest lines beyond the line limit", () => {
    const p = new PendingLines(2, 100);
    for (const t of ["a", "b", "c", "d"]) p.push(line(t));
    expect(p.take()).toEqual({ lines: [line("c"), line("d")], dropped: 2 });
  });

  it("drops the oldest lines beyond the character limit", () => {
    const p = new PendingLines(100, 5);
    for (const t of ["aaa", "bb", "c"]) p.push(line(t));
    expect(p.take()).toEqual({ lines: [line("bb"), line("c")], dropped: 1 });
  });

  it("keeps the newest line even if it alone exceeds the character limit", () => {
    const p = new PendingLines(100, 5);
    p.push(line("a"));
    p.push(line("too long"));
    expect(p.take()).toEqual({ lines: [line("too long")], dropped: 1 });
  });

  it("stays bounded over many pushes", () => {
    const p = new PendingLines(10, 1000);
    for (let i = 0; i < 100_000; i++) p.push(line(String(i)));
    expect(p.size).toBe(10);
    const { lines, dropped } = p.take();
    expect(lines.map((l) => l.text)).toEqual(
      Array.from({ length: 10 }, (_, i) => String(99_990 + i)),
    );
    expect(dropped).toBe(99_990);
  });

  it("clear resets the dropped count", () => {
    const p = new PendingLines(1, 100);
    p.push(line("a"));
    p.push(line("b"));
    p.clear();
    expect(p.take()).toEqual({ lines: [], dropped: 0 });
  });
});
