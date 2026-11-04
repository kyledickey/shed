import { Toasty, TooltipProvider } from "@cloudflare/kumo";
import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";
import { RestartScreen } from "../features/update/RestartScreen";

export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  component: Root,
});

function Root() {
  return (
    <TooltipProvider delay={300}>
      <Toasty>
        <Outlet />
        <RestartScreen />
      </Toasty>
    </TooltipProvider>
  );
}
