import type { AuditEvent } from "../../api/types";

/** Detail fields shown elsewhere in a row, or not worth a chip. */
const hidden = new Set(["service", "client", "clientId", "count"]);

/**
 * detailPairs flattens an event's detail into labeled values for display:
 * lists are joined, empty lists and hidden fields are left out, and objects
 * (an MCP call's arguments) become key=value text.
 */
export function detailPairs(detail: AuditEvent["detail"]): [string, string][] {
  const out: [string, string][] = [];
  for (const [key, value] of Object.entries(detail ?? {})) {
    if (hidden.has(key) || value === null || value === "") continue;
    if (Array.isArray(value)) {
      if (value.length > 0) out.push([key, value.join(", ")]);
    } else if (typeof value === "object") {
      const args = Object.entries(value as Record<string, unknown>).map(
        ([k, v]) => `${k}=${String(v)}`,
      );
      if (args.length > 0) out.push([key, args.join(" ")]);
    } else {
      out.push([key, String(value as string | number | boolean)]);
    }
  }
  return out;
}

/** actionGroups are the action prefixes the dashboard filters by. */
export const actionGroups = [
  "auth",
  "oauth",
  "setup",
  "webhook",
  "project",
  "service",
  "service.variables",
  "deployment",
  "domain",
  "volume",
  "backup",
  "onepassword",
  "update",
  "audit",
  "mcp",
] as const;
