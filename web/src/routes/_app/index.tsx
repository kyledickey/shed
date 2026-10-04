import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { FolderPlus, Plus } from "lucide-react";
import { useState } from "react";
import { projectsQuery, useCreateProject } from "../../api/projects";
import { Button } from "../../components/Button";
import { EmptyState } from "../../components/EmptyState";
import { Page } from "../../components/Page";
import { ProjectGrid } from "../../features/projects/ProjectGrid";
import { ProjectNameDialog } from "../../features/projects/ProjectNameDialog";

export const Route = createFileRoute("/_app/")({
  loader: ({ context }) => context.queryClient.ensureQueryData(projectsQuery),
  component: ProjectsPage,
});

function ProjectsPage() {
  const { data: projects } = useSuspenseQuery(projectsQuery);
  const [creating, setCreating] = useState(false);
  const create = useCreateProject();
  const navigate = useNavigate();

  const newButton = (
    <Button variant="primary" onClick={() => setCreating(true)}>
      <Plus size={14} />
      New project
    </Button>
  );

  return (
    <Page title="Projects" actions={projects.length > 0 && newButton}>
      {projects.length === 0 ? (
        <EmptyState icon={<FolderPlus size={18} />} title="No projects yet" action={newButton}>
          A project groups services that reach each other over a private network. Create one, then
          add an app or a database.
        </EmptyState>
      ) : (
        <ProjectGrid projects={projects} />
      )}
      <ProjectNameDialog
        open={creating}
        onOpenChange={(open) => {
          setCreating(open);
          create.reset();
        }}
        title="New project"
        submitLabel="Create project"
        pending={create.isPending}
        error={create.error}
        onSubmit={(name) =>
          create.mutate(name, {
            onSuccess: (project) =>
              navigate({ to: "/projects/$projectId", params: { projectId: project.id } }),
          })
        }
      />
    </Page>
  );
}
