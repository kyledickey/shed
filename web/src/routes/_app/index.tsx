import { Button } from "@cloudflare/kumo";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { projectsQuery } from "../../api/projects";
import { PageHeader } from "../../components/PageHeader";
import { HelpTip } from "../../features/docs/HelpTip";
import { NewProjectDialog } from "../../features/projects/NewProjectDialog";
import { NoProjects, ProjectList } from "../../features/projects/ProjectList";
import styles from "../../features/projects/ProjectList.module.css";

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

  return (
    <>
      <PageHeader
        title={
          <>
            <HelpTip topic="projects">Projects</HelpTip>
          </>
        }
        description="Each project is a group of services that reach each other by name on a private network."
        actions={
          <Button variant="primary" onClick={() => setCreating(true)}>
            Create project
          </Button>
        }
      />
      <div className={styles.page}>
        {projects.length === 0 ? <NoProjects /> : <ProjectList projects={projects} />}
      </div>
      <NewProjectDialog open={search.new === true} onOpenChange={setCreating} />
    </>
  );
}
