import { describe, expect, it } from "vite-plus/test";
import { parseBuildLine, parseRuntimeLine } from "./logs";

const TS = "2026-10-04T12:00:01.123456789Z";

describe("parseRuntimeLine", () => {
  it("parses JSON with docker prefix", () => {
    const r = parseRuntimeLine(`${TS} {"level":"info","msg":"listening","port":3000}`);
    expect(r.time?.toISOString()).toBe("2026-10-04T12:00:01.123Z");
    expect(r.level).toBe("info");
    expect(r.message).toBe("listening");
    expect(r.fields).toEqual([["port", "3000"]]);
    expect(r.http).toBeNull();
  });

  it("parses JSON without prefix and takes the structured time", () => {
    const r = parseRuntimeLine('{"time":"2026-10-04T12:00:00Z","message":"hi","a":1}');
    expect(r.time?.toISOString()).toBe("2026-10-04T12:00:00.000Z");
    expect(r.message).toBe("hi");
    expect(r.fields).toEqual([["a", "1"]]);
  });

  it("prefers the docker time over a structured one", () => {
    const r = parseRuntimeLine(`${TS} {"ts":1,"msg":"x"}`);
    expect(r.time?.toISOString()).toBe("2026-10-04T12:00:01.123Z");
    expect(r.fields).toEqual([]);
  });

  it.each([
    ["unix seconds", '{"ts":1759579200,"msg":"x"}', "2025-10-04T12:00:00.000Z"],
    ["unix millis", '{"time":1759579200000,"msg":"x"}', "2025-10-04T12:00:00.000Z"],
    ["@timestamp", '{"@timestamp":"2025-10-04T12:00:00Z","msg":"x"}', "2025-10-04T12:00:00.000Z"],
  ])("parses JSON time as %s", (_, line, iso) => {
    expect(parseRuntimeLine(line).time?.toISOString()).toBe(iso);
  });

  it.each([
    ['{"level":"WARNING","msg":"x"}', "warn"],
    ['{"lvl":"trace","msg":"x"}', "debug"],
    ['{"severity":"CRITICAL","msg":"x"}', "error"],
    ['{"log.level":"notice","msg":"x"}', "info"],
    ['{"log":{"level":"warn"},"msg":"x"}', "warn"],
    ['{"level":10,"msg":"x"}', "debug"],
    ['{"level":30,"msg":"x"}', "info"],
    ['{"level":40,"msg":"x"}', "warn"],
    ['{"level":60,"msg":"x"}', "error"],
    ['{"msg":"x"}', null],
  ])("maps JSON level %s to %s", (line, level) => {
    expect(parseRuntimeLine(line).level).toBe(level);
  });

  it("stringifies nested JSON values in order", () => {
    const r = parseRuntimeLine('{"b":{"x":1},"msg":"m","a":[1,2],"c":null,"d":"s"}');
    expect(r.fields).toEqual([
      ["b", '{"x":1}'],
      ["a", "[1,2]"],
      ["c", "null"],
      ["d", "s"],
    ]);
  });

  it("falls back to plain for malformed JSON", () => {
    const r = parseRuntimeLine('{"msg": oops');
    expect(r.message).toBe('{"msg": oops');
    expect(r.fields).toEqual([]);
  });

  it("does not treat JSON arrays as objects", () => {
    const r = parseRuntimeLine("[1,2,3]");
    expect(r.message).toBe("[1,2,3]");
    expect(r.level).toBeNull();
  });

  it("parses logfmt with quoted values and escapes", () => {
    const r = parseRuntimeLine(
      'time=2026-10-04T12:00:00Z level=INFO msg="server \\"started\\"" addr=:3000 err="x y"',
    );
    expect(r.time?.toISOString()).toBe("2026-10-04T12:00:00.000Z");
    expect(r.level).toBe("info");
    expect(r.message).toBe('server "started"');
    expect(r.fields).toEqual([
      ["addr", ":3000"],
      ["err", "x y"],
    ]);
  });

  it("does not treat a lone key=value as logfmt", () => {
    const r = parseRuntimeLine("msg=hello");
    expect(r.message).toBe("msg=hello");
    expect(r.fields).toEqual([]);
  });

  it("requires msg, level or time for logfmt", () => {
    const r = parseRuntimeLine("a=1 b=2");
    expect(r.message).toBe("a=1 b=2");
    expect(r.fields).toEqual([]);
  });

  it.each([
    ["ERROR: db down", "error", "db down"],
    ["[warn] disk low", "warn", "disk low"],
    ["INFO  ready", "info", "ready"],
    ["WARNING something", "warn", "something"],
    ["DEBUG x", "debug", "x"],
    ["ERR bad", "error", "bad"],
    ["level: info started", "info", "started"],
    ["Error: connection refused", "error", "Error: connection refused"],
    ["FATAL out of memory", "error", "out of memory"],
    ["panic: runtime error", "error", "panic: runtime error"],
    ["Traceback (most recent call last):", "error", "Traceback (most recent call last):"],
    ["java.lang.NullPointerException: x", "error", "java.lang.NullPointerException: x"],
    ["E something", null, "E something"],
    ["I something", null, "I something"],
    ["information is useful", null, "information is useful"],
    ["error handling is hard", null, "error handling is hard"],
  ])("detects plain level in %j", (line, level, message) => {
    const r = parseRuntimeLine(`${TS} ${line}`);
    expect(r.level).toBe(level);
    expect(r.message).toBe(message);
  });

  it("strips a leading plain timestamp", () => {
    const r = parseRuntimeLine("[2026-10-04 12:00:00] ERROR boom");
    expect(r.time?.toISOString()).toBe("2026-10-04T12:00:00.000Z");
    expect(r.level).toBe("error");
    expect(r.message).toBe("boom");
  });

  it("prefers docker time over a plain text timestamp", () => {
    const r = parseRuntimeLine(`${TS} 2026-10-04T13:00:00.5Z INFO hi`);
    expect(r.time?.toISOString()).toBe("2026-10-04T12:00:01.123Z");
    expect(r.message).toBe("hi");
  });

  it("handles a line that is just a timestamp", () => {
    const r = parseRuntimeLine("2026-10-04T12:00:00Z");
    expect(r.time?.toISOString()).toBe("2026-10-04T12:00:00.000Z");
    expect(r.message).toBe("");
  });

  it("handles an empty line", () => {
    expect(parseRuntimeLine("")).toEqual({
      time: null,
      level: null,
      message: "",
      fields: [],
      http: null,
    });
  });

  it.each([
    ["GET /api/users 200 12ms", "GET", "/api/users", 200, "12ms", "info"],
    ["POST /login - 302 - 4.2 ms", "POST", "/login", 302, "4.2ms", "info"],
    ['"GET /x HTTP/1.1" 404 512', "GET", "/x", 404, null, "warn"],
    [
      '1.2.3.4 - - [04/Oct/2026:12:00:00 +0000] "POST /y?a=1 HTTP/2.0" 503 0 "-" "curl"',
      "POST",
      "/y?a=1",
      503,
      null,
      "error",
    ],
  ])("detects http in %j", (line, method, path, status, duration, level) => {
    const r = parseRuntimeLine(line);
    expect(r.http).toEqual({ method, path, status, duration });
    expect(r.level).toBe(level);
  });

  it("rejects out of range http status", () => {
    expect(parseRuntimeLine("GET /x 999 1ms").http).toBeNull();
  });

  it("keeps an explicit level over the http-derived one", () => {
    expect(parseRuntimeLine("DEBUG GET /x 500 1ms").level).toBe("debug");
  });

  it("detects http from structured fields", () => {
    const r = parseRuntimeLine(
      '{"msg":"request","method":"get","path":"/a","status":500,"latency":"3ms","ip":"1.2.3.4"}',
    );
    expect(r.http).toEqual({ method: "GET", path: "/a", status: 500, duration: "3ms" });
    expect(r.level).toBe("error");
    expect(r.fields).toEqual([["ip", "1.2.3.4"]]);
  });
});

