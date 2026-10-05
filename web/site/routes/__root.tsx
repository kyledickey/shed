import { createRootRoute, Outlet, useLocation } from "@tanstack/react-router";
import { useEffect } from "react";
import { TooltipProvider } from "../../src/components/Overlay";
import { headFor } from "../head";
import { SiteShell } from "../SiteShell";

export const Route = createRootRoute({
  component: Root,
});

function Root() {
  const { pathname } = useLocation();

  // The prerendered HTML has the right head; client navigation keeps it so.
  useEffect(() => {
    const head = headFor(pathname);
    document.title = head.title;
    document.querySelector('meta[name="description"]')?.setAttribute("content", head.description);
  }, [pathname]);

  return (
    <TooltipProvider delay={300}>
      <SiteShell>
        <Outlet />
      </SiteShell>
    </TooltipProvider>
  );
}
