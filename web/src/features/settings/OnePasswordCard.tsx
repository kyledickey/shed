import { CircleCheck, CircleX } from "lucide-react";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import { useConnectOnePassword, useDisconnectOnePassword } from "../../api/onepassword";
import { Button } from "../../components/Button";
import { LayerCard } from "../../components/Card";
import { Field, Input } from "../../components/Form";
import { Callout, OnePasswordIcon } from "../../components/Misc";
import { useToast } from "../../components/Overlay";
import { HelpTip } from "../docs/HelpTip";
import { ConfirmDialog } from "./ConfirmDialog";
import styles from "./Settings.module.css";

/** OnePasswordCard connects or disconnects the 1Password service account token. */
export function OnePasswordCard({ connected }: { connected: boolean }) {
  return (
    <LayerCard
      icon={<OnePasswordIcon />}
      title={
        <>
          1Password <HelpTip topic="onePassword" />
        </>
      }
      meta="Variables from 1Password Environments."
      padded
    >
      <div className={styles.fields}>
        <p className={styles.note}>
          shed reads Environments with a service account token. Grant the service account read
          access to each Environment explicitly; vault access is not enough. While connected,
          1Password uses about 110 MB of shed's memory.
        </p>
        {connected ? <Connected /> : <ConnectForm />}
      </div>
    </LayerCard>
  );
}

function ConnectForm() {
  const [token, setToken] = useState("");
  const connect = useConnectOnePassword();
  const toast = useToast();
  return (
    <form
      className={styles.fields}
      onSubmit={(e) => {
        e.preventDefault();
        if (!token.trim()) return;
        connect.mutate(token.trim(), {
          onSuccess: () => toast.add({ type: "success", title: "1Password connected" }),
        });
      }}
    >
      <div className={styles.inline}>
        <div className={styles.grow}>
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
                autoComplete="off"
                spellCheck={false}
              />
            )}
          </Field>
        </div>
        <Button
          type="submit"
          variant="primary"
          loading={connect.isPending}
          disabled={!token.trim()}
        >
          Connect
        </Button>
      </div>
      {connect.error && (
        <Callout tone="tomato" icon={<CircleX size={16} />} title="Could not connect">
          {errorMessage(connect.error)}
        </Callout>
      )}
    </form>
  );
}

function Connected() {
  const [confirming, setConfirming] = useState(false);
  const disconnect = useDisconnectOnePassword();
  const toast = useToast();
  return (
    <div className={styles.danger}>
      <div className={styles.grow}>
        <Callout tone="grass" icon={<CircleCheck size={16} />} title="Connected">
          The token is saved encrypted and is never shown again.
        </Callout>
      </div>
      <Button variant="danger" onClick={() => setConfirming(true)}>
        Disconnect
      </Button>
      <ConfirmDialog
        open={confirming}
        onOpenChange={(open) => {
          setConfirming(open);
          disconnect.reset();
        }}
        title="Disconnect 1Password"
        confirmLabel="Disconnect"
        pending={disconnect.isPending}
        error={disconnect.error ? errorMessage(disconnect.error) : undefined}
        onConfirm={() =>
          disconnect.mutate(undefined, {
            onSuccess: () => {
              setConfirming(false);
              toast.add({ type: "success", title: "1Password disconnected" });
            },
          })
        }
      >
        shed forgets the token. Running services keep their variables, but deploys of services
        linked to an Environment fail until a token is connected again. The memory 1Password uses is
        freed on shed's next restart.
      </ConfirmDialog>
    </div>
  );
}
