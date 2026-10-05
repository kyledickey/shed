/**
 * A port of shed's variable resolution: internal/vars (Resolve) and the
 * variable merge of internal/deploy (environment, injected). Keep it in step
 * with the Go code; the tests mirror internal/vars.
 */

/** Limits enforced by internal/vars. */
export const MAX_VALUE_BYTES = 64 << 10;
export const MAX_RESOLVED_BYTES = 1 << 20;
export const MAX_REFERENCE_DEPTH = 64;

// Go's \s is [\t\n\f\r ], narrower than JavaScript's.
const WS = "[ \\t\\n\\f\\r]";
const REF_SOURCE = `\\$\\{\\{${WS}*([^ \\t\\n\\f\\r{}]+)${WS}*\\}\\}`;

/** Variables of every service of a project, keyed by service name. */
export type Scope = Record<string, Record<string, string>>;

/** A reference target, as in internal/vars: a variable of one service. */
export type Target = { service: string; key: string };

/** Segment is a piece of a raw value: literal text or one ${{ }} reference. */
export type Segment =
  | { kind: "text"; text: string }
  | { kind: "ref"; text: string; target: Target };

/** splitRefs cuts raw into literal text and references, resolving each reference in the scope of self. */
export function splitRefs(raw: string, self: string): Segment[] {
  const out: Segment[] = [];
  let last = 0;
  for (const m of raw.matchAll(new RegExp(REF_SOURCE, "g"))) {
    if (m.index > last) out.push({ kind: "text", text: raw.slice(last, m.index) });
    out.push({ kind: "ref", text: m[0], target: parseTarget(m[1] ?? "", self) });
    last = m.index + m[0].length;
  }
  if (last < raw.length) out.push({ kind: "text", text: raw.slice(last) });
  return out;
}

/** parseTarget splits a reference body at its first dot; without a dot the key is in service self. */
export function parseTarget(body: string, self: string): Target {
  const dot = body.indexOf(".");
  return dot < 0
    ? { service: self, key: body }
    : { service: body.slice(0, dot), key: body.slice(dot + 1) };
}

/** Outcome is a resolved value or the error that stopped resolution. */
export type Outcome<T> = { ok: true; value: T } | { ok: false; error: string };

class ResolveError extends Error {}

const encoder = new TextEncoder();
const byteLength = (s: string) => encoder.encode(s).length;
const id = (t: Target) => `${t.service}\0${t.key}`;
const label = (t: Target) => `${t.service}.${t.key}`;

class Resolver {
  private resolved = new Map<string, string>();
  private visiting = new Set<string>();
  private stack: Target[] = [];
  private bytes = 0;

  private all: Scope;

  constructor(all: Scope) {
    this.all = all;
  }

  value(t: Target): string {
    const done = this.resolved.get(id(t));
    if (done !== undefined) return done;
    const vars = Object.hasOwn(this.all, t.service) ? this.all[t.service] : undefined;
    if (!vars || !Object.hasOwn(vars, t.key)) return "";
    const raw = vars[t.key]!;
    if (this.visiting.has(id(t))) throw this.cycleError(t);
    if (this.stack.length >= MAX_REFERENCE_DEPTH) {
      throw new ResolveError(`vars: reference depth exceeds ${MAX_REFERENCE_DEPTH} at ${label(t)}`);
    }
    if (byteLength(raw) > MAX_VALUE_BYTES) {
      throw new ResolveError(`vars: value ${label(t)} exceeds ${MAX_VALUE_BYTES} bytes`);
    }
    this.visiting.add(id(t));
    this.stack.push(t);
    const s = this.expand(t.service, raw);
    this.stack.pop();
    this.visiting.delete(id(t));
    const size = byteLength(s);
    if (size > MAX_RESOLVED_BYTES - this.bytes) {
      throw new ResolveError(`vars: resolved values exceed ${MAX_RESOLVED_BYTES} bytes`);
    }
    this.bytes += size;
    this.resolved.set(id(t), s);
    return s;
  }

  private expand(service: string, raw: string): string {
    let out = "";
    let size = 0;
    const append = (s: string) => {
      const n = byteLength(s);
      if (n > MAX_VALUE_BYTES - size) {
        throw new ResolveError(`vars: expanded value exceeds ${MAX_VALUE_BYTES} bytes`);
      }
      size += n;
      out += s;
    };
    for (const seg of splitRefs(raw, service)) {
      append(seg.kind === "text" ? seg.text : this.value(seg.target));
    }
    return out;
  }

  private cycleError(t: Target): ResolveError {
    const i = this.stack.findIndex((s) => id(s) === id(t));
    const names = [...this.stack.slice(i), t].map(label);
    return new ResolveError(`vars: reference cycle: ${names.join(" -> ")}`);
  }
}

function attempt<T>(fn: () => T): Outcome<T> {
  try {
    return { ok: true, value: fn() };
  } catch (e) {
    if (e instanceof ResolveError) return { ok: false, error: e.message };
    throw e;
  }
}

/**
 * resolve returns the variables of service self with all references
 * expanded, like vars.Resolve. Any error fails the whole set, as it fails a
 * deployment. Keys are visited in sorted order so cycle errors are
 * deterministic.
 */
export function resolve(self: string, all: Scope): Outcome<Record<string, string>> {
  return attempt(() => {
    const r = new Resolver(all);
    const out: Record<string, string> = {};
    const own = Object.hasOwn(all, self) ? all[self]! : {};
    for (const key of Object.keys(own).sort()) out[key] = r.value({ service: self, key });
    return out;
  });
}

/**
 * resolveKey expands a single variable on its own. Unlike resolve it does not
 * share a size budget with other keys, so the playground can show which keys
 * are fine while another one fails.
 */
export function resolveKey(self: string, key: string, all: Scope): Outcome<string> {
  return attempt(() => new Resolver(all).value({ service: self, key }));
}

/** Facts about a service that shed turns into injected variables. */
export type ServiceFacts = {
  name: string;
  kind: string;
  port: number;
  branch: string;
  /** Domains in creation order; the first becomes SHED_PUBLIC_DOMAIN. */
  domains: string[];
  /** The variables the user stored on the service. */
  own: Record<string, string>;
};

/** injectedVars mirrors injected() in internal/deploy: the variables shed adds to every service. */
export function injectedVars(
  project: string,
  svc: ServiceFacts,
  commitSha: string,
): Record<string, string> {
  const m: Record<string, string> = {
    SHED_PROJECT_NAME: project,
    SHED_SERVICE_NAME: svc.name,
    SHED_PRIVATE_DOMAIN: svc.name,
  };
  if (svc.kind === "app" && svc.port > 0) m.PORT = String(svc.port);
  if (svc.domains.length > 0) m.SHED_PUBLIC_DOMAIN = svc.domains[0]!;
  if (commitSha !== "") m.SHED_GIT_COMMIT_SHA = commitSha;
  if (svc.branch !== "") m.SHED_GIT_BRANCH = svc.branch;
  return m;
}

/**
 * buildScope merges injected and stored variables of every service, as
 * Deployer.environment does when deploying `selected`. Stored variables win
 * over injected ones, and only the selected service gets a commit SHA.
 */
export function buildScope(
  project: string,
  services: ServiceFacts[],
  selected: string,
  commitSha: string,
): Scope {
  const scope: Scope = {};
  for (const svc of services) {
    scope[svc.name] = {
      ...injectedVars(project, svc, svc.name === selected ? commitSha : ""),
      ...svc.own,
    };
  }
  return scope;
}
