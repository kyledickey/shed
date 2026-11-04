import { LayerCard } from "@cloudflare/kumo";
import type { CSSProperties, ReactNode } from "react";
import { cx } from "../lib/cx";
import styles from "./RecordList.module.css";

type RecordListProps = {
  title: ReactNode;
  /** Beside the title, such as a count chip or a help tip. */
  meta?: ReactNode;
  /** On the right of the title strip: buttons, filters, or an arrow link. */
  actions?: ReactNode;
  /** Under the rows, such as a "Load more" button or a limit note. */
  footer?: ReactNode;
  /** Record rows, or an empty well when there are none. */
  children: ReactNode;
};

/**
 * RecordList is a card of records such as deployments, backups, or audit
 * events: a titled strip over a panel of rows.
 */
export function RecordList({ title, meta, actions, footer, children }: RecordListProps) {
  return (
    <LayerCard className={styles.card}>
      <LayerCard.Secondary className={styles.head}>
        <span className={styles.title}>
          {title}
          {meta}
        </span>
        {actions && <div className={styles.actions}>{actions}</div>}
      </LayerCard.Secondary>
      <LayerCard.Primary className={styles.body}>
        {children}
        {footer && <div className={styles.footer}>{footer}</div>}
      </LayerCard.Primary>
    </LayerCard>
  );
}

type RecordRowProps = {
  /** grid-template-columns for the row's cells. */
  columns: string;
  /** Columns below 760px; cells marked `optional` are hidden there. */
  narrowColumns?: string;
  /** Makes the whole row open something, such as a detail dialog. */
  onOpen?: () => void;
  /** Names what `onOpen` opens, for screen readers. */
  openLabel?: string;
  children: ReactNode;
};

/**
 * RecordRow is one record: cells on a grid, 44px tall. With `onOpen` the
 * row is clickable; links and buttons inside it stay clickable on their own.
 */
export function RecordRow({ columns, narrowColumns, onOpen, openLabel, children }: RecordRowProps) {
  return (
    <div
      className={cx(styles.row, onOpen && styles.openable)}
      style={{ "--cols": columns, "--cols-narrow": narrowColumns ?? columns } as CSSProperties}
    >
      {onOpen && (
        <button type="button" className={styles.open} onClick={onOpen} aria-label={openLabel} />
      )}
      {children}
    </div>
  );
}

type CellProps = {
  children?: ReactNode;
  /** Hidden on narrow screens. */
  optional?: boolean;
  className?: string;
  title?: string;
};

function cell(base: string) {
  return function Cell({ children, optional, className, title }: CellProps) {
    return (
      <span
        className={cx(styles.cell, base, className)}
        data-optional={optional || undefined}
        title={title}
      >
        {children}
      </span>
    );
  };
}

/** RecordId is a monospace identifier, such as a short commit SHA. */
export const RecordId = cell(styles.id!);

/** RecordMessage is an italic, subtle line that truncates, such as a commit message. */
export const RecordMessage = cell(styles.message!);

/** RecordText is a plain cell that truncates. */
export const RecordText = cell(styles.text!);

/** RecordMeta is a quiet cell, such as an actor or a time. */
export const RecordMeta = cell(styles.meta!);

/** RecordNote spans the whole row under its cells, such as an error message. */
export const RecordNote = cell(styles.note!);

/** RecordEnd holds the trailing actions, usually a ⋯ menu. */
export const RecordEnd = cell(styles.end!);

/** RecordEmpty is the well a record list shows when it has no rows. */
export function RecordEmpty({ children, action }: { children: ReactNode; action?: ReactNode }) {
  return (
    <div className={styles.empty}>
      <span>{children}</span>
      {action}
    </div>
  );
}
