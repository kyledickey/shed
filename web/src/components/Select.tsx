import { Select as Base } from "@base-ui/react/select";
import { Check, ChevronsUpDown } from "lucide-react";
import styles from "./Select.module.css";

type SelectProps = {
  id?: string;
  value: string;
  onChange: (value: string) => void;
  options: string[];
  placeholder?: string;
  disabled?: boolean;
  mono?: boolean;
};

export function Select({ id, value, onChange, options, placeholder, disabled, mono }: SelectProps) {
  return (
    <Base.Root
      value={value || null}
      onValueChange={(next) => onChange(next ?? "")}
      disabled={disabled}
    >
      <Base.Trigger id={id} className={`${styles.trigger} ${mono ? styles.mono : ""}`}>
        <Base.Value className={styles.value} placeholder={placeholder} />
        <Base.Icon className={styles.chevron}>
          <ChevronsUpDown size={14} />
        </Base.Icon>
      </Base.Trigger>
      <Base.Portal>
        <Base.Positioner className={styles.positioner} sideOffset={4} alignItemWithTrigger={false}>
          <Base.Popup className={styles.popup}>
            <Base.List className={styles.list}>
              {options.map((option) => (
                <Base.Item key={option} value={option} className={styles.item}>
                  <Base.ItemText className={mono ? styles.mono : undefined}>{option}</Base.ItemText>
                  <Base.ItemIndicator className={styles.indicator}>
                    <Check size={14} />
                  </Base.ItemIndicator>
                </Base.Item>
              ))}
            </Base.List>
          </Base.Popup>
        </Base.Positioner>
      </Base.Portal>
    </Base.Root>
  );
}
