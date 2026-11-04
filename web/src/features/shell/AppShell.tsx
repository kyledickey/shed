import { buttonVariants, DropdownMenu, Tooltip } from "@cloudflare/kumo";
import {
  ArrowCircleUpIcon,
  BookOpenIcon,
  ChartLineIcon,
  DesktopIcon,
  HardDrivesIcon,
  MagnifyingGlassIcon,
  MoonIcon,
  SignOutIcon,
  SunIcon,
} from "@phosphor-icons/react";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useParams } from "@tanstack/react-router";
import { useState } from "react";
import { meQuery, useLogout } from "../../api/auth";
import { telemetryQuery } from "../../api/telemetry";
import { updateQuery } from "../../api/update";
import { Avatar } from "../../components/Misc";
import { useTheme, type Theme } from "../../lib/theme";
import styles from "./AppShell.module.css";
import { Logo } from "./Logo";
import { Palette } from "./Palette";
import { TabBar } from "./TabBar";
import { TabsProvider } from "./TabsContext";

/**
 * AppShell frames every signed-in page: a top bar with shed's tabs and
 * account actions over a page that scrolls like any web page, so browser
 * gestures such as swipe-to-go-back keep working.
 */
export function AppShell() {
  const { projectId, serviceId } = useParams({ strict: false });
  const [palette, setPalette] = useState(false);
  return (
    <TabsProvider>
      <header className={styles.top}>
        <Link to="/" className={styles.home} aria-label="Home">
          <Logo size={26} />
        </Link>
        <TabBar />
        <div className={styles.actions}>
          <UpdateBadge />
          <SearchButton onClick={() => setPalette(true)} />
          <GrafanaButton />
          <ServerButton />
          <UserMenu />
        </div>
      </header>
      <main className={styles.main}>
        <Outlet />
      </main>
      <Palette
        open={palette}
        onOpenChange={setPalette}
        projectId={projectId}
        serviceId={serviceId}
      />
    </TabsProvider>
  );
}

function SearchButton({ onClick }: { onClick: () => void }) {
  return (
    <button type="button" className={styles.search} onClick={onClick}>
      <MagnifyingGlassIcon size={15} />
      <span className={styles.searchLabel}>Search</span>
      <kbd className={styles.searchKey}>⌘K</kbd>
    </button>
  );
}

/** UpdateBadge links to the update settings while a newer release is available. */
function UpdateBadge() {
  // A failed status request just hides the badge; Settings reports the error.
  const { data } = useQuery({ ...updateQuery, retry: false });
  if (!data || data.unsupported || !(data.available || data.staged)) return null;
  return (
    <Link
      to="/server/$section"
      params={{ section: "updates" }}
      className={`${buttonVariants({ variant: "ghost", size: "sm" })} ${styles.update}`}
      aria-label={data.staged ? "Restart to update" : "Update available"}
      title={data.staged ? "Restart to update" : "Update available"}
    >
      <ArrowCircleUpIcon size={16} />
      <span className={styles.updateLabel}>{data.staged ? "Restart to update" : "Update"}</span>
    </Link>
  );
}

/** ServerButton opens the server settings. */
function ServerButton() {
  return (
    <Tooltip
      content="Server"
      render={
        <Link
          to="/server"
          className={buttonVariants({ variant: "ghost", shape: "square", size: "sm" })}
          aria-label="Server"
        >
          <HardDrivesIcon size={16} />
        </Link>
      }
    />
  );
}

/** GrafanaButton opens Grafana in a new tab while telemetry is on. */
function GrafanaButton() {
  const { data } = useQuery({ ...telemetryQuery, retry: false, refetchInterval: false });
  if (!data?.settings.enabled) return null;
  return (
    <Tooltip
      content="Open Grafana"
      render={
        <a
          className={styles.iconLink}
          href={data.grafanaUrl}
          target="_blank"
          rel="noreferrer"
          aria-label="Open Grafana"
        >
          <ChartLineIcon size={16} />
        </a>
      }
    />
  );
}

const themes = [
  { value: "system", label: "System", icon: DesktopIcon },
  { value: "light", label: "Light", icon: SunIcon },
  { value: "dark", label: "Dark", icon: MoonIcon },
] as const;

function UserMenu() {
  const { data: me } = useSuspenseQuery(meQuery);
  const logout = useLogout();
  const navigate = useNavigate();
  const [theme, setTheme] = useTheme();
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <button type="button" className={styles.avatarButton} aria-label="Account">
            <Avatar src={me.avatarUrl || undefined} name={me.login} size={26} />
          </button>
        }
      />
      <DropdownMenu.Content align="end">
        <div className={styles.account}>
          <span className={styles.userName}>{me.name || me.login}</span>
          <span className={styles.userLogin}>@{me.login}</span>
        </div>
        <DropdownMenu.Separator />
        <DropdownMenu.Group>
          <DropdownMenu.Label className={styles.menuLabel}>Theme</DropdownMenu.Label>
          <DropdownMenu.RadioGroup value={theme} onValueChange={(v: Theme) => setTheme(v)}>
            {themes.map((t) => (
              <DropdownMenu.RadioItem key={t.value} value={t.value} icon={t.icon}>
                {t.label}
                <DropdownMenu.RadioItemIndicator />
              </DropdownMenu.RadioItem>
            ))}
          </DropdownMenu.RadioGroup>
        </DropdownMenu.Group>
        <DropdownMenu.Separator />
        <DropdownMenu.Item icon={BookOpenIcon} onClick={() => void navigate({ to: "/docs" })}>
          Docs
        </DropdownMenu.Item>
        <DropdownMenu.Separator />
        <DropdownMenu.Item
          icon={SignOutIcon}
          onClick={() => logout.mutate(undefined, { onSuccess: () => navigate({ to: "/login" }) })}
        >
          Sign out
        </DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu>
  );
}
