/** ConfigKey describes one key of shed.toml. */
export type ConfigKey = {
  key: string;
  type: "string" | "int" | "float" | "bool" | "list";
  /** Built-in default, as written in TOML. Empty means unset. */
  default: string;
};

/** configKeys lists every key shed reads, with its built-in default. */
export const configKeys: readonly ConfigKey[] = [
  { key: "server.listen", type: "string", default: '"127.0.0.1:3000"' },
  { key: "server.url", type: "string", default: '"http://localhost:3000"' },
  { key: "data.dir", type: "string", default: '"/var/lib/shed"' },
  { key: "proxy.enabled", type: "bool", default: "true" },
  { key: "proxy.http_port", type: "int", default: "80" },
  { key: "proxy.https_port", type: "int", default: "443" },
  { key: "proxy.acme_email", type: "string", default: '""' },
  { key: "proxy.base_domain", type: "string", default: '""' },
  { key: "auth.allowed_users", type: "list", default: "[]" },
  { key: "build.memory_mb", type: "int", default: "2048" },
  { key: "build.cpus", type: "float", default: "2" },
  { key: "build.min_free_mb", type: "int", default: "2048" },
  { key: "deployments.log_max_mb", type: "int", default: "10" },
  { key: "deployments.keep", type: "int", default: "50" },
  { key: "log.level", type: "string", default: '"info"' },
  { key: "log.max_size_mb", type: "int", default: "20" },
  { key: "log.max_backups", type: "int", default: "5" },
  { key: "log.max_age_days", type: "int", default: "30" },
];

const prefix = "SHED_";

/**
 * envToKey maps an environment variable name to the key shed reads it as:
 * strip SHED_, lowercase, replace the first underscore with a dot. It returns
 * null for names without the prefix, which shed ignores.
 */
export function envToKey(name: string): string | null {
  const trimmed = name.trim();
  if (!trimmed.startsWith(prefix)) return null;
  return trimmed.slice(prefix.length).toLowerCase().replace("_", ".");
}

/** keyToEnv is the environment variable that sets a TOML key. */
export function keyToEnv(key: string): string {
  return prefix + key.trim().toUpperCase().replace(".", "_");
}

/** Resolution is the outcome of reading an environment variable name. */
export type Resolution =
  | { kind: "key"; key: ConfigKey }
  | { kind: "unknown"; key: string; suggestion: ConfigKey | null }
  | { kind: "ignored" };

/**
 * resolveEnv explains what shed does with an environment variable name. An
 * unknown key is silently ignored by shed, so a suggestion is offered when
 * the name matches a known key once underscores are ignored.
 */
export function resolveEnv(name: string): Resolution {
  const key = envToKey(name);
  if (key === null) return { kind: "ignored" };
  const known = configKeys.find((k) => k.key === key);
  if (known) return { kind: "key", key: known };
  const flat = key.replace(/[._]/g, "");
  return {
    kind: "unknown",
    key,
    suggestion: configKeys.find((k) => k.key.replace(/[._]/g, "") === flat) ?? null,
  };
}
