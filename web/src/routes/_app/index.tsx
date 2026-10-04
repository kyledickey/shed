import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { FolderPlus, Plus } from "lucide-react";
import { projectsQuery } from "../../api/projects";
import { Button } from "../../components/Button";
import { Page } from "../../components/Layout";
import { EmptyState } from "../../components/Misc";
import { PageHeader } from "../../components/Shell";
import { NewProjectDialog } from "../../features/projects/NewProjectDialog";
import { ProjectGrid } from "../../features/projects/ProjectGrid";

type HomeSearch = { new?: boolean };

export const Route = createFileRoute("/_app/")({
  validateSearch: (search): HomeSearch => (search.new === true ? { new: true } : {}),
  loader: ({ context }) => context.queryClient.ensureQueryData(projectsQuery),
  component: ProjectsPage,
});

function ProjectsPage() {
  const { data: projects } = useSuspenseQuery(projectsQuery);
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const setCreating = (open: boolean) =>
    void navigate({ search: open ? { new: true } : {}, replace: !open });

  const newButton = (
    <Button variant="primary" onClick={() => setCreating(true)}>
      <Plus size={14} />
      New project
    </Button>
  );

  return (
    <Page>
      <PageHeader title="Projects" actions={newButton} />
      {projects.length === 0 ? (
        <EmptyState
          icon={<FolderPlus />}
          title="No projects yet"
          description="A project groups services that reach each other over a private network. Create one, then add an app or a database."
          actions={newButton}
        />
      ) : (
        <ProjectGrid projects={projects} onNew={() => setCreating(true)} />
      )}
      <NewProjectDialog open={search.new === true} onOpenChange={setCreating} />
    </Page>
  );
}
