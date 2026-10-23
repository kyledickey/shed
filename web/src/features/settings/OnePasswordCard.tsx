import { Link } from "@tanstack/react-router";
import { ArrowRight } from "lucide-react";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import { useConnectOnePassword, useDisconnectOnePassword } from "../../api/onepassword";
import { Button } from "../../components/Button";
import { Field, Input } from "../../components/Form";
import { useToast } from "../../components/Overlay";
import { HelpTip } from "../docs/HelpTip";
import { ConfirmDialog } from "./ConfirmDialog";
import { SettingsCard } from "./SectionForm";
import styles from "./Settings.module.css";

/** OnePasswordCard saves or forgets the 1Password service account token. */
export function OnePasswordCard({ connected }: { connected: boolean }) {
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
      meta="Load variables from 1Password."
      form={form}
      footerStart={
        connected && (
          <Button size="sm" variant="danger" onClick={() => setDisconnecting(true)}>
            Disconnect
          </Button>
        )
      }
    >
      <p className={styles.intro}>
        Keep your secrets in 1Password and shed loads them each time a service deploys. In
        1Password, create a service account that can read the Environments your services use, and
        paste its token here.{" "}
        <Link to="/docs/$slug" params={{ slug: "onepassword" }} className={styles.docs}>
          Docs <ArrowRight size={12} />
        </Link>
      </p>
      <Field
        label="Service account token"
        hint={connected ? "Leave blank to keep the saved token." : undefined}
      >
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
            placeholder={connected ? "••••••• (saved)" : "ops_…"}
            autoComplete="new-password"
            spellCheck={false}
          />
        )}
      </Field>
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
        shed forgets the token. Services that use a 1Password Environment keep running, but their
        next deploy fails until you connect 1Password again.
      </ConfirmDialog>
    </SettingsCard>
  );
}
