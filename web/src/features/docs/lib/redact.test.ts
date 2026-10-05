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
      secrets: ["s3cr3t-pw"],
      pieces: ["pw=s3cr3t-pw!"],
      want: "pw=***!",
    },
    {
      name: "split across writes",
      secrets: ["s3cr3t-pw"],
      pieces: ["pw=s3c", "r3t-pw!"],
      want: "pw=***!",
    },
    {
      name: "split into single characters",
      secrets: ["s3cr3t-pw"],
      pieces: "pw=s3cr3t-pw!".split(""),
      want: "pw=***!",
    },
    {
      name: "repeated",
      secrets: ["abcdefgh"],
      pieces: ["abcdefghabcdefgh", "abcdefgh"],
      want: "*********",
    },
    {
      name: "longest secret wins",
      secrets: ["abcdefgh", "abcdefghijkl"],
      pieces: ["abcdefghijkl"],
      want: "***",
    },
    {
      name: "earliest match wins",
      secrets: ["bcdefghi", "abcdefghij"],
      pieces: ["abcdefghij"],
      want: "***",
    },
    {
      name: "partial match at end is flushed",
      secrets: ["secret-pw"],
      pieces: ["sec"],
      want: "sec",
    },
    {
      name: "short secrets ignored",
      secrets: ["", "1", "true"],
      pieces: ["2026-10-01 true"],
      want: "2026-10-01 true",
    },
  ];
  for (const tt of tests) {
    it(tt.name, () => expect(run(tt.secrets, tt.pieces)).toBe(tt.want));
  }

  it("holds back one character fewer than the longest secret", () => {
    const r = new StreamRedactor(["abcdefghij", "xyzxyzxy"]);
    expect(r.tail).toBe(9);
    expect(r.write("0123456789ABCD")).toBe("01234");
    expect(r.held).toBe("56789ABCD");
  });

  it("gives the same output however the text is cut", () => {
    const text = "token=hunter22 and again hunter22; done";
    const want = run(["hunter22"], [text]);
    for (let size = 1; size <= text.length; size++) {
      expect(run(["hunter22"], chunk(text, size))).toBe(want);
    }
  });
});

describe("redactEach", () => {
  it("masks inside a piece but misses a split secret", () => {
    expect(redactEach("a s3cr3t-pw b", ["s3cr3t-pw"])).toBe("a *** b");
    expect(redactEach("a s3c", ["s3cr3t-pw"])).toBe("a s3c");
  });
});

describe("chunk", () => {
  it("splits into fixed-size pieces", () => {
    expect(chunk("abcde", 2)).toEqual(["ab", "cd", "e"]);
  });
});
