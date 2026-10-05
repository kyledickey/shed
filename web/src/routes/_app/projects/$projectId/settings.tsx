import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { projectQuery } from "../../../../api/projects";
import { Page } from "../../../../components/Layout";
import { PageHeader } from "../../../../components/Shell";
import { DangerCard, RenameCard } from "../../../../features/projects/ProjectSettings";

export const Route = createFileRoute("/_app/projects/$projectId/settings")({
  component: ProjectSettingsPage,
});

function ProjectSettingsPage() {
  const { projectId } = Route.useParams();
  const { data: project } = useSuspenseQuery(projectQuery(projectId));
  return (
    <Page>
      <PageHeader title="Settings" description={project.name} />
      <RenameCard key={project.id} project={project} />
      <DangerCard project={project} />
    </Page>
  );
}
