import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useParams } from "@tanstack/react-router";
import { LogOut } from "lucide-react";
import { Fragment } from "react";
import { meQuery, useLogout } from "../../api/auth";
import { projectQuery } from "../../api/projects";
import { serviceQuery } from "../../api/services";
import { Menu, MenuItem, MenuLabel, MenuSeparator } from "../../components/Menu";
import { Wordmark } from "./Wordmark";
import styles from "./AppShell.module.css";

export function AppShell() {
  return (
    <div className={styles.shell}>
      <header className={styles.topbar}>
        <nav className={styles.nav} aria-label="Breadcrumb">
          <Link to="/" className={styles.home} aria-label="shed home">
            <Wordmark />
          </Link>
          <Breadcrumbs />
        </nav>
        <UserMenu />
      </header>
      <Outlet />
    </div>
  );
}

function Breadcrumbs() {
  const { projectId = "", serviceId = "" } = useParams({ strict: false });
  const project = useQuery({ ...projectQuery(projectId), enabled: !!projectId });
  const service = useQuery({ ...serviceQuery(serviceId), enabled: !!serviceId });

  const crumbs = [
    <Link key="projects" to="/" activeOptions={{ exact: true }} className={styles.crumb}>
      Projects
    </Link>,
  ];
  if (projectId) {
    crumbs.push(
      <Link
        key="project"
        to="/projects/$projectId"
        params={{ projectId }}
        activeOptions={{ exact: true }}
        className={styles.crumb}
      >
        {project.data?.name ?? "…"}
      </Link>,
    );
  }
  if (projectId && serviceId) {
    crumbs.push(
      <Link
        key="service"
        to="/projects/$projectId/services/$serviceId"
        params={{ projectId, serviceId }}
        className={styles.crumb}
      >
        {service.data?.name ?? "…"}
      </Link>,
    );
  }

  return (
    <ol className={styles.crumbs}>
      {crumbs.map((crumb, i) => (
        <Fragment key={crumb.key}>
          <li className={styles.separator} aria-hidden>
            /
          </li>
          <li aria-current={i === crumbs.length - 1 ? "page" : undefined}>{crumb}</li>
        </Fragment>
      ))}
    </ol>
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
          {me.avatarUrl ? (
            <img className={styles.avatar} src={me.avatarUrl} alt="" />
          ) : (
            <span className={styles.avatar}>{me.login.charAt(0).toUpperCase()}</span>
          )}
        </button>
      }
    >
      <MenuLabel>
        <div className={styles.userName}>{me.name || me.login}</div>
        <div>@{me.login}</div>
      </MenuLabel>
      <MenuSeparator />
      <MenuItem
        icon={<LogOut size={14} />}
        onClick={() => logout.mutate(undefined, { onSuccess: () => navigate({ to: "/login" }) })}
      >
        Sign out
      </MenuItem>
    </Menu>
  );
}
