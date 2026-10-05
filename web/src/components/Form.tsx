import { Select as BaseSelect } from "@base-ui/react/select";
import { Check, ChevronsUpDown } from "lucide-react";
import {
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ComponentProps,
  type CSSProperties,
  type RefObject,
  type ReactNode,
} from "react";
import { cx } from "../lib/cx";
import styles from "./Form.module.css";

type InputProps = Omit<ComponentProps<"input">, "prefix"> & {
  mono?: boolean;
  prefix?: ReactNode;
  suffix?: ReactNode;
};

export function Input({ mono, prefix, suffix, className, ...rest }: InputProps) {
  return (
    <span className={cx(styles.input, mono && styles.mono, className)}>
      {prefix && <span className={styles.affix}>{prefix}</span>}
      <input {...rest} />
      {suffix && <span className={styles.affix}>{suffix}</span>}
    </span>
  );
}

export function Textarea({
  mono,
  className,
  ...rest
}: ComponentProps<"textarea"> & { mono?: boolean }) {
  return <textarea className={cx(styles.textarea, mono && styles.mono, className)} {...rest} />;
}

type FieldProps = {
  label: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  children: (id: string) => ReactNode;
};

/** Field labels a control. The child receives the id to put on the control. */
export function Field({ label, hint, error, children }: FieldProps) {
  const id = useId();
  return (
    <div className={styles.field}>
      <label htmlFor={id} className={styles.label}>
        {label}
      </label>
      {children(id)}
      {error ? (
        <p className={styles.error}>{error}</p>
      ) : (
        hint && <p className={styles.hint}>{hint}</p>
      )}
    </div>
  );
}

type SwitchProps = {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label?: ReactNode;
  description?: ReactNode;
  disabled?: boolean;
};

export function Switch({ checked, onChange, label, description, disabled }: SwitchProps) {
  const control = (
    <input
      type="checkbox"
      role="switch"
      className={styles.switch}
      checked={checked}
      disabled={disabled}
      onChange={(e) => onChange(e.target.checked)}
      aria-label={typeof label === "string" ? label : undefined}
    />
  );
  if (!label) return control;
  return (
    <label className={styles.switchRow} data-disabled={disabled || undefined}>
      <span className={styles.switchText}>
        <span className={styles.switchLabel}>{label}</span>
        {description && <span className={styles.hint}>{description}</span>}
      </span>
      {control}
    </label>
  );
}

type SegmentedProps<T extends string> = {
  value: T;
  onChange: (value: T) => void;
  options: { value: T; label: ReactNode; title?: string }[];
  size?: "sm" | "md";
  label: string;
};

/** Segmented is a radio group of buttons with a sliding thumb. */
export function Segmented<T extends string>({
  value,
  onChange,
  options,
  size = "md",
  label,
}: SegmentedProps<T>) {
  const ref = useRef<HTMLDivElement>(null);
  const thumb = useThumb(ref, value);
  return (
    <div
      ref={ref}
      role="radiogroup"
      aria-label={label}
      className={cx(styles.segmented, size === "sm" && styles.segmentedSm)}
    >
      <span className={styles.thumb} style={thumb} aria-hidden />
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={o.value === value}
          title={o.title}
          data-value={o.value}
          className={styles.segment}
          onClick={() => onChange(o.value)}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

/**
 * useThumb positions a sliding indicator under the child of `ref` whose
 * data-value matches `value`. Shared by Segmented and Tabs.
 */
export function useThumb(ref: RefObject<HTMLElement | null>, value: string) {
  const [style, setStyle] = useState<CSSProperties>({ opacity: 0 });
  useLayoutEffect(() => {
    const root = ref.current;
    if (!root) return;
    const measure = () => {
      const el = root.querySelector<HTMLElement>(`[data-value="${CSS.escape(value)}"]`);
      if (!el) return setStyle({ opacity: 0 });
      setStyle({
        width: el.offsetWidth,
        height: el.offsetHeight,
        transform: `translate(${el.offsetLeft}px, ${el.offsetTop}px)`,
      });
    };
    measure();
    // Children can reflow without the root resizing, so watch them too.
    const ro = new ResizeObserver(measure);
    ro.observe(root);
    for (const child of root.children) ro.observe(child);
    // Web fonts change text widths; re-measure whenever a load finishes.
    document.fonts.addEventListener("loadingdone", measure);
    return () => {
      ro.disconnect();
      document.fonts.removeEventListener("loadingdone", measure);
    };
  }, [ref, value]);
  return style;
}

type SelectProps = {
  id?: string;
  value: string;
  onChange: (value: string) => void;
  options: { value: string; label: ReactNode }[];
  placeholder?: string;
  disabled?: boolean;
  mono?: boolean;
};

export function Select({ id, value, onChange, options, placeholder, disabled, mono }: SelectProps) {
  return (
    <BaseSelect.Root
      items={options}
      value={value || null}
      onValueChange={(next) => onChange(next ?? "")}
      disabled={disabled}
    >
      <BaseSelect.Trigger id={id} className={cx(styles.input, styles.trigger, mono && styles.mono)}>
        <BaseSelect.Value className={styles.value} placeholder={placeholder} />
        <BaseSelect.Icon className={styles.chevron}>
          <ChevronsUpDown size={14} />
        </BaseSelect.Icon>
      </BaseSelect.Trigger>
      <BaseSelect.Portal>
        <BaseSelect.Positioner
          className={styles.positioner}
          sideOffset={6}
          alignItemWithTrigger={false}
        >
          <BaseSelect.Popup className={styles.popup}>
            <BaseSelect.List>
              {options.map((o) => (
                <BaseSelect.Item key={o.value} value={o.value} className={styles.option}>
                  <BaseSelect.ItemText className={mono ? styles.mono : undefined}>
                    {o.label}
                  </BaseSelect.ItemText>
                  <BaseSelect.ItemIndicator className={styles.indicator}>
                    <Check size={14} />
                  </BaseSelect.ItemIndicator>
                </BaseSelect.Item>
              ))}
            </BaseSelect.List>
          </BaseSelect.Popup>
        </BaseSelect.Positioner>
      </BaseSelect.Portal>
    </BaseSelect.Root>
  );
}
