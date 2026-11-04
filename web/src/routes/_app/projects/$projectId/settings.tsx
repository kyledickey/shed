import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useMemo } from "react";
import { projectQuery } from "../../../../api/projects";
import { Page } from "../../../../components/Layout";
import { PageHeader } from "../../../../components/PageHeader";
import { SettingsLayout } from "../../../../components/Settings";
import { DangerSection, GeneralSection } from "../../../../features/projects/ProjectSettings";

export const Route = createFileRoute("/_app/projects/$projectId/settings")({
  component: ProjectSettingsPage,
});

function ProjectSettingsPage() {
  const { projectId } = Route.useParams();
  const { data: project } = useSuspenseQuery(projectQuery(projectId));
  const sections = useMemo(
    () => [
      { id: "general", title: "General", content: <GeneralSection project={project} /> },
      {
        id: "danger",
        title: "Danger zone",
        danger: true,
        content: <DangerSection project={project} />,
      },
    ],
    [project],
  );
  return (
    <>
      <PageHeader
        crumbs={[
          { label: "Projects", href: "/" },
          { label: project.name, href: `/projects/${project.id}` },
        ]}
        title="Settings"
      />
      <Page>
        <SettingsLayout key={project.id} sections={sections} />
      </Page>
    </>
  );
}
