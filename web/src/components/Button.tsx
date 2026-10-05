import { LoaderCircle } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import { cx } from "../lib/cx";
import styles from "./Button.module.css";
import type { Tone } from "./tone";

type Variant = "primary" | "secondary" | "ghost" | "soft" | "danger";
type Size = "sm" | "md" | "lg";

type Look = {
  variant?: Variant;
  size?: Size;
  /** Square, icon-only button. Pair with aria-label. */
  icon?: boolean;
  /** Crayon for the soft variant. */
  tone?: Tone;
};

export function buttonClass({ variant = "secondary", size = "md", icon }: Look = {}): string {
  return cx(styles.button, styles[variant], styles[size], icon && styles.icon);
}

type ButtonProps = ComponentProps<"button"> &
  Look & {
    loading?: boolean;
    children?: ReactNode;
  };

export function Button({
  variant,
  size,
  icon,
  tone = "accent",
  loading,
  disabled,
  className,
  children,
  type = "button",
  ...rest
}: ButtonProps) {
  return (
    <button
      type={type}
      className={cx(buttonClass({ variant, size, icon }), className)}
      data-tone={variant === "soft" ? tone : undefined}
      data-loading={loading || undefined}
      disabled={disabled || loading}
      {...rest}
    >
      {loading && <LoaderCircle className={styles.spinner} size={14} aria-hidden />}
      {children}
    </button>
  );
}

export function LinkButton({
  variant,
  size,
  icon,
  tone = "accent",
  className,
  ...rest
}: ComponentProps<"a"> & Look) {
  return (
    <a
      className={cx(buttonClass({ variant, size, icon }), className)}
      data-tone={variant === "soft" ? tone : undefined}
      {...rest}
    />
  );
}
