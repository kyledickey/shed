import { Button, ClipboardText, Loader, Table, Tabs } from "@cloudflare/kumo";
import { ArrowRightIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import { oauthGrantsQuery, useRevokeOAuthGrant } from "../../api/oauth";
import type { OAuthGrant } from "../../api/types";
import { useToast } from "../../components/Overlay";
import { SettingRow, Well } from "../../components/Settings";
import { formatDate, relativeTime } from "../../lib/time";
import { HelpTip } from "../docs/HelpTip";
import styles from "./AgentsCard.module.css";
import { ConfirmDialog } from "./ConfirmDialog";
import form from "./Form.module.css";
import { Group } from "./Group";

type Client = "any" | "claude" | "codex" | "url";

const clients: { value: Client; label: string }[] = [
  { value: "any", label: "Any agent" },
  { value: "claude", label: "Claude Code" },
  { value: "codex", label: "Codex" },
  { value: "url", label: "URL" },
];

function connect(client: Client, url: string): { value: string; note: string } {
  switch (client) {
    case "any":
      return {
        value: `npx add-mcp ${url} --name shed`,
        note: "Asks which agents to add shed to, like Claude Code, Codex, or Cursor.",
      };
    case "claude":
      return {
        value: `claude mcp add --transport http shed ${url}`,
        note: "Then run /mcp in Claude Code and choose shed to sign in.",
      };
    case "codex":
      return {
        value: `codex mcp add shed --url ${url} && codex mcp login shed`,
        note: "The login opens the sign-in page in your browser.",
      };
    case "url":
      return { value: url, note: "Add it as a remote MCP server (Streamable HTTP)." };
  }
}

/** AgentsCard explains how to connect MCP clients and lists the ones that are connected. */
export function AgentsCard() {
  const url = `${window.location.origin}/mcp`;
  const [client, setClient] = useState<Client>("any");
  const { value, note } = connect(client, url);
  return (
    <Group
      title={
        <>
          <HelpTip topic="mcp">Agents</HelpTip>
        </>
      }
      description={
        <>
          Agents can read your projects, deployments, logs, and metrics over MCP, never variable
          values. Each signs in through your browser with GitHub.{" "}
          <Link to="/docs/$slug" params={{ slug: "mcp" }} className={form.link}>
            Docs <ArrowRightIcon size={12} />
          </Link>
        </>
      }
    >
      <SettingRow
        label="Connect an agent"
        description={note}
        control={
          <Tabs
            variant="segmented"
            size="sm"
            value={client}
            onValueChange={(v) => setClient(v as Client)}
            tabs={clients}
          />
        }
      >
        <ClipboardText text={value} size="base" className={styles.command} />
      </SettingRow>
      <ConnectedClients />
    </Group>
  );
}

function ConnectedClients() {
  const grants = useQuery(oauthGrantsQuery);
  const revoke = useRevokeOAuthGrant();
  const [revoking, setRevoking] = useState<OAuthGrant | null>(null);
  const toast = useToast();

  return (
    <>
      <h4 className={form.subhead}>Connected clients</h4>
      {grants.error ? (
        <p className={form.error}>{errorMessage(grants.error)}</p>
      ) : !grants.data ? (
        <Well empty>
          <Loader size="sm" />
        </Well>
      ) : grants.data.length === 0 ? (
        <Well empty>No agents are connected.</Well>
      ) : (
        <div className={form.table}>
          <Table>
            <Table.Header>
              <Table.Row>
                <Table.Head>Client</Table.Head>
                <Table.Head>Redirect host</Table.Head>
                <Table.Head>Connected</Table.Head>
                <Table.Head>Last used</Table.Head>
                <Table.Head aria-label="Actions" />
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {grants.data.map((g) => (
                <Table.Row key={g.id}>
                  <Table.Cell className={styles.name}>
                    {g.clientName || "Unnamed client"}
                  </Table.Cell>
                  <Table.Cell className={form.cellMono}>{g.redirectHost}</Table.Cell>
                  <Table.Cell className={form.cellSubtle} title={formatDate(g.createdAt)}>
                    {relativeTime(g.createdAt)}
                  </Table.Cell>
                  <Table.Cell
                    className={form.cellSubtle}
                    title={g.lastUsedAt ? formatDate(g.lastUsedAt) : undefined}
                  >
                    {g.lastUsedAt ? relativeTime(g.lastUsedAt) : "Never"}
                  </Table.Cell>
                  <Table.Cell className={form.cellActions}>
                    <Button
                      size="sm"
                      variant="secondary-destructive"
                      onClick={() => setRevoking(g)}
                    >
                      Revoke
                    </Button>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </div>
      )}
      <ConfirmDialog
        open={!!revoking}
        onOpenChange={(open) => {
          if (!open) setRevoking(null);
          revoke.reset();
        }}
        title="Revoke access"
        confirmLabel="Revoke"
        pending={revoke.isPending}
        error={revoke.error ? errorMessage(revoke.error) : undefined}
        onConfirm={() =>
          revoking &&
          revoke.mutate(revoking.id, {
            onSuccess: () => {
              toast.add({ type: "success", title: "Access revoked" });
              setRevoking(null);
            },
          })
        }
      >
        <strong>{revoking?.clientName || "This client"}</strong> loses access immediately. To use it
        again, connect it again from the agent.
      </ConfirmDialog>
    </>
  );
}
