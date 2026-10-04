import {
  Box,
  ChartArea,
  Command as CommandIcon,
  Database,
  Layers,
  Monitor,
  Moon,
  MousePointerClick,
  Palette,
  Rocket,
  ScrollText,
  SlidersHorizontal,
  Sparkles,
  Sun,
  X,
} from "lucide-react";
import { useState, type ReactNode } from "react";
import { Button } from "../components/Button";
import { CommandPalette, type Command } from "../components/CommandPalette";
import { Segmented } from "../components/Form";
import { Avatar, GitHubIcon, Kbd } from "../components/Misc";
import { Tooltip, useToast } from "../components/Overlay";
import { Crumbs, Logo, PageHeader, Shell } from "../components/Shell";
import { Tabs } from "../components/Tabs";
import { useTheme, type Theme } from "../lib/theme";
import { Controls } from "./sections/Controls";
import { Deployments } from "./sections/Deployments";
import { Foundations } from "./sections/Foundations";
import { Logs } from "./sections/Logs";
import { Metrics } from "./sections/Metrics";
import { Overlays } from "./sections/Overlays";
import { Surfaces } from "./sections/Surfaces";
import s from "./showcase.module.css";
import { useTweaks, type Accent } from "./tweaks";

export const showcaseTabs = [
  "foundations",
  "controls",
  "surfaces",
  "deployments",
  "logs",
  "metrics",
  "overlays",
] as const;
export type ShowcaseTab = (typeof showcaseTabs)[number];

const tabMeta: Record<
  ShowcaseTab,
  { label: string; icon: ReactNode; title: string; blurb: string }
> = {
  foundations: {
    label: "Foundations",
    icon: <Palette size={15} />,
    title: "Foundations",
    blurb: "Color, type, radius and elevation tokens. Everything else is built from these.",
  },
  controls: {
    label: "Controls",
    icon: <MousePointerClick size={15} />,
    title: "Controls",
    blurb: "Buttons, tabs, inputs, switches and badges.",
  },
  surfaces: {
    label: "Surfaces",
    icon: <Layers size={15} />,
    title: "Surfaces",
    blurb: "Cards, lists, callouts and the in-between states.",
  },
  deployments: {
    label: "Deployments",
    icon: <Rocket size={15} />,
    title: "Deployments",
    blurb: "Progress, history and build output.",
  },
  logs: {
    label: "Logs",
    icon: <ScrollText size={15} />,
    title: "Logs",
    blurb: "A live, parsed log stream with search, filters and a fullscreen watch mode.",
  },
  metrics: {
    label: "Metrics",
    icon: <ChartArea size={15} />,
    title: "Metrics",
    blurb: "Stat tiles, area charts, volumes and uptime.",
  },
  overlays: {
    label: "Overlays",
    icon: <Sparkles size={15} />,
    title: "Overlays",
    blurb: "Dialogs, menus, tooltips, toasts and the command palette.",
  },
};

const accents: Accent[] = ["grass", "sky", "grape", "teal", "tangerine", "bubblegum"];

