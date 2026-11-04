import { Button } from "@cloudflare/kumo";
import { ArrowUpRightIcon, CloudArrowUpIcon, GithubLogoIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { errorMessage } from "../../api/client";
import { useDeploy } from "../../api/deployments";
import { projectQuery } from "../../api/projects";
import type { Service } from "../../api/types";
import { StatusBadge } from "../../components/Badge";
import { ServiceIcon } from "../../components/Misc";
import { Notice } from "../../components/Notice";
import { useToast } from "../../components/Overlay";
import { PageHeader } from "../../components/PageHeader";
import { HelpTip } from "../docs/HelpTip";
import { useRedeployHint } from "./redeploy";
import { RestoreFenceCallout } from "./RestoreFenceCallout";
import { serviceTabHref, visibleServiceTabs, type ServiceTab } from "./sections";
import { ServiceControls } from "./ServiceControls";
import styles from "./ServiceHeader.module.css";

/** serviceIcon picks the glyph for a service: GitHub for repo apps, a box for image apps. */
export function serviceIcon(service: Pick<Service, "kind" | "repo">, size = 32) {
  if (service.kind !== "app") return <ServiceIcon kind={service.kind} size={size} />;
  return service.repo ? (
    <ServiceIcon kind="app" icon={<GithubLogoIcon />} size={size} />
  ) : (
    <ServiceIcon kind="app" size={size} />
  );
}

/** ServiceHeader titles every service page and holds its run controls. */
export function ServiceHeader({ service, tab }: { service: Service; tab: ServiceTab }) {
  const { data: project } = useQuery(projectQuery(service.projectId));
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
    <PageHeader
      crumbs={[
        { label: "Projects", href: "/" },
        { label: project?.name ?? "Project", href: `/projects/${service.projectId}` },
      ]}
      title={service.name}
      meta={<StatusBadge kind="service" status={service.status} size="sm" />}
      description={
        <span className={styles.meta}>
          {service.repo ? (
            <a
              className={styles.link}
              href={`https://github.com/${service.repo}/tree/${service.branch}`}
              target="_blank"
              rel="noreferrer"
            >
              {service.repo}
              <span className={styles.branch}>{service.branch}</span>
            </a>
          ) : (
            service.image && (
              <span className={styles.mono} title={service.image}>
                {service.image.replace(/@sha256:[0-9a-f]+$/i, "")}
              </span>
            )
          )}
          {domain && (
            <a className={styles.link} href={domain.url} target="_blank" rel="noreferrer">
              {domain.host}
              <ArrowUpRightIcon size={12} />
            </a>
          )}
          <span title="Private hostname and port, reachable from the project's other services">
            Private{" "}
            <span className={styles.mono}>
              {service.privateHost}
              {service.port > 0 && `:${service.port}`}
            </span>
          </span>
        </span>
      }
      actions={<ServiceControls service={service} />}
      tabs={visibleServiceTabs(service).map((t) => ({
        value: t.value,
        label: t.label,
        href: serviceTabHref(service, t.value),
      }))}
      tab={tab}
    >
      <RestoreFenceCallout service={service} />
      {hint.pending && !service.restoreFence && (
        <Notice
          icon={<CloudArrowUpIcon size={16} />}
          title={
            <span className={styles.line}>
              <HelpTip topic="settingsApply">Unapplied changes</HelpTip>
              <span className={styles.detail}>
                Saved settings take effect with the next deploy.
              </span>
            </span>
          }
          actions={
            <Button size="sm" variant="primary" onClick={onDeploy} loading={deploy.isPending}>
              Deploy changes
            </Button>
          }
        />
      )}
    </PageHeader>
  );
}
