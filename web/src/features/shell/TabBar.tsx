import { ContextMenu } from "@cloudflare/kumo/primitives/context-menu";
import { Button, DropdownMenu, Tooltip } from "@cloudflare/kumo";
import {
  BookOpenIcon,
  FolderIcon,
  HardDrivesIcon,
  HouseIcon,
  PlusIcon,
  PushPinIcon,
  PushPinSlashIcon,
  XIcon,
} from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useState, type DragEvent, type ReactNode } from "react";
import { projectQuery } from "../../api/projects";
import { serviceQuery } from "../../api/services";
import { ServiceIcon } from "../../components/Misc";
import { cx } from "../../lib/cx";
import { docPages } from "../docs/registry";
import { findSection } from "../server/sections";
import { parsePlace, sectionLabel, type Place } from "./places";
import { homeHref, type Tab } from "./tabs";
import { useTabs } from "./TabsContext";
import styles from "./TabBar.module.css";

/** TabBar is the strip of shed tabs in the top bar. */
export function TabBar() {
  const { state, dispatch, open } = useTabs();
  const [dragging, setDragging] = useState<string | null>(null);

  const onDragOver = (e: DragEvent, index: number) => {
    const tab = state.tabs[index];
    if (!dragging || !tab) return;
    e.preventDefault();
    if (tab.id !== dragging) dispatch({ type: "move", id: dragging, to: index });
  };

  return (
    <div className={styles.bar}>
      <div className={styles.strip} role="tablist" aria-label="Open tabs">
        {state.tabs.map((tab, i) => (
          <TabItem
            key={tab.id}
            tab={tab}
            active={tab.id === state.active}
            dragging={tab.id === dragging}
            onDragStart={() => setDragging(tab.id)}
            onDragEnd={() => setDragging(null)}
            onDragOver={(e) => onDragOver(e, i)}
          />
        ))}
      </div>
      <Tooltip
        content="New tab"
        render={
          <Button
            variant="ghost"
            size="sm"
            shape="square"
            icon={<PlusIcon size={14} />}
            aria-label="New tab"
            onClick={() => open(homeHref)}
          />
        }
      />
    </div>
  );
}

function TabItem({
  tab,
  active,
  dragging,
  onDragStart,
  onDragEnd,
  onDragOver,
}: {
  tab: Tab;
  active: boolean;
  dragging: boolean;
  onDragStart: () => void;
  onDragEnd: () => void;
  onDragOver: (e: DragEvent) => void;
}) {
  const { state, dispatch, close } = useTabs();
  const { icon, title, detail } = usePlaceLabel(tab.href);
  const label = detail ? `${title} · ${detail}` : title;
  return (
    <ContextMenu.Root>
      <ContextMenu.Trigger
        render={<div />}
        className={cx(styles.tab, tab.pinned && styles.pinned, dragging && styles.dragging)}
        data-active={active || undefined}
        draggable
        onDragStart={(e) => {
          e.dataTransfer.effectAllowed = "move";
          onDragStart();
        }}
        onDragEnd={onDragEnd}
        onDragOver={onDragOver}
        onAuxClick={(e) => {
          if (e.button === 1 && !tab.pinned) close(tab.id);
        }}
      >
        <button
          type="button"
          role="tab"
          aria-selected={active}
          className={styles.tabButton}
          title={tab.pinned ? label : undefined}
          onClick={() => dispatch({ type: "activate", id: tab.id })}
        >
          <span className={styles.tabIcon}>{icon}</span>
          {!tab.pinned && (
            <span className={styles.tabLabel}>
              {title}
              {detail && <span className={styles.tabDetail}>{detail}</span>}
            </span>
          )}
        </button>
        {!tab.pinned && (
          <button
            type="button"
            className={styles.close}
            aria-label={`Close ${label}`}
            onClick={() => close(tab.id)}
          >
            <XIcon size={12} weight="bold" />
          </button>
        )}
      </ContextMenu.Trigger>
      <DropdownMenu.Content>
        <DropdownMenu.Item
          icon={tab.pinned ? PushPinSlashIcon : PushPinIcon}
          onClick={() => dispatch({ type: "pin", id: tab.id, pinned: !tab.pinned })}
        >
          {tab.pinned ? "Unpin tab" : "Pin tab"}
        </DropdownMenu.Item>
        <DropdownMenu.Separator />
        <DropdownMenu.Item icon={XIcon} onClick={() => close(tab.id)}>
          Close tab
        </DropdownMenu.Item>
        <DropdownMenu.Item
          inset
          disabled={state.tabs.length < 2}
          onClick={() => dispatch({ type: "closeOthers", id: tab.id })}
        >
          Close other tabs
        </DropdownMenu.Item>
      </DropdownMenu.Content>
    </ContextMenu.Root>
  );
}

type PlaceLabel = { icon: ReactNode; title: string; detail?: string };

const iconSize = 16;

/** usePlaceLabel names a place for its tab, looking up project and service names. */
export function usePlaceLabel(href: string): PlaceLabel {
  const place: Place = parsePlace(href);
  const projectId = place.kind === "project" || place.kind === "service" ? place.projectId : "";
  const serviceId = place.kind === "service" ? place.serviceId : "";
  const project = useQuery({ ...projectQuery(projectId), enabled: !!projectId && !serviceId });
  const service = useQuery({ ...serviceQuery(serviceId), enabled: !!serviceId });
  const detail = "section" in place && place.section ? sectionLabel(place.section) : undefined;

  switch (place.kind) {
    case "home":
      return { icon: <HouseIcon size={iconSize} />, title: "Home" };
    case "project":
      return {
        icon: <FolderIcon size={iconSize} />,
        title: project.data?.name ?? "Project",
        detail,
      };
    case "service":
      return {
        icon: service.data ? (
          <ServiceIcon kind={service.data.kind} size={iconSize} />
        ) : (
          <FolderIcon size={iconSize} />
        ),
        title: service.data?.name ?? "Service",
        detail,
      };
    case "server":
      return {
        icon: <HardDrivesIcon size={iconSize} />,
        title: "Server",
        detail: findSection(place.section)?.title ?? detail,
      };
    case "docs":
      return {
        icon: <BookOpenIcon size={iconSize} />,
        title: "Docs",
        detail: docPages.find((p) => p.slug === place.slug)?.title,
      };
    case "other":
      return { icon: <HouseIcon size={iconSize} />, title: place.path };
  }
}