/** Showcase renders every component idea inside the real shell for review. */
export function Showcase({ tab, onTab }: { tab: ShowcaseTab; onTab: (tab: ShowcaseTab) => void }) {
  const [theme, setTheme] = useTheme();
  const [palette, setPalette] = useState(false);
  const toast = useToast();
  const meta = tabMeta[tab];

  const commands: Command[] = [
    ...showcaseTabs.map((t) => ({
      id: `go-${t}`,
      group: "Go to",
      label: tabMeta[t].label,
      icon: tabMeta[t].icon,
      run: () => onTab(t),
    })),
    {
      id: "svc-api",
      group: "Services",
      label: "acme / api",
      icon: <GitHubIcon size={15} />,
      hint: "app",
      run: () => onTab("logs"),
    },
    {
      id: "svc-pg",
      group: "Services",
      label: "acme / postgres",
      icon: <Database size={15} />,
      hint: "database",
      run: () => onTab("metrics"),
    },
    {
      id: "svc-worker",
      group: "Services",
      label: "acme / worker",
      icon: <Box size={15} />,
      hint: "image",
      run: () => onTab("deployments"),
    },
    {
      id: "deploy",
      group: "Actions",
      label: "Deploy api",
      icon: <Rocket size={15} />,
      run: () =>
        toast.add({ title: "Deployment queued", description: "acme/api@a91c03b", type: "info" }),
    },
    {
      id: "theme",
      group: "Actions",
      label: "Toggle dark mode",
      icon: <Moon size={15} />,
      run: () => setTheme(document.documentElement.matches("[data-theme=dark]") ? "light" : "dark"),
    },
  ];

  return (
    <Shell
      brand={
        <>
          <Logo />
          <Crumbs items={[{ label: "acme" }, { label: "api", icon: <GitHubIcon size={13} /> }]} />
        </>
      }
      nav={
        <Tabs
          label="Showcase"
          value={tab}
          onChange={onTab}
          items={showcaseTabs.map((t) => ({
            value: t,
            label: tabMeta[t].label,
            icon: tabMeta[t].icon,
            count: t === "deployments" ? 5 : undefined,
          }))}
        />
      }
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
          <ThemeButton theme={theme} onChange={setTheme} />
          <Avatar name="kyle" size={30} />
        </>
      }
    >
      <div className={s.content}>
        <PageHeader eyebrow="Design kit" title={meta.title} description={meta.blurb} />
        {tab === "foundations" && <Foundations />}
        {tab === "controls" && <Controls />}
        {tab === "surfaces" && <Surfaces />}
        {tab === "deployments" && <Deployments />}
        {tab === "logs" && <Logs />}
        {tab === "metrics" && <Metrics />}
        {tab === "overlays" && <Overlays onCommand={() => setPalette(true)} />}
      </div>
      <TweaksPanel theme={theme} onTheme={setTheme} />
      <CommandPalette open={palette} onOpenChange={setPalette} commands={commands} />
    </Shell>
  );
}

function ThemeButton({ theme, onChange }: { theme: Theme; onChange: (t: Theme) => void }) {
  const next: Record<Theme, Theme> = { system: "light", light: "dark", dark: "system" };
  const icon = {
    system: <Monitor size={16} />,
    light: <Sun size={16} />,
    dark: <Moon size={16} />,
  }[theme];
  return (
    <Tooltip content={`Theme: ${theme}`}>
      <Button variant="ghost" icon aria-label="Toggle theme" onClick={() => onChange(next[theme])}>
        {icon}
      </Button>
    </Tooltip>
  );
}

function TweaksPanel({ theme, onTheme }: { theme: Theme; onTheme: (t: Theme) => void }) {
  const [open, setOpen] = useState(false);
  const [tweaks, set] = useTweaks();
  return (
    <div className={s.tweaks}>
      {open && (
        <div className={s.tweaksPanel}>
          <div className={s.tweakRow}>
            <span>Theme</span>
            <Segmented
              label="Theme"
              size="sm"
              value={theme}
              onChange={onTheme}
              options={[
                { value: "light", label: <Sun size={13} />, title: "Light" },
                { value: "dark", label: <Moon size={13} />, title: "Dark" },
                { value: "system", label: <Monitor size={13} />, title: "System" },
              ]}
            />
          </div>
          <div className={s.tweakRow}>
            <span>Accent</span>
            <div className={s.accents}>
              {accents.map((a) => (
                <button
                  key={a}
                  type="button"
                  className={s.accentDot}
                  data-tone={a}
                  aria-label={a}
                  aria-pressed={tweaks.accent === a}
                  onClick={() => set({ accent: a })}
                />
              ))}
            </div>
          </div>
          <div className={s.tweakRow}>
            <span>Softness · {tweaks.soft.toFixed(2)}×</span>
            <input
              className={s.range}
              type="range"
              min={0.3}
              max={1.8}
              step={0.05}
              value={tweaks.soft}
              onChange={(e) => set({ soft: Number(e.target.value) })}
            />
          </div>
        </div>
      )}
      <div className={s.tweaksToggle}>
        <Button variant={open ? "secondary" : "primary"} onClick={() => setOpen(!open)}>
          {open ? <X size={14} /> : <SlidersHorizontal size={14} />}
          Tweaks
        </Button>
      </div>
    </div>
  );
}
