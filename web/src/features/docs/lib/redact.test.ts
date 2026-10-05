import { describe, expect, it } from "vite-plus/test";
import { chunk, redactEach, StreamRedactor } from "./redact";

function run(secrets: string[], pieces: string[]): string {
  const r = new StreamRedactor(secrets);
  return pieces.map((p) => r.write(p)).join("") + r.flush();
}

describe("StreamRedactor", () => {
  const tests: { name: string; secrets: string[]; pieces: string[]; want: string }[] = [
    { name: "no secrets", secrets: [], pieces: ["a", "b"], want: "ab" },
    {
      name: "whole secret in one write",
      secrets: ["s3cr3t"],
      pieces: ["pw=s3cr3t!"],
      want: "pw=***!",
    },
    {
      name: "split across writes",
      secrets: ["s3cr3t"],
      pieces: ["pw=s3c", "r3t!"],
      want: "pw=***!",
    },
    {
      name: "split into single characters",
      secrets: ["s3cr3t"],
      pieces: "pw=s3cr3t!".split(""),
      want: "pw=***!",
    },
    { name: "repeated", secrets: ["ab"], pieces: ["abab", "ab"], want: "*********" },
    { name: "longest secret wins", secrets: ["abc", "abcdef"], pieces: ["abcdef"], want: "***" },
    { name: "earliest match wins", secrets: ["bc", "abcd"], pieces: ["abcd"], want: "***" },
    { name: "partial match at end is flushed", secrets: ["secret"], pieces: ["sec"], want: "sec" },
    { name: "empty secret ignored", secrets: ["", "x"], pieces: ["axb"], want: "a***b" },
  ];
  for (const tt of tests) {
    it(tt.name, () => expect(run(tt.secrets, tt.pieces)).toBe(tt.want));
  }

  it("holds back one character fewer than the longest secret", () => {
    const r = new StreamRedactor(["abcdef", "xy"]);
    expect(r.tail).toBe(5);
    expect(r.write("0123456789")).toBe("01234");
    expect(r.held).toBe("56789");
  });

  it("gives the same output however the text is cut", () => {
    const text = "token=hunter2 and again hunter2; done";
    const want = run(["hunter2"], [text]);
    for (let size = 1; size <= text.length; size++) {
      expect(run(["hunter2"], chunk(text, size))).toBe(want);
    }
  });
});

describe("redactEach", () => {
  it("masks inside a piece but misses a split secret", () => {
    expect(redactEach("a s3cr3t b", ["s3cr3t"])).toBe("a *** b");
    expect(redactEach("a s3c", ["s3cr3t"])).toBe("a s3c");
  });
});

describe("chunk", () => {
  it("splits into fixed-size pieces", () => {
    expect(chunk("abcde", 2)).toEqual(["ab", "cd", "e"]);
  });
});
