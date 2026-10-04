import { Search } from "lucide-react";
import type { ComponentProps } from "react";
import styles from "./Input.module.css";

type InputProps = ComponentProps<"input"> & { mono?: boolean };

export function Input({ mono = false, className, ...rest }: InputProps) {
  return (
    <input
      className={[styles.input, mono && styles.mono, className].filter(Boolean).join(" ")}
      spellCheck={mono ? false : undefined}
      autoComplete="off"
      {...rest}
    />
  );
}

export function Textarea({ className, ...rest }: ComponentProps<"textarea">) {
  return (
    <textarea
      className={[styles.input, styles.textarea, className].filter(Boolean).join(" ")}
      spellCheck={false}
      {...rest}
    />
  );
}

export function SearchInput({ className, ...rest }: ComponentProps<"input">) {
  return (
    <div className={[styles.search, className].filter(Boolean).join(" ")}>
      <Search size={14} className={styles.searchIcon} aria-hidden />
      <Input type="search" aria-label={rest.placeholder} {...rest} />
    </div>
  );
}
