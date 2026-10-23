import { Link } from "@tanstack/react-router";
import { ArrowRight } from "lucide-react";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import { useConnectOnePassword, useDisconnectOnePassword } from "../../api/onepassword";
import type { OnePassword } from "../../api/types";
import { Button } from "../../components/Button";
import { Field, Input } from "../../components/Form";
import { useToast } from "../../components/Overlay";
import { HelpTip } from "../docs/HelpTip";
import { ConfirmDialog } from "./ConfirmDialog";
import { SettingsCard } from "./SectionForm";
import styles from "./Settings.module.css";

/** OnePasswordCard moves every service's variables into 1Password, or back. */
export function OnePasswordCard({ status }: { status: OnePassword }) {
  const [token, setToken] = useState("");
  const [disconnecting, setDisconnecting] = useState(false);
  const connect = useConnectOnePassword();
  const disconnect = useDisconnectOnePassword();
  const toast = useToast();

  const form = {
    dirty: token.trim() !== "",
    pending: connect.isPending,
    error: connect.error,
    reset: () => {
      setToken("");
      connect.reset();
    },
    save: () =>
      connect.mutate(token.trim(), {
        onSuccess: () => {
          setToken("");
          toast.add({ type: "success", title: "1Password connected" });
        },
      }),
  };

  return (
    <SettingsCard
      title={
        <>
          1Password <HelpTip topic="onePassword" />
        </>
      }
      meta="Store variables in 1Password."
      form={status.connected ? undefined : form}
      footerStart={
        status.connected ? (
          <Button size="sm" variant="danger" onClick={() => setDisconnecting(true)}>
            Disconnect
          </Button>
        ) : (
          connect.isPending && (
            <span className={styles.muted}>Moving variables into 1Password…</span>
          )
        )
      }
    >
      <p className={styles.intro}>
        Keep every service's variables in your 1Password account instead of on this server. On
        1Password.com, create a service account that's allowed to create vaults, and paste its token
        here. shed creates a vault named shed, adds an item for each service, and moves your
        variables into it.{" "}
        <Link to="/docs/$slug" params={{ slug: "onepassword" }} className={styles.docs}>
          Docs <ArrowRight size={12} />
        </Link>
      </p>
      {status.connected ? (
        <dl className={styles.facts}>
          <div>
            <dt>Vault</dt>
            <dd>{status.vault}</dd>
          </div>
          <div>
            <dt>Services</dt>
            <dd>{status.items}</dd>
          </div>
        </dl>
      ) : (
        <Field label="Service account token">
          {(id) => (
            <Input
              id={id}
              mono
              type="password"
              value={token}
              onChange={(e) => {
                setToken(e.target.value);
                connect.reset();
              }}
              placeholder="ops_…"
              autoComplete="new-password"
              spellCheck={false}
              disabled={connect.isPending}
            />
          )}
        </Field>
      )}
      <ConfirmDialog
        open={disconnecting}
        onOpenChange={(open) => {
          setDisconnecting(open);
          disconnect.reset();
        }}
        title="Disconnect 1Password"
        confirmLabel="Disconnect"
        pending={disconnect.isPending}
        error={disconnect.error ? errorMessage(disconnect.error) : undefined}
        onConfirm={() =>
          disconnect.mutate(undefined, {
            onSuccess: () => {
              setDisconnecting(false);
              toast.add({ type: "success", title: "1Password disconnected" });
            },
          })
        }
      >
        shed copies every service's variables back to this server and forgets the token. The shed
        vault and its items stay in 1Password.
      </ConfirmDialog>
    </SettingsCard>
  );
}
