import type { CSSProperties, ReactNode } from "react";
import s from "./showcase.module.css";

export function Section({
  title,
  description,
  actions,
  children,
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className={s.section}>
      <div className={s.sectionHead}>
        <div>
          <h2 className={s.sectionTitle}>{title}</h2>
          {description && <p className={s.sectionDescription}>{description}</p>}
        </div>
        {actions}
      </div>
      {children}
    </section>
  );
}

export function Specimen({ label, children }: { label: ReactNode; children: ReactNode }) {
  return (
    <div className={s.specimen}>
      <div className={s.specimenLabel}>{label}</div>
      <div className={s.specimenBody}>{children}</div>
    </div>
  );
}

export function Grid({ min = 280, children }: { min?: number; children: ReactNode }) {
  return (
    <div className={s.grid} style={{ "--min": `${min}px` } as CSSProperties}>
      {children}
    </div>
  );
}
