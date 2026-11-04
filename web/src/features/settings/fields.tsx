import { InputGroup } from "@cloudflare/kumo";
import type { ChangeEvent, ReactNode } from "react";
import { cx } from "../../lib/cx";
import styles from "./Form.module.css";

type NumberInputProps = {
  "aria-label": string;
  value: number | string;
  onChange: (e: ChangeEvent<HTMLInputElement>) => void;
  /** Text after the number, such as "MiB" or "/s". */
  unit?: ReactNode;
  /** Text before the number, such as "Spans". */
  prefix?: ReactNode;
  min?: number;
  max?: number;
  step?: number | "any";
  disabled?: boolean;
  className?: string;
};

/** NumberInput is a monospace number field with an optional label before it and unit after it. */
export function NumberInput({
  unit,
  prefix,
  disabled,
  className,
  value,
  ...input
}: NumberInputProps) {
  return (
    <InputGroup className={cx(styles.number, className)} disabled={disabled}>
      {prefix && (
        <InputGroup.Addon>
          <span className={styles.unit}>{prefix}</span>
        </InputGroup.Addon>
      )}
      <InputGroup.Input
        className={styles.mono}
        type="number"
        inputMode="decimal"
        value={typeof value === "number" && Number.isNaN(value) ? "" : value}
        {...input}
      />
      {unit && (
        <InputGroup.Addon align="end">
          <span className={styles.unit}>{unit}</span>
        </InputGroup.Addon>
      )}
    </InputGroup>
  );
}

type InlineNumberProps = {
  "aria-label": string;
  value: number | string;
  onChange: (e: ChangeEvent<HTMLInputElement>) => void;
  min?: number;
  max?: number;
  step?: number | "any";
  disabled?: boolean;
  invalid?: boolean;
};

/**
 * InlineNumber is a small number field that sits inside a sentence, such as
 * "Above [90]% for [10] min". It is as wide as its value, and the words
 * around it are plain text.
 */
export function InlineNumber({ value, invalid, ...input }: InlineNumberProps) {
  const text = typeof value === "number" && Number.isNaN(value) ? "" : String(value);
  return (
    <input
      className={styles.inline}
      type="number"
      inputMode="decimal"
      aria-invalid={invalid || undefined}
      style={{ width: `calc(${Math.max(text.length, 2)}ch + 14px)` }}
      value={text}
      {...input}
    />
  );
}
