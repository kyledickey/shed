import { MAX_REFERENCE_DEPTH, splitRefs, type Scope } from "../../lib/vars";

/** Step is one move of the resolver, with the stack and memo as they stand after it. */
export type Step = {
  kind: "enter" | "memo" | "missing" | "done" | "cycle" | "depth";
  /** The variable the step is about, as service.KEY. */
  ref: string;
  note: string;
  stack: string[];
  memo: [string, string][];
};

/** parseScope reads lines of service.KEY=value; lines that do not fit are ignored. */
export function parseScope(text: string): Scope {
  const scope: Scope = {};
  for (const line of text.split("\n")) {
    const eq = line.indexOf("=");
    const dot = line.indexOf(".");
    if (eq < 0 || dot < 0 || dot > eq) continue;
    const service = line.slice(0, dot).trim();
    const key = line.slice(dot + 1, eq).trim();
    if (!service || !key) continue;
    (scope[service] ??= {})[key] = line.slice(eq + 1);
  }
  return scope;
}

/**
 * traceResolve replays internal/vars.Resolve for service self and records
 * each step. It stops at the first error, as Resolve does.
 */
export function traceResolve(self: string, all: Scope): Step[] {
  const steps: Step[] = [];
  const memo = new Map<string, string>();
  const stack: string[] = [];
  let failed = false;

  const push = (kind: Step["kind"], ref: string, note: string) =>
    steps.push({ kind, ref, note, stack: [...stack], memo: [...memo] });

  const value = (service: string, key: string): string => {
    const ref = `${service}.${key}`;
    const known = memo.get(ref);
    if (known !== undefined) {
      push("memo", ref, "already finished: reuse the memo");
      return known;
    }
    const raw = all[service]?.[key];
    if (raw === undefined) {
      push("missing", ref, "no such variable: expands to the empty string");
      return "";
    }
    if (stack.includes(ref)) {
      push(
        "cycle",
        ref,
        `reference cycle: ${[...stack.slice(stack.indexOf(ref)), ref].join(" -> ")}`,
      );
      failed = true;
      return "";
    }
    if (stack.length >= MAX_REFERENCE_DEPTH) {
      push("depth", ref, `reference depth exceeds ${MAX_REFERENCE_DEPTH}`);
      failed = true;
      return "";
    }
    stack.push(ref);
    push("enter", ref, "push on the stack and expand its value");
    let out = "";
    for (const seg of splitRefs(raw, service)) {
      if (seg.kind === "text") {
        out += seg.text;
        continue;
      }
      out += value(seg.target.service, seg.target.key);
      if (failed) return "";
    }
    stack.pop();
    memo.set(ref, out);
    push("done", ref, `pop, and memoize ${JSON.stringify(out)}`);
    return out;
  };

  for (const key of Object.keys(all[self] ?? {}).sort()) {
    value(self, key);
    if (failed) break;
  }
  return steps;
}