describe("parseBuildLine", () => {
  it("parses step headings", () => {
    expect(parseBuildLine("==> Cloning repo")).toEqual({ kind: "step", text: "Cloning repo" });
  });

  it.each([
    ["#5 [internal] load build definition", 5, "[internal] load build definition", null, null],
    ["#5 transferring dockerfile: 2B done", 5, "transferring dockerfile: 2B done", null, null],
    ["#5 DONE 0.0s", 5, "", "done", "0.0s"],
    ["#12 DONE 1.2s", 12, "", "done", "1.2s"],
    ["#8 CACHED", 8, "", "cached", null],
    ["#8 0.512 added 120 packages in 3s", 8, "added 120 packages in 3s", null, "0.512s"],
    ['#8 ERROR: process "npm ci" failed', 8, 'process "npm ci" failed', "error", null],
  ])("parses buildkit line %j", (line, id, text, state, elapsed) => {
    expect(parseBuildLine(line)).toEqual({ kind: "buildkit", id, text, state, elapsed });
  });

  it.each([
    ["error: failed to compile", "error"],
    ["npm ERR! code ELIFECYCLE", "error"],
    ["npm WARN deprecated x", "warn"],
    ["warning: unused", "warn"],
    ["WARN something", "warn"],
    ["just output", null],
    ["", null],
  ])("parses plain build line %j", (line, level) => {
    expect(parseBuildLine(line)).toEqual({ kind: "plain", text: line, level });
  });
});
