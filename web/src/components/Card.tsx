import type { ComponentProps, ReactNode } from "react";
import { cx } from "../lib/cx";
import styles from "./Card.module.css";

type CardProps = ComponentProps<"div"> & {
  /** Highlight on hover, for cards that link somewhere. */
  interactive?: boolean;
  padded?: boolean;
};

export function Card({ interactive, padded, className, ...rest }: CardProps) {
  return (
    <div
      className={cx(
        styles.card,
        interactive && styles.interactive,
        padded && styles.padded,
        className,
      )}
      {...rest}
    />
  );
}

type LayerCardProps = {
  /** Title in the outer layer. Ignored when `header` is given. */
  title?: ReactNode;
  /** Quiet text beside the title. */
  meta?: ReactNode;
  icon?: ReactNode;
  actions?: ReactNode;
  /** Replaces the whole header row, e.g. with a toolbar. */
  header?: ReactNode;
  footer?: ReactNode;
  /** Pad the inner sheet. Off for lists, logs and charts that pad themselves. */
  padded?: boolean;
  className?: string;
  sheetClassName?: string;
  children: ReactNode;
};

/**
 * LayerCard is two stacked sheets: a tinted outer layer that carries the
 * title, toolbar or footer, and a raised inner sheet that holds the content.
 */
export function LayerCard({
  title,
  meta,
  icon,
  actions,
  header,
  footer,
  padded,
  className,
  sheetClassName,
  children,
}: LayerCardProps) {
  const head =
    header ??
    (title || actions ? (
      <>
        {icon && <span className={styles.layerIcon}>{icon}</span>}
        <span className={styles.layerTitle}>{title}</span>
        {meta && <span className={styles.layerMeta}>{meta}</span>}
        {actions && <span className={styles.layerActions}>{actions}</span>}
      </>
    ) : null);
  return (
    <section className={cx(styles.layer, !head && styles.headless, className)}>
      {head && <header className={styles.layerHead}>{head}</header>}
      <div className={cx(styles.sheet, padded && styles.padded, sheetClassName)}>{children}</div>
      {footer && <footer className={styles.layerFoot}>{footer}</footer>}
    </section>
  );
}
