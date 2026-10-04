import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { Layers, Plus } from "lucide-react";
import { useState } from "react";
import { projectQuery } from "../../../../api/projects";
import { Button } from "../../../../components/Button";
import { EmptyState } from "../../../../components/EmptyState";
import { Page } from "../../../../components/Page";
import { ProjectMenu } from "../../../../features/projects/ProjectMenu";
import { NewServiceDialog } from "../../../../features/services/NewServiceDialog";
import { ServiceList } from "../../../../features/services/ServiceList";

export const Route = createFileRoute("/_app/projects/$projectId/")({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(projectQuery(params.projectId)),
  component: ProjectPage,
});

function ProjectPage() {
  const { projectId } = Route.useParams();
  const { data: project } = useSuspenseQuery(projectQuery(projectId));
  const [adding, setAdding] = useState(false);

  const newButton = (
    <Button variant="primary" onClick={() => setAdding(true)}>
      <Plus size={14} />
      New service
    </Button>
  );

  return (
    <Page
      title={project.name}
      actions={
        <>
          <ProjectMenu project={project} />
          {project.services.length > 0 && newButton}
        </>
      }
    >
      {project.services.length === 0 ? (
        <EmptyState icon={<Layers size={18} />} title="No services yet" action={newButton}>
          Deploy an app from a GitHub repository or Docker image, or add a database. Services in
          this project can reach each other by name.
        </EmptyState>
      ) : (
        <ServiceList project={project} />
      )}
      <NewServiceDialog projectId={projectId} open={adding} onOpenChange={setAdding} />
    </Page>
  );
}
