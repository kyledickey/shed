import { Button, Empty, SkeletonLine } from "@cloudflare/kumo";
import { MagnifyingGlassIcon, WarningCircleIcon } from "@phosphor-icons/react";
import { useQueryErrorResetBoundary } from "@tanstack/react-query";
import { useRouter, type ErrorComponentProps } from "@tanstack/react-router";
import { useEffect } from "react";
import { errorMessage, isApiError } from "../../api/client";
import styles from "./RouteStates.module.css";

/** RoutePending is the router's loading state: the outline of a page header and a card. */
export function RoutePending() {
  return (
    <div className={styles.page} aria-busy aria-label="Loading">
      <div className={styles.head}>
        <SkeletonLine minWidth={12} maxWidth={20} blockHeight={20} />
        <SkeletonLine minWidth={30} maxWidth={45} />
      </div>
      <div className={styles.card}>
        <SkeletonLine minWidth={40} maxWidth={70} />
        <SkeletonLine minWidth={30} maxWidth={60} />
        <SkeletonLine minWidth={45} maxWidth={80} />
        <SkeletonLine minWidth={20} maxWidth={50} />
      </div>
    </div>
  );
}

/** RouteError is the router's error state, with a retry that refetches queries. */
export function RouteError({ error, reset }: ErrorComponentProps) {
  const router = useRouter();
  const { reset: resetQueries } = useQueryErrorResetBoundary();
  useEffect(() => resetQueries(), [resetQueries]);

  if (isApiError(error, 404)) {
    return (
      <div className={styles.page}>
        <Empty
          className={styles.empty}
          icon={<MagnifyingGlassIcon size={48} className={styles.icon} />}
          title="Not found"
          description="It may have been deleted, or the link is wrong."
          contents={<Button onClick={() => void router.navigate({ to: "/" })}>All projects</Button>}
        />
      </div>
    );
  }
  return (
    <div className={styles.page}>
      <Empty
        className={styles.empty}
        icon={<WarningCircleIcon size={48} className={styles.icon} />}
        title="Something went wrong"
        description={errorMessage(error)}
        contents={
          <Button
            onClick={() => {
              reset();
              void router.invalidate();
            }}
          >
            Try again
          </Button>
        }
      />
    </div>
  );
}
