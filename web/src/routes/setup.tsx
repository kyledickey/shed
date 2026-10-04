import { createFileRoute, redirect } from "@tanstack/react-router";
import { CircleX } from "lucide-react";
import { useState } from "react";
import { setupQuery } from "../api/auth";
import { Button } from "../components/Button";
import { Field, Input } from "../components/Form";
import { Callout, GitHubIcon } from "../components/Misc";
import { AuthCard, authFormClass } from "../features/auth/AuthCard";

type SetupSearch = { error?: string };

const errors: Record<string, string> = {
  invalid_token: "That setup token isn't valid. Copy it again from the server log.",
  failed: "GitHub App setup didn't finish. Check the server log, then try again.",
};

export const Route = createFileRoute("/setup")({
  validateSearch: (search): SetupSearch =>
    typeof search.error === "string" ? { error: search.error } : {},
  beforeLoad: async ({ context }) => {
    const setup = await context.queryClient.fetchQuery(setupQuery);
    if (setup.githubConfigured) throw redirect({ to: "/login" });
  },
  component: SetupPage,
});

function SetupPage() {
  const { error } = Route.useSearch();
  const [token, setToken] = useState("");
  return (
    <AuthCard
      title="Create the GitHub App"
      description="shed uses a GitHub App for sign-in, repository access, and push-to-deploy. GitHub will ask you to confirm it, then to pick the repositories it can access."
    >
      {error && (
        <Callout tone="tomato" icon={<CircleX size={16} />}>
          {errors[error] ?? `Setup failed: ${error}`}
        </Callout>
      )}
      <form method="get" action="/api/setup/github" className={authFormClass}>
        <Field
          label="Setup token"
          hint={
            <>
              Printed once in the server log (<code>shed.log</code> or{" "}
              <code>journalctl -u shed</code>).
            </>
          }
        >
          {(id) => (
            <Input
              id={id}
              name="token"
              mono
              required
              autoFocus
              autoComplete="off"
              value={token}
              onChange={(e) => setToken(e.target.value.trim())}
            />
          )}
        </Field>
        <Button type="submit" variant="primary" size="lg" disabled={!token}>
          <GitHubIcon size={16} />
          Create GitHub App
        </Button>
      </form>
    </AuthCard>
  );
}
