import { createFileRoute, redirect } from "@tanstack/react-router";
import { CircleX } from "lucide-react";
import { meQuery, setupQuery } from "../api/auth";
import { LinkButton } from "../components/Button";
import { Callout, GitHubIcon } from "../components/Misc";
import { AuthCard } from "../features/auth/AuthCard";
import { safeNext } from "../lib/next";

/** next is a relative path to return to after signing in. */
type LoginSearch = { error?: string; next?: string };

export const Route = createFileRoute("/login")({
  validateSearch: (search): LoginSearch => ({
    ...(typeof search.error === "string" ? { error: search.error } : {}),
    ...(safeNext(search.next) ? { next: safeNext(search.next) } : {}),
  }),
  beforeLoad: async ({ context: { queryClient }, search }) => {
    const setup = await queryClient.fetchQuery(setupQuery);
    if (!setup.githubConfigured) throw redirect({ to: "/setup" });
    const me = await queryClient.fetchQuery(meQuery).catch(() => null);
    // next may be a server route such as /oauth/authorize, so leave the SPA for it.
    if (me) {
      throw search.next
        ? redirect({ href: search.next, reloadDocument: true })
        : redirect({ to: "/" });
    }
  },
  component: LoginPage,
});

function LoginPage() {
  const { error, next } = Route.useSearch();
  return (
    <AuthCard title="Sign in" description="Use the GitHub account that owns this shed.">
      {error && (
        <Callout tone="tomato" icon={<CircleX size={16} />}>
          {error === "denied"
            ? "That GitHub account isn't allowed to sign in here."
            : `Sign in failed: ${error}`}
        </Callout>
      )}
      <LinkButton
        href={next ? `/api/auth/login?next=${encodeURIComponent(next)}` : "/api/auth/login"}
        variant="primary"
        size="lg"
      >
        <GitHubIcon size={16} />
        Sign in with GitHub
      </LinkButton>
    </AuthCard>
  );
}
