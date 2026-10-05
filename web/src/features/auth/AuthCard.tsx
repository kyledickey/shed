import type { ReactNode } from "react";
import { Card } from "../../components/Card";
import { Logo } from "../../components/Shell";
import styles from "./AuthCard.module.css";

type AuthCardProps = { title: string; description: ReactNode; children: ReactNode };

/** AuthCard is the centered card for pages shown before sign-in. */
export function AuthCard({ title, description, children }: AuthCardProps) {
  return (
    <main className={styles.screen}>
      <div className={styles.column}>
        <Logo size={36} />
        <Card className={styles.card}>
          <div className={styles.heading}>
            <h1 className={styles.title}>{title}</h1>
            <p className={styles.description}>{description}</p>
          </div>
          {children}
        </Card>
      </div>
    </main>
  );
}

/** authFormClass lays out a form inside an AuthCard. */
export const authFormClass = styles.form;
