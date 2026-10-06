import { createFileRoute, redirect } from "@tanstack/react-router";
import { meQuery, setupQuery } from "../api/auth";
import { isApiError } from "../api/client";
import { Consent } from "../features/auth/Consent";

/** request is the id of a pending authorization, set by /oauth/authorize. */
type AuthorizeSearch = { request?: string };

export const Route = createFileRoute("/authorize")({
  validateSearch: (search): AuthorizeSearch =>
    typeof search.request === "string" ? { request: search.request } : {},
  beforeLoad: async ({ context: { queryClient }, location }) => {
    try {
      await queryClient.ensureQueryData(meQuery);
    } catch (err) {
      if (!isApiError(err, 401)) throw err;
      const setup = await queryClient.fetchQuery(setupQuery);
      if (!setup.githubConfigured) throw redirect({ to: "/setup" });
      throw redirect({ to: "/login", search: { next: location.href } });
    }
  },
  component: AuthorizePage,
});

function AuthorizePage() {
  const { request } = Route.useSearch();
  return <Consent requestId={request} />;
}
