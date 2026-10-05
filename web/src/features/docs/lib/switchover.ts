/** SwitchMode is how the pipeline replaces a running container. */
export type SwitchMode = "overlap" | "stop-first";

/** ContainerView is one container's state in a switchover frame. */
export type ContainerView = {
  state: "running" | "stopped" | "absent";
  /** It answers to the service name on the project network. */
  alias: boolean;
};

/** Frame is one moment of a switchover. */
export type Frame = {
  title: string;
  detail: string;
  old: ContainerView;
  next: ContainerView;
  /** Where Caddy sends public traffic: the old container, the new one, or nowhere. */
  routes: "old" | "new" | "none";
};

const live: ContainerView = { state: "running", alias: true };

/** switchoverFrames returns the ordered moments of a deployment going live. */
export function switchoverFrames(mode: SwitchMode): Frame[] {
  const stopFirst = mode === "stop-first";
  const oldAfterStop: ContainerView = stopFirst ? { state: "stopped", alias: true } : live;
  const routesDuring: Frame["routes"] = stopFirst ? "none" : "old";
  const frames: Frame[] = [
    {
      title: "Image is ready",
      detail:
        "The build or pull finished. The old container is serving all traffic, publicly through Caddy and privately through its network alias.",
      old: live,
      next: { state: "absent", alias: false },
      routes: "old",
    },
  ];
  if (stopFirst) {
    frames.push({
      title: "Stop the old container",
      detail:
        "A service with volumes or a public port can't run two containers: a database data directory or a published host port has one owner. The old container gets a graceful stop (up to 30 seconds). From here until the routes switch, requests fail.",
      old: oldAfterStop,
      next: { state: "absent", alias: false },
      routes: "none",
    });
  }
  frames.push(
    {
      title: "Start the candidate",
      detail:
        "The new container starts on the project network with no alias, so only its own container name resolves. Nothing sends it traffic yet.",
      old: oldAfterStop,
      next: { state: "running", alias: false },
      routes: routesDuring,
    },
    {
      title: "Health check",
      detail:
        "shed probes the candidate's container IP for up to 120 seconds: a TCP connect to the port, or a 2xx/3xx from the health check path. A service with no port is only watched for 3 seconds. An exit at any point fails the deployment.",
      old: oldAfterStop,
      next: { state: "running", alias: false },
      routes: routesDuring,
    },
    {
      title: "Take the alias",
      detail:
        "Docker can't edit a connected container's aliases, so shed disconnects the candidate and reconnects it with the service name, asking for the IP it already had. For a moment both containers answer to the name.",
      old: oldAfterStop,
      next: { state: "running", alias: true },
      routes: routesDuring,
    },
    {
      title: "Probe again",
      detail:
        "Reconnecting could have disturbed the container, so shed probes it a second time for up to 10 seconds. If that fails, the deployment fails, the candidate is removed, and the old container keeps its alias and routes.",
      old: oldAfterStop,
      next: { state: "running", alias: true },
      routes: routesDuring,
    },
    {
      title: "Apply routes",
      detail:
        "Caddy is loaded with a config pointing every domain at the candidate's IP and port. Only after that succeeds does one transaction mark the new deployment active and the old one removed.",
      old: oldAfterStop,
      next: { state: "running", alias: true },
      routes: "new",
    },
    {
      title: "Retire the old container",
      detail:
        "The old container is disconnected from the network first, so the name resolves only to the new one, then stopped and removed. Images beyond the newest 5 are pruned.",
      old: { state: "absent", alias: false },
      next: { state: "running", alias: true },
      routes: "new",
    },
  );
  return frames;
}
