import { Link } from "@tanstack/react-router";
import { ArrowRight } from "lucide-react";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import {
  noVaultsMessage,
  planConnect,
  useConnectOnePassword,
  useDisconnectOnePassword,
  useOnePasswordVaults,
} from "../../api/onepassword";
import type { OnePassword, OnePasswordVault } from "../../api/types";
import { Button } from "../../components/Button";
import { Field, Input, Select } from "../../components/Form";
import { useToast } from "../../components/Overlay";
import { HelpTip } from "../docs/HelpTip";
import { ConfirmDialog } from "./ConfirmDialog";
import { SettingsCard } from "./SectionForm";
import styles from "./Settings.module.css";

/** OnePasswordCard moves every service's variables into 1Password, or back. */
export function OnePasswordCard({ status }: { status: OnePassword }) {
  const [token, setToken] = useState("");
  const [disconnecting, setDisconnecting] = useState(false);
  const [vaults, setVaults] = useState<OnePasswordVault[]>([]);
  const [vault, setVault] = useState("");
  const [empty, setEmpty] = useState(false);
  const list = useOnePasswordVaults();
  const connect = useConnectOnePassword();
  const disconnect = useDisconnectOnePassword();
  const toast = useToast();

  const clear = () => {
    setVaults([]);
    setVault("");
    setEmpty(false);
    list.reset();
    connect.reset();
  };

  const finish = (id?: string) =>
    connect.mutate(
      { token: token.trim(), vault: id },
      {
        onSuccess: () => {
          setToken("");
          clear();
          toast.add({ type: "success", title: "1Password connected" });
        },
      },
    );

  const form = {
    dirty: token.trim() !== "",
    pending: list.isPending || connect.isPending,
    error: list.error ?? connect.error ?? (empty ? new Error(noVaultsMessage) : null),
    submitLabel: "Connect",
    reset: () => {
      setToken("");
      clear();
    },
    save: () => {
      if (vaults.length > 1) return finish(vault);
      list.mutate(token.trim(), {
        onSuccess: (found) => {
          const plan = planConnect(found);
          if (plan.kind === "none") setEmpty(true);
          else if (plan.kind === "connect") finish(plan.vault);
          else {
            setVaults(plan.vaults);
            setVault(plan.vault);
          }
        },
      });
    },
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
          (connect.isPending || list.isPending) && (
            <span className={styles.muted}>Moving variables into 1Password…</span>
          )
        )
      }
    >
      <p className={styles.intro}>
        {status.connected
          ? `Every service's variables are kept in ${status.vault ? `the ${status.vault} vault` : "a vault"} in your 1Password account. Edit them on a service's Variables tab or in the 1Password app; edits made in the app apply on the next deploy.`
          : "Keep every service's variables in your 1Password account instead of on this server. In 1Password, create a vault for shed. Then on 1Password.com, create a service account with read and write access to that vault, and paste its token here. shed adds an item for each service to the vault and moves your variables into it."}{" "}
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
        <>
          <Field label="Service account token">
            {(id) => (
              <Input
                id={id}
                mono
                type="password"
                value={token}
                onChange={(e) => {
                  setToken(e.target.value);
                  clear();
                }}
                placeholder="ops_…"
                autoComplete="new-password"
                spellCheck={false}
                disabled={form.pending}
              />
            )}
          </Field>
          {vaults.length > 1 && (
            <Field label="Vault">
              {(id) => (
                <Select
                  id={id}
                  value={vault}
                  onChange={setVault}
                  disabled={form.pending}
                  options={vaults.map((v) => ({ value: v.id, label: v.name }))}
                />
              )}
            </Field>
          )}
        </>
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
        shed copies every service's variables back to this server and forgets the token. The vault
        and its items stay in 1Password.
      </ConfirmDialog>
    </SettingsCard>
  );
}
