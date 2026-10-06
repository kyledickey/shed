/**
 * safeNext returns value if it is a same-origin relative path that is safe to
 * redirect to after sign-in, and undefined otherwise. It must start with a
 * single "/": "//host" and "/\host" are protocol-relative to browsers.
 */
export function safeNext(value: unknown): string | undefined {
  if (typeof value !== "string" || !value.startsWith("/")) return undefined;
  if (value.startsWith("//") || value.startsWith("/\\")) return undefined;
  // Browsers strip tabs and newlines from URLs, which could turn "/\t/host" into "//host".
  for (const ch of value) {
    const code = ch.charCodeAt(0);
    if (code < 0x20 || code === 0x7f) return undefined;
  }
  return value;
}
