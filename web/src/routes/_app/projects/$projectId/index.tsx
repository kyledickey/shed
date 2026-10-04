import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { Layers, Plus } from "lucide-react";
import { projectQuery } from "../../../../api/projects";
import { Button } from "../../../../components/Button";
import { Page } from "../../../../components/Layout";
import { EmptyState } from "../../../../components/Misc";
import { PageHeader } from "../../../../components/Shell";
import { servicesLabel } from "../../../../features/projects/graph";
import { ProjectMap, ServiceCards } from "../../../../features/projects/ProjectCanvas";
import { NewServiceDialog } from "../../../../features/services/NewServiceDialog";

type ProjectSearch = { new?: boolean };

export const Route = createFileRoute("/_app/projects/$projectId/")({
  validateSearch: (search): ProjectSearch => (search.new === true ? { new: true } : {}),
  component: ProjectPage,
});

function ProjectPage() {
  const { projectId } = Route.useParams();
  const { data: project } = useSuspenseQuery(projectQuery(projectId));
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const setAdding = (open: boolean) =>
    void navigate({ search: open ? { new: true } : {}, replace: !open });

  const newButton = (
    <Button variant="primary" onClick={() => setAdding(true)}>
      <Plus size={14} />
      New service
    </Button>
  );

  return (
    <Page>
      <PageHeader
        title={project.name}
        description={servicesLabel(project.services.length)}
        actions={project.services.length > 0 && newButton}
      />
      {project.services.length === 0 ? (
        <EmptyState
          icon={<Layers />}
          title="No services yet"
          description="Deploy a GitHub repo or a Docker image, or add a database. Services in a project reach each other by name."
          actions={newButton}
        />
      ) : (
        <>
          <ProjectMap projectId={project.id} services={project.services} />
          <ServiceCards services={project.services} />
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
  );
}
