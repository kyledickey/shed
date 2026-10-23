import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowRight, CircleX, Eye, EyeOff, PlugZap, Unlink } from "lucide-react";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import { onePasswordVariablesQuery } from "../../api/onepassword";
import { useUpdateService } from "../../api/services";
import type { Service, Variables } from "../../api/types";
import { Badge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { LayerCard } from "../../components/Card";
import { Input } from "../../components/Form";
import { Callout, CopyButton, OnePasswordIcon, Skeleton } from "../../components/Misc";
import { useToast } from "../../components/Overlay";
import { HelpTip } from "../docs/HelpTip";
import { useRedeployHint } from "../services/redeploy";
import styles from "./Variables.module.css";

/**
 * OnePasswordVars links a service to a 1Password Environment and lists the
 * Environment's variables. The service's own variables win on the same key.
 */
export function OnePasswordVars({
  service,
  variables,
  connected,
}: {
  service: Service;
  variables: Variables;
  connected: boolean;
}) {
  const environment = service.onePasswordEnvironment;
  const update = useUpdateService(service.id);
  const hint = useRedeployHint();
  const toast = useToast();

  const setEnvironment = (id: string) =>
    update.mutate(
      { onePasswordEnvironment: id },
      {
        onSuccess: () => {
          hint.markPending();
          toast.add({
            type: "success",
            title: id ? "Environment linked" : "Environment unlinked",
            description: "Redeploy to apply.",
          });
        },
      },
    );

  return (
    <LayerCard
      icon={<OnePasswordIcon />}
      title={
        <>
          1Password <HelpTip topic="onePassword" />
        </>
      }
      meta={environment ? <span className={styles.envId}>{environment}</span> : undefined}
      actions={
        environment && (
          <Button size="sm" loading={update.isPending} onClick={() => setEnvironment("")}>
            <Unlink size={13} /> Unlink
          </Button>
        )
      }
    >
      {!connected && (
        <div className={styles.section}>
          <Callout tone="sunflower" icon={<PlugZap size={16} />} title="1Password is not connected">
            {environment
              ? "Deploys of this service fail until a service account token is connected. "
              : "Connect a service account token before linking an Environment. "}
            <Link to="/settings" className={styles.link}>
              Settings <ArrowRight size={12} />
            </Link>
          </Callout>
        </div>
      )}
      {!environment ? (
        <LinkForm pending={update.isPending} onLink={setEnvironment} />
      ) : (
        connected && (
          <EnvironmentList serviceId={service.id} environment={environment} variables={variables} />
        )
      )}
      {update.error && <p className={styles.errors}>{errorMessage(update.error)}</p>}
      <p className={styles.hint}>
        {environment
          ? "Your variables above win on the same key. "
          : "Variables from a linked Environment are added under your own. "}
        Changes in 1Password apply on the next deploy, so redeploy after rotating a secret.
      </p>
    </LayerCard>
  );
}

function LinkForm({ pending, onLink }: { pending: boolean; onLink: (id: string) => void }) {
  const [id, setId] = useState("");
  return (
    <form
      className={styles.linkForm}
      onSubmit={(e) => {
        e.preventDefault();
        if (id.trim()) onLink(id.trim());
      }}
    >
      <Input
        mono
        className={styles.editValue}
        value={id}
        onChange={(e) => setId(e.target.value)}
        placeholder="Environment ID"
        aria-label="1Password Environment ID"
        spellCheck={false}
        autoComplete="off"
      />
      <Button type="submit" size="sm" variant="primary" loading={pending} disabled={!id.trim()}>
        Link
      </Button>
    </form>
  );
}

function EnvironmentList({
  serviceId,
  environment,
  variables,
}: {
  serviceId: string;
  environment: string;
  variables: Variables;
}) {
  const { data, error } = useQuery(onePasswordVariablesQuery(serviceId, environment));
  const [revealed, setRevealed] = useState<ReadonlySet<string>>(new Set());

  if (error) {
    return (
      <div className={styles.section}>
        <Callout tone="tomato" icon={<CircleX size={16} />} title="Could not read the Environment">
          {errorMessage(error)}
        </Callout>
      </div>
    );
  }
  if (!data) {
    return (
      <div className={styles.list}>
        {[0, 1, 2].map((i) => (
          <div key={i} className={styles.row}>
            <Skeleton width="40%" height={14} />
          </div>
        ))}
      </div>
    );
  }
  const entries = Object.entries(data).sort(([a], [b]) => (a < b ? -1 : 1));
  if (entries.length === 0) {
    return <p className={styles.empty}>This Environment has no variables.</p>;
  }

  const toggle = (key: string) =>
    setRevealed((prev) => {
      const next = new Set(prev);
      if (!next.delete(key)) next.add(key);
      return next;
    });

  return (
    <div className={styles.list}>
      {entries.map(([key, value]) => {
        const overridden = key in variables;
        const shown = revealed.has(key);
        return (
          <div key={key} className={styles.row} data-overridden={overridden || undefined}>
            <span className={styles.key}>
              <span className={styles.mark}>
                <OnePasswordIcon size={12} />
              </span>
              {key}
            </span>
            <span className={styles.value}>
              {shown ? value : <span className={styles.masked}>••••••••</span>}
            </span>
            {overridden && (
              <span title="A variable with this name above wins.">
                <Badge size="sm">overridden</Badge>
              </span>
            )}
            <span className={styles.actions}>
              <Button
                variant="ghost"
                size="sm"
                icon
                aria-label={shown ? "Hide" : "Reveal"}
                onClick={() => toggle(key)}
              >
                {shown ? <EyeOff size={14} /> : <Eye size={14} />}
              </Button>
              <CopyButton value={value} label={`Copy ${key}`} />
            </span>
          </div>
        );
      })}
    </div>
  );
}
