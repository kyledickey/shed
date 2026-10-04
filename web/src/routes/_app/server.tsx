import { createFileRoute, Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import { Archive, ChartArea, ScrollText } from "lucide-react";
import type { ReactNode } from "react";
import { Page } from "../../components/Layout";
import { PageHeader } from "../../components/Shell";
import { Tabs } from "../../components/Tabs";

export const Route = createFileRoute("/_app/server")({
  component: ServerLayout,
});

type ServerTab = "metrics" | "logs" | "backups";

const tabs: { value: ServerTab; label: string; icon: ReactNode }[] = [
  { value: "metrics", label: "Metrics", icon: <ChartArea size={15} /> },
  { value: "logs", label: "Logs", icon: <ScrollText size={15} /> },
  { value: "backups", label: "Backups", icon: <Archive size={15} /> },
];

function ServerLayout() {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const tab = tabs.find((t) => pathname === `/server/${t.value}`)?.value ?? "metrics";
  return (
    <Page wide>
      <PageHeader title="Server" description="The machine shed runs on, and shed itself." />
      <Tabs
        label="Server sections"
        variant="line"
        value={tab}
        items={tabs}
        onChange={(next) =>
          void navigate({ to: next === "metrics" ? "/server" : `/server/${next}` })
        }
      />
      <Outlet />
    </Page>
  );
}
