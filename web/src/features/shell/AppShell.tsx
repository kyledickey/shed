import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { Outlet, useLocation, useNavigate, useParams } from "@tanstack/react-router";
import {
  Archive,
  ChartArea,
  Command as CommandIcon,
  FolderPlus,
  Layers,
  LayoutGrid,
  LogOut,
  Monitor,
  Moon,
  Plus,
  Rocket,
  ScrollText,
  Settings,
  SlidersHorizontal,
  Sun,
} from "lucide-react";
import { useState, type ReactNode } from "react";
import { meQuery, useLogout } from "../../api/auth";
import { errorMessage } from "../../api/client";
import { useDeploy } from "../../api/deployments";
import { projectQuery, projectsQuery } from "../../api/projects";
import { serviceQuery } from "../../api/services";
import type { Service } from "../../api/types";
import { Button } from "../../components/Button";
import { CommandPalette, type Command } from "../../components/CommandPalette";
import { Avatar, Kbd, ServiceIcon } from "../../components/Misc";
import {
  Menu,
  MenuItem,
  MenuLabel,
  MenuSeparator,
  Tooltip,
  useToast,
} from "../../components/Overlay";
import { Crumbs, Logo, Shell, type Crumb } from "../../components/Shell";
import { Tabs } from "../../components/Tabs";
import { useTheme, type Theme } from "../../lib/theme";
import styles from "./AppShell.module.css";

export type ServiceTab = "deployments" | "logs" | "metrics" | "variables" | "backups" | "settings";
type ProjectTab = "services" | "settings";
type RootTab = "projects" | "settings";

const serviceTabs: { value: ServiceTab; label: string; icon: ReactNode }[] = [
  { value: "deployments", label: "Deployments", icon: <Rocket size={15} /> },
  { value: "logs", label: "Logs", icon: <ScrollText size={15} /> },
  { value: "metrics", label: "Metrics", icon: <ChartArea size={15} /> },
  { value: "variables", label: "Variables", icon: <SlidersHorizontal size={15} /> },
  { value: "backups", label: "Backups", icon: <Archive size={15} /> },
  { value: "settings", label: "Settings", icon: <Settings size={15} /> },
];

const projectTabs: { value: ProjectTab; label: string; icon: ReactNode }[] = [
  { value: "services", label: "Services", icon: <Layers size={15} /> },
  { value: "settings", label: "Settings", icon: <Settings size={15} /> },
];

const rootTabs: { value: RootTab; label: string; icon: ReactNode }[] = [
  { value: "projects", label: "Projects", icon: <Layers size={15} /> },
  { value: "settings", label: "Settings", icon: <Settings size={15} /> },
];

/** visibleServiceTabs hides Backups from services that have no volume to back up. */
function visibleServiceTabs(service?: Service) {
  return serviceTabs.filter((t) => t.value !== "backups" || (service?.volumes.length ?? 0) > 0);
}

/** AppShell frames every signed-in page: switcher crumbs, section tabs and account actions. */
export function AppShell() {
  const { projectId, serviceId } = useParams({ strict: false });
  const [palette, setPalette] = useState(false);
  return (
    <Shell
      brand={<Brand projectId={projectId} serviceId={serviceId} />}
      nav={<Nav projectId={projectId} serviceId={serviceId} />}
      actions={
        <>
          <Tooltip
            content={
              <>
                Command palette <Kbd>⌘K</Kbd>
              </>
            }
          >
            <Button
              variant="ghost"
              icon
              aria-label="Command palette"
              onClick={() => setPalette(true)}
            >
              <CommandIcon size={16} />
            </Button>
          </Tooltip>
          <ThemeButton />
          <UserMenu />
        </>
      }
    >
      <Outlet />
      <Palette
        open={palette}
        onOpenChange={setPalette}
        projectId={projectId}
        serviceId={serviceId}
      />
    </Shell>
  );
}

