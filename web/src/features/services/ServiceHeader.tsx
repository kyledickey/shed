import { ArrowUpRight, Box, Lock, TriangleAlert } from "lucide-react";
import { errorMessage } from "../../api/client";
import { useDeploy } from "../../api/deployments";
import type { Service } from "../../api/types";
import { StatusBadge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { Callout, GitHubIcon, ServiceIcon } from "../../components/Misc";
import { useToast } from "../../components/Overlay";
import { useRedeployHint } from "./redeploy";
import styles from "./ServiceHeader.module.css";

/** serviceIcon picks the tile for a service: GitHub for repo apps, a box for image apps. */
export function serviceIcon(service: Pick<Service, "kind" | "repo">, size = 32) {
  if (service.kind !== "app") return <ServiceIcon kind={service.kind} size={size} />;
  return service.repo ? (
    <ServiceIcon kind="app" icon={<GitHubIcon />} size={size} />
  ) : (
    <ServiceIcon kind="app" icon={<Box />} tone="teal" size={size} />
  );
}

/** ServiceHeader titles every service page and offers a deploy after unapplied changes. */
export function ServiceHeader({ service }: { service: Service }) {
  const deploy = useDeploy(service.id);
  const hint = useRedeployHint();
  const toast = useToast();
  const domain = service.domains[0];

  const onDeploy = () =>
    deploy.mutate(undefined, {
      onSuccess: () => {
        hint.clear();
        toast.add({ title: "Deployment queued", description: service.name, type: "info" });
      },
      onError: (err) =>
        toast.add({ title: "Couldn't deploy", description: errorMessage(err), type: "error" }),
    });

  return (
    <div className={styles.wrap}>
      <header className={styles.header}>
        {serviceIcon(service, 44)}
        <div className={styles.main}>
          <div className={styles.titleRow}>
            <h1 className={styles.title}>{service.name}</h1>
            <StatusBadge kind="service" status={service.status} size="sm" />
          </div>
          <div className={styles.meta}>
            {service.repo ? (
              <a
                className={styles.metaLink}
                href={`https://github.com/${service.repo}/tree/${service.branch}`}
                target="_blank"
                rel="noreferrer"
              >
                <GitHubIcon size={12} />
                {service.repo}
                <span className={styles.branch}>{service.branch}</span>
              </a>
            ) : (
              service.image && <span className={styles.metaItem}>{service.image}</span>
            )}
            {domain && (
              <a className={styles.metaLink} href={domain.url} target="_blank" rel="noreferrer">
                {domain.host}
                <ArrowUpRight size={12} />
              </a>
            )}
            <span className={styles.metaItem}>
              <Lock size={12} />
              {service.privateHost}
              {service.port > 0 && `:${service.port}`}
            </span>
          </div>
        </div>
      </header>
      {hint.pending && (
        <Callout
          tone="sunflower"
          icon={<TriangleAlert size={16} />}
          title="Unapplied changes"
          actions={
            <Button size="sm" variant="primary" onClick={onDeploy} loading={deploy.isPending}>
              Deploy changes
            </Button>
          }
        >
          Changes are saved. Deploy to apply them.
        </Callout>
      )}
    </div>
  );
}
