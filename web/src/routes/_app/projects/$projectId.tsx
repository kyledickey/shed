import { createFileRoute, Outlet } from "@tanstack/react-router";
import { projectQuery } from "../../../api/projects";

export const Route = createFileRoute("/_app/projects/$projectId")({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(projectQuery(params.projectId)),
  component: Outlet,
});
