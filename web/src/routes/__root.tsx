import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";
import { ToastProvider, TooltipProvider } from "../components/Overlay";
import { RestartScreen } from "../features/update/RestartScreen";

export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  component: Root,
});

function Root() {
  return (
    <TooltipProvider delay={300}>
      <ToastProvider>
        <Outlet />
        <RestartScreen />
      </ToastProvider>
    </TooltipProvider>
  );
}
