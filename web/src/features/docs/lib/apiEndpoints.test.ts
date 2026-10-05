import { describe, expect, it } from "vite-plus/test";
import { curlCommand } from "./apiEndpoints";

describe("curlCommand", () => {
  it("sends a GET with only the cookie", () => {
    expect(curlCommand("https://x.test", { method: "GET", path: "/api/services/{id}" })).toBe(
      "curl -s 'https://x.test/api/services/<id>' \\\n  -H 'Cookie: shed_session=<your session token>'",
    );
  });

  it("adds the JSON content type to mutations with a body", () => {
    const cmd = curlCommand("https://x.test", {
      method: "POST",
      path: "/api/services/{id}/backups",
    });
    expect(cmd).toContain("-X POST");
    expect(cmd).toContain("Content-Type: application/json");
  });

  it("omits the body headers on DELETE", () => {
    const cmd = curlCommand("https://x.test", { method: "DELETE", path: "/api/services/{id}" });
    expect(cmd).not.toContain("Content-Type");
  });
});
