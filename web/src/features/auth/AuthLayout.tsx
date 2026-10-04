import type { ReactNode } from "react";
import { Wordmark } from "../shell/Wordmark";
import styles from "./AuthLayout.module.css";

type AuthLayoutProps = { title: string; description: ReactNode; children: ReactNode };

export const authFormClass = styles.form;

export function AuthLayout({ title, description, children }: AuthLayoutProps) {
  return (
    <main className={styles.screen}>
      <div className={styles.panel}>
        <Wordmark />
        <div className={styles.heading}>
          <h1 className={styles.title}>{title}</h1>
          <p className={styles.description}>{description}</p>
        </div>
        {children}
      </div>
    </main>
  );
}
