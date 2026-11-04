import { CommandPalette } from "@cloudflare/kumo";
import {
  BookOpenIcon,
  FolderSimplePlusIcon,
  GearIcon,
  HardDrivesIcon,
  MoonIcon,
  PlusIcon,
  RocketLaunchIcon,
  SquaresFourIcon,
  StackIcon,
} from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useState, type ReactNode } from "react";
import { errorMessage } from "../../api/client";
import { useDeploy } from "../../api/deployments";
import { projectsQuery } from "../../api/projects";
import { serviceQuery } from "../../api/services";
import { ServiceIcon } from "../../components/Misc";
import { useToast } from "../../components/Overlay";
import { useTheme } from "../../lib/theme";
import { docPages } from "../docs/registry";
import { serverSections } from "../server/sections";
import { serviceTabHref, visibleServiceTabs } from "../services/sections";
import styles from "./Palette.module.css";

type Command = {
  id: string;
  label: string;
  /** Places above the command, such as the project of a service. */
  crumbs?: string[];
  icon: ReactNode;
  hint?: string;
  run: () => void;
};

type Group = { id: string; label: string; items: Command[] };

const glyph = { size: 16 } as const;

/** Palette is the ⌘K launcher: places, actions, and docs. ⌘K toggles it anywhere. */
export function Palette({
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
  const [query, setQuery] = useState("");
  const projects = useQuery(projectsQuery);
  const service = useQuery({ ...serviceQuery(serviceId ?? ""), enabled: !!serviceId });
  const deploy = useDeploy(serviceId ?? "");

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        onOpenChange(!open);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onOpenChange]);

  const groups: Group[] = [];
  const add = (id: string, label: string, items: Command[]) => {
    if (items.length > 0) groups.push({ id, label, items });
  };

  if (service.data && projectId && serviceId) {
    const s = service.data;
    add(`service-${s.id}`, s.name, [
      {
        id: "deploy",
        label: `Deploy ${s.name}`,
        icon: <RocketLaunchIcon {...glyph} />,
        run: () =>
          deploy.mutate(undefined, {
            onSuccess: () => toast.add({ title: "Deployment queued", description: s.name }),
            onError: (err) =>
              toast.add({ title: "Deploy failed", description: errorMessage(err), type: "error" }),
          }),
      },
      ...visibleServiceTabs(s).map((t) => ({
        id: `tab-${t.value}`,
        label: t.label,
        icon: <SquaresFourIcon {...glyph} />,
        run: () => void navigate({ to: serviceTabHref(s, t.value) }),
      })),
    ]);
  }

  add("actions", "Actions", [
    ...(projectId
      ? [
          {
            id: "new-service",
            label: "New service",
            icon: <PlusIcon {...glyph} />,
            run: () =>
              void navigate({
                to: "/projects/$projectId",
                params: { projectId },
                search: { new: true },
              }),
          },
        ]
      : []),
    {
      id: "new-project",
      label: "New project",
      icon: <FolderSimplePlusIcon {...glyph} />,
      run: () => void navigate({ to: "/", search: { new: true } }),
    },
    {
      id: "docs",
      label: "Docs",
      icon: <BookOpenIcon {...glyph} />,
      run: () => void navigate({ to: "/docs" }),
    },
    {
      id: "theme",
      label: "Toggle dark mode",
      icon: <MoonIcon {...glyph} />,
      run: () => setTheme(document.documentElement.matches("[data-mode=dark]") ? "light" : "dark"),
    },
  ]);

  const list = projects.data ?? [];
  add(
    "projects",
    "Projects",
    list.map((p) => ({
      id: `project-${p.id}`,
      label: p.name,
      icon: <StackIcon {...glyph} />,
      hint: `${p.services.length} ${p.services.length === 1 ? "service" : "services"}`,
      run: () => void navigate({ to: "/projects/$projectId", params: { projectId: p.id } }),
    })),
  );
  add(
    "services",
    "Services",
    list.flatMap((p) =>
      p.services.map((s) => ({
        id: `service-${s.id}`,
        label: s.name,
        crumbs: [p.name],
        icon: <ServiceIcon kind={s.kind} size={28} />,
        run: () =>
          void navigate({
            to: "/projects/$projectId/services/$serviceId",
            params: { projectId: p.id, serviceId: s.id },
          }),
      })),
    ),
  );
  add(
    "server",
    "Server",
    serverSections.map((section) => ({
      id: `server-${section.id}`,
      label: section.title,
      icon: section.group === "monitor" ? <HardDrivesIcon {...glyph} /> : <GearIcon {...glyph} />,
      hint: section.description,
      run: () => void navigate({ to: "/server/$section", params: { section: section.id } }),
    })),
  );
  add(
    "docs",
    "Docs",
    docPages.map((page) => ({
      id: `docs-${page.slug}`,
      label: page.title,
      icon: <BookOpenIcon {...glyph} />,
      run: () => void navigate({ to: "/docs/$slug", params: { slug: page.slug } }),
    })),
  );

  const q = query.trim().toLowerCase();
  const matches = q
    ? groups
        .map((g) => ({
          ...g,
          items: g.items.filter((c) =>
            [...(c.crumbs ?? []), c.label].join(" / ").toLowerCase().includes(q),
          ),
        }))
        .filter((g) => g.items.length > 0)
    : groups;

  const change = (next: boolean) => {
    if (!next) setQuery("");
    onOpenChange(next);
  };

  const run = (c: Command) => {
    change(false);
    c.run();
  };

  return (
    <CommandPalette.Root
      open={open}
      onOpenChange={change}
      items={matches}
      value={query}
      onValueChange={setQuery}
      itemToStringValue={(g: Group) => g.label}
      getSelectableItems={(gs: Group[]) => gs.flatMap((g) => g.items)}
    >
      <CommandPalette.Input
        className={styles.input}
        placeholder="Search projects, services, actions…"
      />
      <CommandPalette.List>
        <CommandPalette.Results>
          {(group: Group) => (
            <CommandPalette.Group key={group.id} items={group.items}>
              <CommandPalette.GroupLabel>{group.label}</CommandPalette.GroupLabel>
              <CommandPalette.Items>
                {(c: Command) => (
                  <CommandPalette.ResultItem
                    key={c.id}
                    value={c}
                    title={c.label}
                    breadcrumbs={c.crumbs}
                    description={c.hint}
                    icon={c.icon}
                    onClick={() => run(c)}
                  />
                )}
              </CommandPalette.Items>
            </CommandPalette.Group>
          )}
        </CommandPalette.Results>
        <CommandPalette.Empty>Nothing matches “{query.trim()}”.</CommandPalette.Empty>
      </CommandPalette.List>
      <CommandPalette.Footer>
        <span className={styles.keys}>
          <kbd className={styles.kbd}>↑</kbd>
          <kbd className={styles.kbd}>↓</kbd>
          Navigate
        </span>
        <span className={styles.keys}>
          <kbd className={styles.kbd}>↵</kbd>
          Open
        </span>
      </CommandPalette.Footer>
    </CommandPalette.Root>
  );
}
