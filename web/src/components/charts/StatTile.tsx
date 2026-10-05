import { ArrowDown, ArrowUp } from "lucide-react";
import type { ReactNode } from "react";
import type { Tone } from "../tone";
import styles from "./charts.module.css";
import { Sparkline } from "./Mini";

type StatTileProps = {
  label: ReactNode;
  icon?: ReactNode;
  value: ReactNode;
  unit?: ReactNode;
  /** Change versus the previous period; `good` decides its color, not its sign. */
  delta?: { label: string; up: boolean; good: boolean };
  spark?: (number | null)[];
  tone?: Tone;
  footer?: ReactNode;
};

/** StatTile is a headline number with an optional trend. */
export function StatTile({
  label,
  icon,
  value,
  unit,
  delta,
  spark,
  tone = "accent",
  footer,
}: StatTileProps) {
  return (
    <div className={styles.tile} data-tone={tone}>
      <div className={styles.tileHead}>
        {icon && <span className={styles.tileIcon}>{icon}</span>}
        <span className={styles.tileLabel}>{label}</span>
        {delta && (
          <span className={styles.delta} data-tone={delta.good ? "grass" : "tomato"}>
            {delta.up ? <ArrowUp size={11} /> : <ArrowDown size={11} />}
            {delta.label}
          </span>
        )}
      </div>
      <div className={styles.tileSheet}>
        <div className={styles.tileValue}>
          {value}
          {unit && <span className={styles.tileUnit}>{unit}</span>}
        </div>
        {spark && (
          <div className={styles.tileSpark}>
            <Sparkline values={spark} tone={tone} height={36} />
          </div>
        )}
        {footer && <div className={styles.tileFooter}>{footer}</div>}
      </div>
    </div>
  );
}
