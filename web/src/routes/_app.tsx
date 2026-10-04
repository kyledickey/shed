import { createFileRoute, redirect } from "@tanstack/react-router";
import { meQuery, setupQuery } from "../api/auth";
import { isApiError } from "../api/client";
import { AppShell } from "../features/shell/AppShell";

export const Route = createFileRoute("/_app")({
  beforeLoad: async ({ context: { queryClient } }) => {
    try {
      await queryClient.ensureQueryData(meQuery);
    } catch (err) {
      if (!isApiError(err, 401)) throw err;
      const setup = await queryClient.fetchQuery(setupQuery);
      throw redirect({ to: setup.githubConfigured ? "/login" : "/setup" });
    }
  },
  component: AppShell,
});
