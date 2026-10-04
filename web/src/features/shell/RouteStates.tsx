import { useQueryErrorResetBoundary } from "@tanstack/react-query";
import { useRouter, type ErrorComponentProps } from "@tanstack/react-router";
import { CircleX, SearchX } from "lucide-react";
import { useEffect } from "react";
import { errorMessage, isApiError } from "../../api/client";
import { Button } from "../../components/Button";
import { Page } from "../../components/Layout";
import { EmptyState, Skeleton } from "../../components/Misc";

/** RoutePending is the router's loading state: a quiet skeleton page. */
export function RoutePending() {
  return (
    <Page>
      <div style={{ display: "flex", gap: 16, alignItems: "center" }}>
        <Skeleton width={44} height={44} radius={12} />
        <div style={{ display: "flex", flexDirection: "column", gap: 8, flex: 1 }}>
          <Skeleton width="30%" height={20} />
          <Skeleton width="45%" height={12} />
        </div>
      </div>
      <Skeleton height={220} radius={18} />
    </Page>
  );
}

/** RouteError is the router's error state, with a retry that refetches queries. */
export function RouteError({ error, reset }: ErrorComponentProps) {
  const router = useRouter();
  const { reset: resetQueries } = useQueryErrorResetBoundary();
  useEffect(() => resetQueries(), [resetQueries]);

  if (isApiError(error, 404)) {
    return (
      <Page>
        <EmptyState
          icon={<SearchX />}
          tone="neutral"
          title="Not found"
          description="It may have been deleted."
          actions={<Button onClick={() => void router.navigate({ to: "/" })}>All projects</Button>}
        />
      </Page>
    );
  }
  return (
    <Page>
      <EmptyState
        icon={<CircleX />}
        tone="tomato"
        title="Something went wrong"
        description={errorMessage(error)}
        actions={
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
    </Page>
  );
}