function Brand({ projectId, serviceId }: { projectId?: string; serviceId?: string }) {
  const navigate = useNavigate();
  const projects = useQuery(projectsQuery);
  const project = useQuery({ ...projectQuery(projectId ?? ""), enabled: !!projectId });
  const service = useQuery({ ...serviceQuery(serviceId ?? ""), enabled: !!serviceId });

  const items: Crumb[] = [];
  if (projectId) {
    items.push({
      label: project.data?.name ?? "…",
      menu: (
        <>
          <MenuLabel>Projects</MenuLabel>
          {projects.data?.map((p) => (
            <MenuItem
              key={p.id}
              icon={<LayoutGrid size={14} />}
              onClick={() =>
                void navigate({ to: "/projects/$projectId", params: { projectId: p.id } })
              }
            >
              {p.name}
            </MenuItem>
          ))}
          <MenuSeparator />
          <MenuItem icon={<Layers size={14} />} onClick={() => void navigate({ to: "/" })}>
            All projects
          </MenuItem>
        </>
      ),
    });
  }
  if (projectId && serviceId) {
    items.push({
      label: service.data?.name ?? "…",
      icon: service.data && <ServiceIcon kind={service.data.kind} size={18} />,
      menu: (
        <>
          <MenuLabel>Services</MenuLabel>
          {project.data?.services.map((s) => (
            <MenuItem
              key={s.id}
              icon={<ServiceIcon kind={s.kind} size={18} />}
              onClick={() =>
                void navigate({
                  to: "/projects/$projectId/services/$serviceId",
                  params: { projectId, serviceId: s.id },
                })
              }
            >
              {s.name}
            </MenuItem>
          ))}
        </>
      ),
    });
  }

  return (
    <>
      <button
        type="button"
        className={styles.home}
        aria-label="All projects"
        onClick={() => void navigate({ to: "/" })}
      >
        <Logo />
      </button>
      {items.length > 0 && <Crumbs items={items} />}
    </>
  );
}

/** activeTab is the last path segment after `base`, or `fallback` at the base itself. */
function activeTab<T extends string>(pathname: string, base: string, tabs: T[], fallback: T): T {
  const rest = pathname.slice(base.length).replace(/^\/|\/$/g, "");
  return tabs.find((t) => t === rest) ?? fallback;
}

function Nav({ projectId, serviceId }: { projectId?: string; serviceId?: string }) {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const service = useQuery({ ...serviceQuery(serviceId ?? ""), enabled: !!serviceId });

  if (projectId && serviceId) {
    const base = `/projects/${projectId}/services/${serviceId}`;
    const tab = activeTab(
      pathname,
      base,
      serviceTabs.map((t) => t.value),
      "deployments",
    );
    return (
      <Tabs
        label="Service sections"
        value={tab}
        items={visibleServiceTabs(service.data)}
        onChange={(next) =>
          void navigate({ to: next === "deployments" ? base : `${base}/${next}` })
        }
      />
    );
  }
  if (projectId) {
    const base = `/projects/${projectId}`;
    const tab = activeTab(pathname, base, ["settings"] as ProjectTab[], "services");
    return (
      <Tabs
        label="Project sections"
        value={tab}
        items={projectTabs}
        onChange={(next) => void navigate({ to: next === "services" ? base : `${base}/${next}` })}
      />
    );
  }
  return (
    <Tabs
      label="Sections"
      value={activeTab(pathname, "", ["settings"] as RootTab[], "projects")}
      items={rootTabs}
      onChange={(next) => void navigate({ to: next === "projects" ? "/" : "/settings" })}
    />
  );
}

const themeIcons: Record<Theme, ReactNode> = {
  system: <Monitor size={16} />,
  light: <Sun size={16} />,
  dark: <Moon size={16} />,
};

function ThemeButton() {
  const [theme, setTheme] = useTheme();
  const next: Record<Theme, Theme> = { system: "light", light: "dark", dark: "system" };
  return (
    <Tooltip content={`Theme: ${theme}`}>
      <Button variant="ghost" icon aria-label="Toggle theme" onClick={() => setTheme(next[theme])}>
        {themeIcons[theme]}
      </Button>
    </Tooltip>
  );
}

