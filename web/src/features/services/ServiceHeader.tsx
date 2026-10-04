import { ArrowUpRight, Rocket } from "lucide-react";
import { useDeploy } from "../../api/deployments";
import type { Service } from "../../api/types";
import { Banner, ErrorText } from "../../components/Banner";
import { Button } from "../../components/Button";
import { kindLabels, ServiceIcon } from "../../components/ServiceIcon";
import { StatusDot } from "../../components/StatusDot";
import { useRedeployHint } from "./redeploy";
import styles from "./ServiceHeader.module.css";

export function ServiceHeader({ service }: { service: Service }) {
  const deploy = useDeploy(service.id);
  const hint = useRedeployHint();
  const domain = service.domains[0];
  const source = service.repo
    ? `${service.repo}@${service.branch}`
    : service.kind === "app"
      ? service.image
      : "";

  const onDeploy = () => deploy.mutate(undefined, { onSuccess: hint.clear });

  return (
    <div className={styles.wrap}>
      <header className={styles.header}>
        <ServiceIcon kind={service.kind} repo={service.repo} />
        <div className={styles.main}>
          <div className={styles.titleRow}>
            <h1 className={styles.title}>{service.name}</h1>
            <span className={styles.kind}>{kindLabels[service.kind]}</span>
          </div>
          <div className={styles.meta}>
            <StatusDot status={service.status} />
            {domain && (
              <a className={styles.domain} href={domain.url} target="_blank" rel="noreferrer">
                {domain.host}
                <ArrowUpRight size={12} />
              </a>
            )}
            {source && <span className={styles.source}>{source}</span>}
          </div>
        </div>
        <Button variant="primary" onClick={onDeploy} loading={deploy.isPending}>
          {!deploy.isPending && <Rocket size={14} />}
          Deploy
        </Button>
      </header>
      {hint.pending && (
        <Banner
          action={
            <Button size="sm" onClick={onDeploy} loading={deploy.isPending}>
              Redeploy
            </Button>
          }
        >
          Changes saved. Redeploy to apply them.
        </Banner>
      )}
      <ErrorText error={deploy.error} />
    </div>
  );
}
