import type { ReactNode } from "react";
import { Logo } from "../shell/Logo";
import styles from "./AuthCard.module.css";

type AuthCardProps = { title: string; description: ReactNode; children: ReactNode };

/** AuthCard is the centered card for pages shown before sign-in: the shed mark above a form. */
export function AuthCard({ title, description, children }: AuthCardProps) {
  return (
    <main className={styles.screen}>
      <div className={styles.column}>
        <Logo size={40} />
        <section className={styles.card}>
          <div className={styles.heading}>
            <h1 className={styles.title}>{title}</h1>
            <p className={styles.description}>{description}</p>
          </div>
          {children}
        </section>
      </div>
    </main>
  );
}

/** authFormClass lays out a form inside an AuthCard. */
export const authFormClass = styles.form;

/** authErrorClass is a line of error text inside an AuthCard. */
export const authErrorClass = styles.error;

/** authButtonClass stretches a button across an AuthCard. */
export const authButtonClass = styles.wide;