function UserMenu() {
  const { data: me } = useSuspenseQuery(meQuery);
  const logout = useLogout();
  const navigate = useNavigate();
  return (
    <Menu
      trigger={
        <button type="button" className={styles.avatarButton} aria-label="Account">
          <Avatar src={me.avatarUrl || undefined} name={me.login} size={30} />
        </button>
      }
    >
      <div className={styles.account}>
        <Avatar src={me.avatarUrl || undefined} name={me.login} size={32} />
        <div className={styles.accountText}>
          <span className={styles.userName}>{me.name || me.login}</span>
          <span className={styles.userLogin}>@{me.login}</span>
        </div>
      </div>
      <MenuItem icon={<Settings size={14} />} onClick={() => void navigate({ to: "/settings" })}>
        Settings
      </MenuItem>
      <MenuItem
        icon={<LogOut size={14} />}
        onClick={() => logout.mutate(undefined, { onSuccess: () => navigate({ to: "/login" }) })}
      >
        Sign out
      </MenuItem>
    </Menu>
  );
}

function Palette({
  open,
  onOpenChange,
  projectId,
  serviceId,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  projectId?: string;
  serviceId?: string;
}) {
  const navigate = useNavigate();
  const toast = useToast();
  const [, setTheme] = useTheme();
  const projects = useQuery(projectsQuery);
  const service = useQuery({ ...serviceQuery(serviceId ?? ""), enabled: !!serviceId });
  const deploy = useDeploy(serviceId ?? "");

  const commands: Command[] = [];
  if (service.data && projectId && serviceId) {
    const s = service.data;
    const base = `/projects/${projectId}/services/${serviceId}`;
    commands.push({
      id: "deploy",
      group: s.name,
      label: `Deploy ${s.name}`,
      icon: <Rocket size={15} />,
      run: () =>
        deploy.mutate(undefined, {
          onSuccess: () => toast.add({ title: "Deployment queued", description: s.name }),
          onError: (err) =>
            toast.add({ title: "Deploy failed", description: errorMessage(err), type: "error" }),
        }),
    });
    for (const t of visibleServiceTabs(s)) {
      commands.push({
        id: `tab-${t.value}`,
        group: s.name,
        label: t.label,
        icon: t.icon,
        run: () => void navigate({ to: t.value === "deployments" ? base : `${base}/${t.value}` }),
      });
    }
  }
  if (projectId) {
    commands.push({
      id: "new-service",
      group: "Actions",
      label: "New service",
      icon: <Plus size={15} />,
      run: () =>
        void navigate({ to: "/projects/$projectId", params: { projectId }, search: { new: true } }),
    });
  }
  commands.push(
    {
      id: "settings",
      group: "Actions",
      label: "Settings",
      icon: <Settings size={15} />,
      run: () => void navigate({ to: "/settings" }),
    },
    {
      id: "new-project",
      group: "Actions",
      label: "New project",
      icon: <FolderPlus size={15} />,
      run: () => void navigate({ to: "/", search: { new: true } }),
    },
    {
      id: "theme",
      group: "Actions",
      label: "Toggle dark mode",
      icon: <Moon size={15} />,
      run: () => setTheme(document.documentElement.matches("[data-theme=dark]") ? "light" : "dark"),
    },
  );
  for (const p of projects.data ?? []) {
    commands.push({
      id: `project-${p.id}`,
      group: "Projects",
      label: p.name,
      icon: <LayoutGrid size={15} />,
      hint: `${p.services.length} services`,
      run: () => void navigate({ to: "/projects/$projectId", params: { projectId: p.id } }),
    });
    for (const s of p.services) {
      commands.push({
        id: `service-${s.id}`,
        group: "Services",
        label: `${p.name} / ${s.name}`,
        icon: <ServiceIcon kind={s.kind} size={18} />,
        hint: s.kind,
        run: () =>
          void navigate({
            to: "/projects/$projectId/services/$serviceId",
            params: { projectId: p.id, serviceId: s.id },
          }),
      });
    }
  }

  return <CommandPalette open={open} onOpenChange={onOpenChange} commands={commands} />;
}
