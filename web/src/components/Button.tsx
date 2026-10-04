import type { ComponentProps } from "react";
import { LoaderCircle } from "lucide-react";
import styles from "./Button.module.css";

type Variant = "primary" | "secondary" | "ghost" | "danger";
type Size = "sm" | "md";

type StyleProps = { variant?: Variant; size?: Size; icon?: boolean };

export function buttonClass({ variant = "secondary", size = "md", icon = false }: StyleProps = {}) {
  return [styles.button, styles[variant], styles[size], icon && styles.icon]
    .filter(Boolean)
    .join(" ");
}

type ButtonProps = ComponentProps<"button"> & StyleProps & { loading?: boolean };

export function Button({
  variant,
  size,
  icon,
  loading = false,
  className,
  disabled,
  children,
  type = "button",
  ...rest
}: ButtonProps) {
  return (
    <button
      type={type}
      className={[buttonClass({ variant, size, icon }), className].filter(Boolean).join(" ")}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...rest}
    >
      {loading && <LoaderCircle className={styles.spinner} size={14} aria-hidden />}
      {children}
    </button>
  );
}

type LinkButtonProps = ComponentProps<"a"> & StyleProps;

export function LinkButton({ variant, size, icon, className, ...rest }: LinkButtonProps) {
  return (
    <a
      className={[buttonClass({ variant, size, icon }), className].filter(Boolean).join(" ")}
      {...rest}
    />
  );
}
