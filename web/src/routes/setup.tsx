import { Button, buttonVariants, Input, InputArea, Tabs } from "@cloudflare/kumo";
import { FileArrowUpIcon, GithubLogoIcon } from "@phosphor-icons/react";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { setupQuery, useImportApp } from "../api/auth";
import { errorMessage } from "../api/client";
import {
  AuthCard,
  authButtonClass,
  authErrorClass,
  authFormClass,
} from "../features/auth/AuthCard";
import styles from "../features/auth/Setup.module.css";

type SetupSearch = { error?: string };

type Mode = "create" | "existing";

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
  const [mode, setMode] = useState<Mode>("create");
  const [token, setToken] = useState("");
  return (
    <AuthCard
      title={mode === "create" ? "Create the GitHub App" : "Connect a GitHub App"}
      description={
        mode === "create"
          ? "shed uses a GitHub App for sign-in, repository access, and push-to-deploy. GitHub will ask you to confirm it, then to pick the repositories it can access."
          : "Reuse a GitHub App that shed created before. GitHub shows its secrets only once, so generate new ones in the app's settings."
      }
    >
      <Tabs
        variant="segmented"
        className={styles.modes}
        listClassName={styles.modeList}
        value={mode}
        onValueChange={(v) => setMode(v === "existing" ? "existing" : "create")}
        tabs={[
          { value: "create", label: "New app" },
          { value: "existing", label: "Existing app" },
        ]}
      />
      {error && (
        <p className={authErrorClass} role="alert">
          {errors[error] ?? `Setup failed: ${error}`}
        </p>
      )}
      {mode === "create" ? (
        <form method="get" action="/api/setup/github" className={authFormClass}>
          <TokenField value={token} onChange={setToken} />
          <Button
            type="submit"
            variant="primary"
            size="lg"
            className={authButtonClass}
            disabled={!token}
            icon={<GithubLogoIcon size={18} />}
          >
            Create GitHub App
          </Button>
        </form>
      ) : (
        <ExistingAppForm token={token} setToken={setToken} />
      )}
    </AuthCard>
  );
}

function TokenField({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <Input
      label="Setup token"
      description={
        <>
          Printed once in the server log (<code>shed.log</code> or <code>journalctl -u shed</code>).
        </>
      }
      name="token"
      className={styles.mono}
      required
      autoFocus
      autoComplete="off"
      value={value}
      onChange={(e) => onChange(e.target.value.trim())}
    />
  );
}

function ExistingAppForm({ token, setToken }: { token: string; setToken: (v: string) => void }) {
  const navigate = useNavigate();
  const importApp = useImportApp();
  const [appId, setAppId] = useState("");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [webhookSecret, setWebhookSecret] = useState("");
  const [privateKey, setPrivateKey] = useState("");
  const origin = window.location.origin;
  const complete = token && appId && clientId && clientSecret && webhookSecret && privateKey;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    importApp.mutate(
      { token, appId: Number(appId), clientId, clientSecret, webhookSecret, privateKey },
      { onSuccess: () => navigate({ to: "/login" }) },
    );
  };

  const text = (label: string, value: string, set: (v: string) => void) => (
    <Input
      label={label}
      className={styles.mono}
      required
      autoComplete="off"
      value={value}
      onChange={(e) => set(e.target.value.trim())}
    />
  );

  return (
    <form onSubmit={submit} className={authFormClass}>
      <p className={styles.note}>
        In the app's settings on GitHub, generate a client secret and a private key, and set a
        webhook secret. Its webhook URL must be <code>{origin}/api/github/webhook</code> and its
        callback URL <code>{origin}/api/auth/callback</code>.
      </p>
      <TokenField value={token} onChange={setToken} />
      <Input
        label="App ID"
        className={styles.mono}
        required
        inputMode="numeric"
        autoComplete="off"
        value={appId}
        onChange={(e) => setAppId(e.target.value.replace(/\D/g, ""))}
      />
      {text("Client ID", clientId, setClientId)}
      {text("Client secret", clientSecret, setClientSecret)}
      {text("Webhook secret", webhookSecret, setWebhookSecret)}
      <div className={styles.key}>
        <InputArea
          label="Private key"
          description="Paste the contents of the .pem file GitHub downloaded, or choose the file."
          className={styles.mono}
          required
          rows={5}
          spellCheck={false}
          placeholder="-----BEGIN RSA PRIVATE KEY-----"
          value={privateKey}
          onValueChange={setPrivateKey}
        />
        <label className={buttonVariants({ size: "sm" })}>
          <input
            type="file"
            accept=".pem"
            hidden
            onChange={async (e) => {
              const file = e.target.files?.[0];
              if (file) setPrivateKey(await file.text());
            }}
          />
          <FileArrowUpIcon size={14} />
          Choose .pem file
        </label>
      </div>
      {importApp.error && (
        <p className={authErrorClass} role="alert">
          {errorMessage(importApp.error)}
        </p>
      )}
      <Button
        type="submit"
        variant="primary"
        size="lg"
        className={authButtonClass}
        disabled={!complete}
        loading={importApp.isPending}
        icon={<GithubLogoIcon size={18} />}
      >
        Connect GitHub App
      </Button>
    </form>
  );
}
