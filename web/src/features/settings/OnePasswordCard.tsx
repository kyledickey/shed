import { Button, Input, Select } from "@cloudflare/kumo";
import { ArrowRightIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
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
import { useToast } from "../../components/Overlay";
import { Rows, SettingRow } from "../../components/Settings";
import { cx } from "../../lib/cx";
import { HelpTip } from "../docs/HelpTip";
import { ConfirmDialog } from "./ConfirmDialog";
import styles from "./Form.module.css";
import { Group } from "./Group";
import { SettingsForm } from "./SectionForm";

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

  const pending = list.isPending || connect.isPending;
  const form = {
    dirty: token.trim() !== "",
    pending,
    error: list.error ?? connect.error ?? (empty ? new Error(noVaultsMessage) : null),
    submitLabel: "Connect",
    message: pending ? "Moving variables into 1Password…" : "Connect with this token.",
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
    <Group
      title={
        <>
          <HelpTip topic="onePassword">1Password</HelpTip>
        </>
      }
      description={
        <>
          {status.connected
            ? `Every service's variables are kept in ${status.vault ? `the ${status.vault} vault` : "a vault"} in your 1Password account. Edit them on a service's Variables tab or in the 1Password app; edits made in the app apply on the next deploy.`
            : "Keep every service's variables in your 1Password account instead of on this server. Create a vault for shed, then on 1Password.com create a service account with read and write access to it and paste its token here. shed adds an item for each service and moves your variables into it."}{" "}
          <Link to="/docs/$slug" params={{ slug: "onepassword" }} className={styles.link}>
            Docs <ArrowRightIcon size={12} />
          </Link>
        </>
      }
    >
      {status.connected ? (
        <Rows>
          <SettingRow
            label="Vault"
            description={`Holds the variables of ${status.items} ${status.items === 1 ? "service" : "services"}.`}
            control={<span className={styles.value}>{status.vault}</span>}
          />
          <SettingRow
            label="Disconnect"
            description="Copy every service's variables back to this server and forget the token."
            control={
              <Button variant="secondary-destructive" onClick={() => setDisconnecting(true)}>
                Disconnect
              </Button>
            }
          />
        </Rows>
      ) : (
        <SettingsForm form={form}>
          <SettingRow
            label="Service account token"
            control={
              <Input
                aria-label="Service account token"
                className={cx(styles.wide, styles.mono)}
                type="password"
                value={token}
                onChange={(e) => {
                  setToken(e.target.value);
                  clear();
                }}
                placeholder="ops_…"
                autoComplete="new-password"
                spellCheck={false}
                disabled={pending}
              />
            }
          />
          {vaults.length > 1 && (
            <SettingRow
              label="Vault"
              description="The token can reach more than one vault. Choose the one for shed."
              control={
                <Select
                  aria-label="Vault"
                  className={styles.wide}
                  value={vault}
                  onValueChange={(v) => v && setVault(v)}
                  disabled={pending}
                  items={vaults.map((v) => ({ value: v.id, label: v.name }))}
                />
              }
            />
          )}
        </SettingsForm>
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
    </Group>
  );
}
