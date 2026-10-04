import type { Variables } from "../api/types";

const KEY = /^[A-Za-z_][A-Za-z0-9_]*$/;

export function isValidKey(key: string): boolean {
  return KEY.test(key);
}

export type ParseResult = { vars: Variables; errors: string[] };

/** Parses .env text: KEY=value lines, # comments, optional `export`, quoted values. */
export function parseEnv(text: string): ParseResult {
  const vars: Variables = {};
  const errors: string[] = [];
  text.split(/\r?\n/).forEach((raw, i) => {
    const line = raw.trim();
    if (!line || line.startsWith("#")) return;
    const eq = line.indexOf("=");
    const key = (eq === -1 ? line : line.slice(0, eq)).replace(/^export\s+/, "").trim();
    if (eq === -1 || !isValidKey(key)) {
      errors.push(`Line ${i + 1}: expected KEY=value`);
      return;
    }
    vars[key] = unquote(line.slice(eq + 1).trim());
  });
  return { vars, errors };
}

function unquote(value: string): string {
  if (value.length >= 2 && value.startsWith('"') && value.endsWith('"')) {
    return value.slice(1, -1).replace(/\\(n|"|\\)/g, (_, c: string) => (c === "n" ? "\n" : c));
  }
  if (value.length >= 2 && value.startsWith("'") && value.endsWith("'")) {
    return value.slice(1, -1);
  }
  return value;
}

export function serializeEnv(vars: Variables): string {
  return Object.entries(vars)
    .map(([key, value]) => `${key}=${quote(value)}`)
    .join("\n");
}

function quote(value: string): string {
  if (!/[\s"'#\\]/.test(value)) return value;
  return `"${value.replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\n/g, "\\n")}"`;
}
