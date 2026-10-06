import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowRight, Bot } from "lucide-react";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import { oauthGrantsQuery, useRevokeOAuthGrant } from "../../api/oauth";
import type { OAuthGrant } from "../../api/types";
import { Button } from "../../components/Button";
import { LayerCard } from "../../components/Card";
import { CopyButton, ServiceIcon, Skeleton } from "../../components/Misc";
import { useToast } from "../../components/Overlay";
import { formatDate, relativeTime } from "../../lib/time";
import { HelpTip } from "../docs/HelpTip";
import styles from "./AgentsCard.module.css";
import { ConfirmDialog } from "./ConfirmDialog";
import settings from "./Settings.module.css";

/** AgentsCard explains how to connect MCP clients and lists the ones that are connected. */
export function AgentsCard() {
  const url = `${window.location.origin}/mcp`;
  return (
    <LayerCard
      title={
        <>
          Agents <HelpTip topic="mcp" />
        </>
      }
      meta="Connect AI agents over MCP."
    >
      <div className={settings.section}>
        <p className={styles.intro}>
          Agents like Claude Code, Codex, and Cursor can read your projects, deployments, logs, and
          metrics to help diagnose problems. Access is read-only and never includes variable values.
          Each agent signs in through your browser with your GitHub account.
        </p>
        <Link to="/docs/$slug" params={{ slug: "mcp" }} className={styles.docs}>
          Read the MCP docs <ArrowRight size={12} />
        </Link>
      </div>
      <div className={settings.section}>
        <div className={styles.group}>
          <p className={settings.subhead}>Any agent</p>
          <p className={settings.note}>Pick your agents, such as Claude Code, Codex, or Cursor.</p>
          <CodeLine value={`npx add-mcp ${url} --name shed`} label="Copy command" />
        </div>
        <div className={styles.group}>
          <p className={settings.subhead}>Claude Code</p>
          <CodeLine value={`claude mcp add --transport http shed ${url}`} label="Copy command" />
        </div>
        <div className={styles.group}>
          <p className={settings.subhead}>Codex</p>
          <CodeLine value={`codex mcp add shed --url ${url}`} label="Copy command" />
        </div>
        <div className={styles.group}>
          <p className={settings.subhead}>Other clients</p>
          <p className={settings.note}>Add a remote MCP server (Streamable HTTP) with this URL.</p>
          <CodeLine value={url} label="Copy URL" />
        </div>
      </div>
      <ConnectedClients />
    </LayerCard>
  );
}

function CodeLine({ value, label }: { value: string; label: string }) {
  return (
    <div className={styles.code}>
      <code className={styles.codeValue}>{value}</code>
      <CopyButton value={value} label={label} />
    </div>
  );
}

function ConnectedClients() {
  const grants = useQuery(oauthGrantsQuery);
  const revoke = useRevokeOAuthGrant();
  const [revoking, setRevoking] = useState<OAuthGrant | null>(null);
  const toast = useToast();

  return (
    <div className={styles.clients}>
      <div className={styles.clientsHead}>
        <p className={settings.subhead}>Connected clients</p>
      </div>
      {grants.error ? (
        <p className={styles.empty}>{errorMessage(grants.error)}</p>
      ) : !grants.data ? (
        <div className={styles.empty}>
          <Skeleton width="50%" height={14} />
        </div>
      ) : grants.data.length === 0 ? (
        <p className={styles.empty}>No agents are connected.</p>
      ) : (
        <div className={settings.list}>
          {grants.data.map((g) => (
            <div key={g.id} className={settings.row}>
              <ServiceIcon icon={<Bot />} tone="neutral" size={28} />
              <div className={settings.rowMain}>
                <span className={styles.name}>{g.clientName || "Unnamed client"}</span>
                <span className={styles.details}>
                  {g.redirectHost} ·{" "}
                  <span title={formatDate(g.createdAt)}>connected {relativeTime(g.createdAt)}</span>{" "}
                  ·{" "}
                  {g.lastUsedAt ? (
                    <span title={formatDate(g.lastUsedAt)}>
                      last used {relativeTime(g.lastUsedAt)}
                    </span>
                  ) : (
                    "never used"
                  )}
                </span>
              </div>
              <Button size="sm" onClick={() => setRevoking(g)}>
                Revoke
              </Button>
            </div>
          ))}
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
    </div>
  );
}
