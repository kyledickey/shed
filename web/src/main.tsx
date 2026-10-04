import { MutationCache, QueryCache, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createRouter, RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { isApiError } from "./api/client";
import { keys } from "./api/keys";
import { RouteError, RoutePending } from "./features/shell/RouteStates";
import { routeTree } from "./routeTree.gen";
import "./styles/global.css";

function onUnauthorized(err: unknown, queryKey?: readonly unknown[]) {
  if (isApiError(err, 401) && queryKey?.[0] !== keys.me[0]) {
    queryClient.clear();
    void router.navigate({ to: "/login" });
  }
}

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5_000,
      retry: (count, err) => (!isApiError(err) || err.status >= 500) && count < 2,
    },
  },
  queryCache: new QueryCache({ onError: (err, query) => onUnauthorized(err, query.queryKey) }),
  mutationCache: new MutationCache({ onError: (err) => onUnauthorized(err) }),
});

const router = createRouter({
  routeTree,
  context: { queryClient },
  defaultPreload: "intent",
  defaultPreloadStaleTime: 0,
  scrollRestoration: true,
  defaultPendingComponent: RoutePending,
  defaultErrorComponent: RouteError,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
