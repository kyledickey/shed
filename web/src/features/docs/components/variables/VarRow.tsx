import { Pencil, Undo2, X } from "lucide-react";
import { Badge } from "../../../../components/Badge";
import { Button } from "../../../../components/Button";
import { Input } from "../../../../components/Form";
import { splitRefs, type Outcome, type Scope, type Target } from "../../lib/vars";
import styles from "./Playground.module.css";

/** Where a variable's value comes from in the playground. */
export type Origin = "stored" | "edited" | "scratch" | "injected";

/** rowId is the DOM id of a variable row, used to scroll a clicked reference into view. */
export function rowId(t: Target): string {
  return `docs-var-${t.service}-${t.key}`;
}

function has(scope: Scope, t: Target): boolean {
  return Object.hasOwn(scope, t.service) && Object.hasOwn(scope[t.service]!, t.key);
}

/** RawValue shows a value with each ${{ }} reference as a button. */
function RawValue({
  raw,
  service,
  scope,
  onJump,
}: {
  raw: string;
  service: string;
  scope: Scope;
  onJump: (t: Target) => void;
}) {
  if (raw === "") return <span className={styles.empty}>(empty)</span>;
  return (
    <>
      {splitRefs(raw, service).map((seg, i) => {
        if (seg.kind === "text") {
          return <span key={i}>{seg.text}</span>;
        }
        const missing = !has(scope, seg.target);
        return (
          <button
            key={i}
            type="button"
            className={styles.chip}
            data-missing={missing || undefined}
            disabled={missing}
            title={
              missing
                ? `${seg.target.service}.${seg.target.key} does not exist, so this expands to an empty string`
                : `Jump to ${seg.target.service}.${seg.target.key}`
            }
            onClick={() => onJump(seg.target)}
          >
            {seg.text}
          </button>
        );
      })}
    </>
  );
}

function Resolved({ outcome }: { outcome: Outcome<string> }) {
  if (!outcome.ok) return <span className={styles.error}>{outcome.error}</span>;
  if (outcome.value === "") return <span className={styles.empty}>(empty)</span>;
  return <>{outcome.value}</>;
}

const badges: Record<Origin, { label: string; tone: "neutral" | "accent" | "sunflower" }> = {
  stored: { label: "stored", tone: "neutral" },
  edited: { label: "edited here", tone: "sunflower" },
  scratch: { label: "scratch", tone: "sunflower" },
  injected: { label: "injected", tone: "accent" },
};

/** VarRow is one variable: its raw value with reference chips, and what it resolves to. */
export function VarRow({
  service,
  name,
  raw,
  origin,
  overridesInjected,
  scope,
  resolved,
  focused,
  editing,
  onEditToggle,
  onChange,
  onReset,
  onJump,
}: {
  service: string;
  name: string;
  raw: string;
  origin: Origin;
  overridesInjected: boolean;
  scope: Scope;
  resolved: Outcome<string>;
  focused: boolean;
  editing: boolean;
  onEditToggle: () => void;
  onChange: (value: string) => void;
  onReset: () => void;
  onJump: (t: Target) => void;
}) {
  const revertible = origin === "edited" || origin === "scratch";
  return (
    <div
      id={rowId({ service, key: name })}
      className={styles.row}
      data-focus={focused || undefined}
    >
      <div className={styles.rowTop}>
        <span className={styles.key}>{name}</span>
        <Badge size="sm" tone={badges[origin].tone}>
          {badges[origin].label}
        </Badge>
        {overridesInjected && (
          <Badge size="sm" tone="neutral">
            overrides injected
          </Badge>
        )}
        {origin !== "injected" && (
          <span className={styles.rowActions}>
            <Button
              size="sm"
              variant="ghost"
              icon
              aria-label={`Edit ${name} locally`}
              title="Edit locally. Nothing is saved."
              onClick={onEditToggle}
            >
              <Pencil size={13} />
            </Button>
            {revertible && (
              <Button
                size="sm"
                variant="ghost"
                icon
                aria-label={origin === "scratch" ? `Remove ${name}` : `Undo edit of ${name}`}
                title={origin === "scratch" ? "Remove" : "Undo edit"}
                onClick={onReset}
              >
                {origin === "scratch" ? <X size={13} /> : <Undo2 size={13} />}
              </Button>
            )}
          </span>
        )}
      </div>
      <div className={styles.line}>
        <span className={styles.label}>Raw</span>
        <span className={styles.value}>
          {editing ? (
            <Input
              mono
              aria-label={`Value of ${name}`}
              value={raw}
              onChange={(e) => onChange(e.target.value)}
            />
          ) : (
            <RawValue raw={raw} service={service} scope={scope} onJump={onJump} />
          )}
        </span>
      </div>
      <div className={styles.line}>
        <span className={styles.label}>Result</span>
        <span className={styles.value}>
          <Resolved outcome={resolved} />
        </span>
      </div>
    </div>
  );
}
