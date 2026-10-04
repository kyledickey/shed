import { createFileRoute, redirect } from "@tanstack/react-router";

// The dashboard is being rebuilt; until then the root shows the design kit.
export const Route = createFileRoute("/")({
  beforeLoad: () => {
    throw redirect({ to: "/showcase" });
  },
});
