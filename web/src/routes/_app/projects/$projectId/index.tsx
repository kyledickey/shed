import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { Button, LinkButton } from "@cloudflare/kumo";
import { ArrowSquareOutIcon, PlusIcon } from "@phosphor-icons/react";
import { Layers } from "lucide-react";
import { projectQuery } from "../../../../api/projects";
import { projectFolderUrl, telemetryQuery } from "../../../../api/telemetry";
import { Page } from "../../../../components/Layout";
import { EmptyState } from "../../../../components/Misc";
import { PageHeader } from "../../../../components/PageHeader";
import { useServiceEdges } from "../../../../features/projects/overview";
import { RecentDeployments } from "../../../../features/projects/ProjectActivity";
import { ProjectDiagram } from "../../../../features/projects/ProjectDiagram";
import { servicesLabel } from "../../../../features/projects/graph";
import { NewServiceDialog } from "../../../../features/services/NewServiceDialog";

type ProjectSearch = { new?: boolean };

export const Route = createFileRoute("/_app/projects/$projectId/")({
  validateSearch: (search): ProjectSearch => (search.new === true ? { new: true } : {}),
  component: ProjectPage,
});

function ProjectPage() {
  const { projectId } = Route.useParams();
  const { data: project } = useSuspenseQuery(projectQuery(projectId));
  const { data: telemetry } = useQuery(telemetryQuery);
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const setAdding = (open: boolean) =>
    void navigate({ search: open ? { new: true } : {}, replace: !open });

  const newButton = (
    <Button variant="primary" icon={<PlusIcon size={14} />} onClick={() => setAdding(true)}>
      New service
    </Button>
  );

  const edges = useServiceEdges(project.services);

  return (
    <>
      <PageHeader
        crumbs={[{ label: "Projects", href: "/" }]}
        title={project.name}
        meta={servicesLabel(project.services.length)}
        actions={
          <>
            {telemetry?.settings.enabled && (
              <LinkButton
                variant="secondary"
                href={projectFolderUrl(telemetry.grafanaUrl, project.id)}
                target="_blank"
                rel="noreferrer"
                icon={<ArrowSquareOutIcon size={14} />}
              >
                Open in Grafana
              </LinkButton>
            )}
            {project.services.length > 0 && newButton}
          </>
        }
      />
      <Page>
        {project.services.length === 0 ? (
          <EmptyState
            icon={<Layers />}
            title="No services yet"
            description="Deploy a GitHub repo or a Docker image, or add a database. Services in a project reach each other by name."
            actions={newButton}
          />
        ) : (
          <>
            <ProjectDiagram
              services={project.services}
              edges={edges}
              href={(s) => `/projects/${s.projectId}/services/${s.id}`}
            />
            <RecentDeployments services={project.services} />
          </>
        )}
        <NewServiceDialog
          projectId={project.id}
          projectName={project.name}
          taken={project.services.map((s) => s.name)}
          open={search.new === true}
          onOpenChange={setAdding}
        />
      </Page>
    </>
  );
}
