import { Link, type ErrorComponentProps } from "@tanstack/react-router";
import { LoaderCircle } from "lucide-react";
import { errorMessage, isApiError } from "../api/client";
import { buttonClass } from "./Button";
import { EmptyState } from "./EmptyState";
import styles from "./RouteStates.module.css";

export function PendingView() {
  return (
    <div className={styles.center} aria-label="Loading">
      <LoaderCircle className={styles.spinner} size={18} />
    </div>
  );
}

export function NotFoundView() {
  return (
    <div className={styles.center}>
      <EmptyState
        title="Not found"
        action={
          <Link to="/" className={buttonClass()}>
            Back to projects
          </Link>
        }
      >
        This page doesn't exist or was deleted.
      </EmptyState>
    </div>
  );
}

export function ErrorView({ error, reset }: ErrorComponentProps) {
  if (isApiError(error, 404)) return <NotFoundView />;
  return (
    <div className={styles.center}>
      <EmptyState
        title="Something went wrong"
        action={
          <button type="button" className={buttonClass()} onClick={reset}>
            Try again
          </button>
        }
      >
        {errorMessage(error)}
      </EmptyState>
    </div>
  );
}
