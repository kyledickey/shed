import { describe, expect, it } from "vite-plus/test";
import { targetForm } from "./targets";

describe("compose", () => {
  it.each([
    ["hark", { url: " https://hark.ryan.ceo/hooks/abc " }, "https://hark.ryan.ceo/hooks/abc"],
    ["ntfy", { url: "https://ntfy.sh/alerts", token: "" }, "https://ntfy.sh/alerts"],
    [
      "ntfy",
      { url: "https://ntfy.example.com/alerts", token: "tk_a/b@c" },
      "https://:tk_a%2Fb%40c@ntfy.example.com/alerts",
    ],
    [
      "smtp",
      { host: "smtp.example.com", security: "smtp", from: "shed@example.com", to: "a@x.io" },
      "smtp://smtp.example.com?from=shed%40example.com&to=a%40x.io",
    ],
    [
      "smtp",
      { host: "2001:db8::1", security: "smtp", port: "25", from: "s@x.io", to: "a@x.io" },
      "smtp://[2001:db8::1]:25?from=s%40x.io&to=a%40x.io",
    ],
    [
      "smtp",
      {
        host: "mail.example.com",
        security: "smtps",
        port: "2465",
        username: "me@example.com",
        password: "p:w/d@1",
        from: "Shed <shed@example.com>",
        to: "a@x.io, b@x.io",
      },
      "smtps://me%40example.com:p%3Aw%2Fd%401@mail.example.com:2465?from=Shed+%3Cshed%40example.com%3E&to=a%40x.io%2C+b%40x.io",
    ],
    ["webhook", { target: " anything " }, "anything"],
  ])("%s %j", (provider, values: Record<string, string>, want) => {
    expect(targetForm(provider).compose(values)).toBe(want);
  });

  it("rejects an ntfy topic URL it can't parse when a token is set", () => {
    expect(() => targetForm("ntfy").compose({ url: "ntfy.sh/alerts", token: "tk" })).toThrow(
      "Topic URL is not a valid URL.",
    );
  });
});
