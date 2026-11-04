import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_app/server/")({
  beforeLoad: () => {
    throw redirect({ to: "/server/$section", params: { section: "metrics" }, replace: true });
  },
});
