import { createFileRoute, redirect } from "@tanstack/react-router";
import { CircleX } from "lucide-react";
import { meQuery, setupQuery } from "../api/auth";
import { LinkButton } from "../components/Button";
import { Callout, GitHubIcon } from "../components/Misc";
import { AuthCard } from "../features/auth/AuthCard";

type LoginSearch = { error?: string };

export const Route = createFileRoute("/login")({
  validateSearch: (search): LoginSearch =>
    typeof search.error === "string" ? { error: search.error } : {},
  beforeLoad: async ({ context: { queryClient } }) => {
    const setup = await queryClient.fetchQuery(setupQuery);
    if (!setup.githubConfigured) throw redirect({ to: "/setup" });
    const me = await queryClient.fetchQuery(meQuery).catch(() => null);
    if (me) throw redirect({ to: "/" });
  },
  component: LoginPage,
});

function LoginPage() {
  const { error } = Route.useSearch();
  return (
    <AuthCard title="Sign in" description="Use the GitHub account that owns this shed.">
      {error && (
        <Callout tone="tomato" icon={<CircleX size={16} />}>
          {error === "denied"
            ? "That GitHub account isn't allowed to sign in here."
            : `Sign in failed: ${error}`}
        </Callout>
      )}
      <LinkButton href="/api/auth/login" variant="primary" size="lg">
        <GitHubIcon size={16} />
        Sign in with GitHub
      </LinkButton>
    </AuthCard>
  );
}
