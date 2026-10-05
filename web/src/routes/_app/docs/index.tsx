import { createFileRoute, redirect } from "@tanstack/react-router";

export const Route = createFileRoute("/_app/docs/")({
  beforeLoad: () => {
    throw redirect({ to: "/docs/$slug", params: { slug: "overview" }, replace: true });
  },
});
