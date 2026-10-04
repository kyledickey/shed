package deploy

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/kyledickey/shed/internal/catalog"
	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/proxy"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/vars"
)

// keepImages is how many built images are kept per service.
const keepImages = 5

// environment returns the resolved variables of svc: its own variables over
// the ones shed injects, with references to other services of the project
// expanded. sha is the commit being deployed.
func (d *Deployer) environment(ctx context.Context, svc store.Service, project store.Project, sha string) (map[string]string, error) {
	services, err := d.store.Services(ctx, svc.ProjectID)
	if err != nil {
		return nil, err
	}
	all := make(map[string]map[string]string, len(services))
	for _, s := range services {
		own, err := d.store.Variables(ctx, s.ID)
		if err != nil {
			return nil, err
		}
		domains, err := d.store.Domains(ctx, s.ID)
		if err != nil {
			return nil, err
		}
		commit := ""
		if s.ID == svc.ID {
			commit = sha
		}
		merged := injected(project, s, domains, commit)
		maps.Copy(merged, own)
		all[s.Name] = merged
	}
	env, err := vars.Resolve(svc.Name, all)
	if err != nil {
		return nil, fmt.Errorf("deploy: resolve variables: %w", err)
	}
	return env, nil
}

// injected returns the variables shed provides to every service.
func injected(project store.Project, svc store.Service, domains []store.Domain, sha string) map[string]string {
	m := map[string]string{
		"SHED_PROJECT_NAME":   project.Name,
		"SHED_SERVICE_NAME":   svc.Name,
		"SHED_PRIVATE_DOMAIN": svc.Name,
	}
	if svc.Kind == "app" && svc.Port > 0 {
		m["PORT"] = strconv.Itoa(svc.Port)
	}
	if len(domains) > 0 {
		m["SHED_PUBLIC_DOMAIN"] = domains[0].Host
	}
	if sha != "" {
		m["SHED_GIT_COMMIT_SHA"] = sha
	}
	if svc.Branch != "" {
		m["SHED_GIT_BRANCH"] = svc.Branch
	}
	return m
}

// runContainer creates and starts the container described by spec, creating
// its network and volumes as needed.
func (d *Deployer) runContainer(ctx context.Context, spec docker.RunSpec) (string, error) {
	if err := d.docker.EnsureNetwork(ctx, spec.Network); err != nil {
		return "", err
	}
	for _, m := range spec.Mounts {
		if err := d.docker.EnsureVolume(ctx, m.Volume); err != nil {
			return "", err
		}
	}
	return d.docker.Run(ctx, spec)
}

// containerSpec describes the container of a deployment on its project
// network.
func containerSpec(svc store.Service, dep store.Deployment, env map[string]string, vols []store.Volume) docker.RunSpec {
	spec := docker.RunSpec{
		Name:  containerName(svc.ID, dep.ID),
		Image: dep.Image,
		Cmd:   command(svc),
		Labels: map[string]string{
			labelProject:    svc.ProjectID,
			labelService:    svc.ID,
			labelDeployment: dep.ID,
		},
		Network: networkName(svc.ProjectID),
		Aliases: []string{svc.Name},
		CPUs:    svc.CPULimit,
		Memory:  svc.MemoryLimit,
	}
	for _, k := range slices.Sorted(maps.Keys(env)) {
		spec.Env = append(spec.Env, k+"="+env[k])
	}
	for _, v := range vols {
		spec.Mounts = append(spec.Mounts, docker.Mount{Volume: volumeName(v.ID), Target: v.MountPath})
	}
	if svc.PublicPort > 0 && svc.Port > 0 {
		spec.Publish = []docker.PortBinding{{HostPort: svc.PublicPort, ContainerPort: svc.Port}}
	}
	return spec
}

// command returns the container command: the start command run by a shell,
// the database template's command, or nil for the image default.
func command(svc store.Service) []string {
	if svc.StartCommand != "" {
		return []string{"sh", "-c", svc.StartCommand}
	}
	if t, ok := catalog.Lookup(svc.Kind); ok {
		return t.Cmd
	}
	return nil
}

var healthClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// probe checks addr once: a TCP connect if path is empty, otherwise an HTTP
// GET of path that must answer 2xx or 3xx.
func probe(ctx context.Context, addr, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if path == "" {
		var dialer net.Dialer
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return err
		}
		return conn.Close()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+path, nil)
	if err != nil {
		return err
	}
	resp, err := healthClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("GET %s returned HTTP %d", path, resp.StatusCode)
	}
	return nil
}

func upstreamAddr(ip string, port int) string {
	return net.JoinHostPort(ip, strconv.Itoa(port))
}

// ApplyRoutes points the proxy at the active container of every running
// service with a domain, plus the dashboard. Call it after anything that changes domains
// or active deployments.
func (d *Deployer) ApplyRoutes(ctx context.Context) error {
	if d.proxy == nil {
		return nil
	}
	d.routesMu.Lock()
	defer d.routesMu.Unlock()
	routes, err := d.routes(ctx)
	if err != nil {
		return err
	}
	return d.proxy.Apply(routes)
}

func (d *Deployer) routes(ctx context.Context) ([]proxy.Route, error) {
	return d.routesFor(ctx, nil)
}

// routesFor optionally previews a candidate deployment before activation.
func (d *Deployer) routesFor(ctx context.Context, candidate *store.Deployment) ([]proxy.Route, error) {
	var routes []proxy.Route
	if d.dashboard.Host != "" {
		routes = append(routes, d.dashboard)
	}
	domains, err := d.store.AllDomains(ctx)
	if err != nil {
		return nil, fmt.Errorf("deploy: routes: %w", err)
	}
	upstreams := make(map[string]string) // by service ID; "" means none
	for _, dom := range domains {
		up, ok := upstreams[dom.ServiceID]
		if !ok {
			if candidate != nil && candidate.ServiceID == dom.ServiceID {
				svc, err := d.store.Service(ctx, dom.ServiceID)
				if err != nil {
					return nil, err
				}
				c, err := d.docker.Inspect(ctx, candidate.ContainerID)
				if err != nil {
					return nil, err
				}
				if svc.Port > 0 {
					ip := c.IPs[networkName(svc.ProjectID)]
					if ip == "" {
						return nil, errors.New("candidate container has no address")
					}
					up = upstreamAddr(ip, svc.Port)
				}
			} else {
				up = d.upstream(ctx, dom.ServiceID)
			}
			upstreams[dom.ServiceID] = up
		}
		if up != "" {
			routes = append(routes, proxy.Route{Host: dom.Host, Upstream: up})
		}
	}
	return routes, nil
}

// upstream returns the address of a service's active container, or "" if it
// has none or the service is stopped.
func (d *Deployer) upstream(ctx context.Context, serviceID string) string {
	svc, err := d.store.Service(ctx, serviceID)
	if err != nil || svc.Port <= 0 || svc.Stopped {
		return ""
	}
	dep, err := d.store.ActiveDeployment(ctx, serviceID)
	if err != nil || dep.ContainerID == "" {
		return ""
	}
	c, err := d.docker.Inspect(ctx, dep.ContainerID)
	if err != nil {
		d.log.Warn("inspect active container", "service", svc.Name, "err", err)
		return ""
	}
	ip := c.IPs[networkName(svc.ProjectID)]
	if ip == "" {
		return ""
	}
	return upstreamAddr(ip, svc.Port)
}

// removeOthers stops and removes every container of a service except keep,
// then removes the volumes they mounted that the service no longer has.
// Failures are logged.
func (d *Deployer) removeOthers(ctx context.Context, serviceID, keep string) {
	containers, err := d.docker.List(ctx, map[string]string{labelService: serviceID})
	if err != nil {
		d.log.Error("list service containers", "service", serviceID, "err", err)
		return
	}
	var detached []string
	for _, c := range containers {
		if c.ID == keep {
			continue
		}
		if c.Running {
			if err := d.docker.Stop(ctx, c.ID, d.stopTimeout); err != nil {
				d.log.Warn("stop old container", "container", c.Name, "err", err)
			}
		}
		if err := d.docker.Remove(ctx, c.ID); err != nil {
			d.log.Error("remove old container", "container", c.Name, "err", err)
		}
		detached = append(detached, c.Volumes...)
	}
	if len(detached) == 0 {
		return
	}
	vols, err := d.store.Volumes(ctx, serviceID)
	if err != nil {
		d.log.Error("list volumes", "service", serviceID, "err", err)
		return
	}
	for _, name := range detached {
		if !slices.ContainsFunc(vols, func(v store.Volume) bool { return volumeName(v.ID) == name }) {
			if err := d.docker.RemoveVolume(ctx, name); err != nil {
				d.log.Error("remove deleted volume", "volume", name, "err", err)
			}
		}
	}
}

// pruneImages removes the built images of a service beyond the newest few,
// always keeping active.
func (d *Deployer) pruneImages(ctx context.Context, serviceID, active string) {
	images, err := d.docker.ListImages(ctx, imageRepo(serviceID))
	if err != nil {
		d.log.Error("list images", "service", serviceID, "err", err)
		return
	}
	slices.SortFunc(images, func(a, b docker.Image) int {
		return cmp.Or(b.Created.Compare(a.Created), cmp.Compare(b.Ref, a.Ref))
	})
	for i, img := range images {
		if i < keepImages || img.Ref == active {
			continue
		}
		if err := d.docker.RemoveImage(ctx, img.Ref); err != nil {
			d.log.Warn("remove old image", "image", img.Ref, "err", err)
		}
	}
}

// Reconcile brings Docker in line with the store after a restart: deployments
// that were in progress are marked failed, the active deployment's container
// of every service that is not stopped is started (or recreated if it is
// gone), and routes are applied. It does not respect holds, so it must finish
// before anything calls Hold.
func (d *Deployer) Reconcile(ctx context.Context) error {
	stale, err := d.store.DeploymentsByStatus(ctx,
		store.StatusQueued, store.StatusWaiting, store.StatusBuilding, store.StatusDeploying)
	if err != nil {
		return fmt.Errorf("deploy: reconcile: %w", err)
	}
	for _, dep := range stale {
		d.removeDeploymentContainers(ctx, dep.ID)
		d.abandon(dep, store.StatusFailed, "interrupted by restart")
	}

	active, err := d.store.DeploymentsByStatus(ctx, store.StatusActive)
	if err != nil {
		return fmt.Errorf("deploy: reconcile: %w", err)
	}
	// Only one deployment per service is restored: the newest, as
	// ActiveDeployment picks it. Containers of any other deployment are
	// removed, such as a predecessor left running by a crash mid-switchover.
	winners := make(map[string]string) // deployment ID by service ID
	for _, dep := range active {       // oldest first
		winners[dep.ServiceID] = dep.ID
	}
	for _, dep := range active {
		if winners[dep.ServiceID] != dep.ID {
			continue
		}
		svc, err := d.store.Service(ctx, dep.ServiceID)
		if err != nil {
			d.log.Error("restore active deployment", "deployment", dep.ID, "err", err)
			continue
		}
		if svc.Stopped {
			continue
		}
		id, err := d.ensureRunning(ctx, dep)
		if err != nil {
			d.log.Error("restore active deployment", "deployment", dep.ID, "err", err)
			continue
		}
		d.removeOthers(ctx, svc.ID, id)
	}
	if err := d.ApplyRoutes(ctx); err != nil {
		return fmt.Errorf("deploy: reconcile: %w", err)
	}
	return nil
}

// removeDeploymentContainers removes the containers of a deployment.
func (d *Deployer) removeDeploymentContainers(ctx context.Context, deploymentID string) {
	containers, err := d.docker.List(ctx, map[string]string{labelDeployment: deploymentID})
	if err != nil {
		d.log.Error("list deployment containers", "deployment", deploymentID, "err", err)
		return
	}
	for _, c := range containers {
		if err := d.docker.Remove(ctx, c.ID); err != nil {
			d.log.Error("remove container", "container", c.Name, "err", err)
		}
	}
}

// ensureRunning starts the container of an active deployment, recreating it
// from the deployment's image if it no longer exists, and returns its ID.
func (d *Deployer) ensureRunning(ctx context.Context, dep store.Deployment) (string, error) {
	containers, err := d.docker.List(ctx, map[string]string{labelDeployment: dep.ID})
	if err != nil {
		return "", err
	}
	if len(containers) > 0 {
		c := containers[0]
		if !c.Running && c.State != "restarting" {
			d.log.Info("starting stopped container", "container", c.Name)
			if err := d.docker.Start(ctx, c.ID); err != nil {
				return "", err
			}
		}
		if dep.ContainerID != c.ID {
			dep.ContainerID = c.ID
			return c.ID, d.store.UpdateDeployment(ctx, dep)
		}
		return c.ID, nil
	}

	svc, err := d.store.Service(ctx, dep.ServiceID)
	if err != nil {
		return "", err
	}
	project, err := d.store.Project(ctx, svc.ProjectID)
	if err != nil {
		return "", err
	}
	env, err := d.environment(ctx, svc, project, dep.CommitSHA)
	if err != nil {
		return "", err
	}
	vols, err := d.store.Volumes(ctx, svc.ID)
	if err != nil {
		return "", err
	}
	d.log.Info("recreating missing container", "service", svc.Name, "deployment", dep.ID)
	id, err := d.runContainer(ctx, containerSpec(svc, dep, env, vols))
	if err != nil {
		return "", err
	}
	dep.ContainerID = id
	return id, d.store.UpdateDeployment(ctx, dep)
}

// DeleteService stops a service's deployments, removes its containers,
// volumes, images, and build logs, and deletes it from the store. It returns
// ErrServiceBusy while the service is held.
func (d *Deployer) DeleteService(ctx context.Context, serviceID string) error {
	d.controlMu.Lock()
	defer d.controlMu.Unlock()
	d.mu.Lock()
	if _, ok := d.held[serviceID]; ok {
		d.mu.Unlock()
		return ErrServiceBusy
	}
	d.deletingServices[serviceID] = true
	d.mu.Unlock()
	defer func() { d.mu.Lock(); delete(d.deletingServices, serviceID); d.mu.Unlock() }()
	svc, err := d.store.Service(ctx, serviceID)
	if err != nil {
		return err
	}
	d.halt(serviceID, errDeleted)
	if err := d.teardown(ctx, svc); err != nil {
		return err
	}
	deps, err := d.store.Deployments(ctx, serviceID, 0)
	if err != nil {
		return err
	}
	if err := d.store.DeleteService(ctx, serviceID); err != nil {
		return err
	}
	d.removeLogs(deps)
	d.applyRoutesLogged(ctx)
	return nil
}

// DeleteProject deletes every service of a project as DeleteService does,
// then its network, then the project itself. It returns ErrServiceBusy while
// any of its services is held.
func (d *Deployer) DeleteProject(ctx context.Context, projectID string) error {
	d.controlMu.Lock()
	defer d.controlMu.Unlock()
	d.mu.Lock()
	for _, p := range d.held {
		if p == projectID {
			d.mu.Unlock()
			return ErrServiceBusy
		}
	}
	d.deletingProjects[projectID] = true
	d.mu.Unlock()
	defer func() { d.mu.Lock(); delete(d.deletingProjects, projectID); d.mu.Unlock() }()
	if _, err := d.store.Project(ctx, projectID); err != nil {
		return err
	}
	services, err := d.store.Services(ctx, projectID)
	if err != nil {
		return err
	}
	var deps []store.Deployment
	var errs []error
	for _, svc := range services {
		d.halt(svc.ID, errDeleted)
		errs = append(errs, d.teardown(ctx, svc))
		ds, err := d.store.Deployments(ctx, svc.ID, 0)
		if err != nil {
			return err
		}
		deps = append(deps, ds...)
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if err := d.docker.RemoveNetwork(ctx, networkName(projectID)); err != nil {
		return err
	}
	if err := d.store.DeleteProject(ctx, projectID); err != nil {
		return err
	}
	d.removeLogs(deps)
	d.applyRoutesLogged(ctx)
	return nil
}

// teardown removes the Docker resources of a service.
func (d *Deployer) teardown(ctx context.Context, svc store.Service) error {
	containers, err := d.docker.List(ctx, map[string]string{labelService: svc.ID})
	if err != nil {
		return err
	}
	vols, err := d.store.Volumes(ctx, svc.ID)
	if err != nil {
		return err
	}
	var errs []error
	var volumes []string
	for _, v := range vols {
		volumes = append(volumes, volumeName(v.ID))
	}
	for _, c := range containers {
		errs = append(errs, d.docker.Remove(ctx, c.ID))
		volumes = append(volumes, c.Volumes...)
	}
	slices.Sort(volumes)
	for _, name := range slices.Compact(volumes) {
		errs = append(errs, d.docker.RemoveVolume(ctx, name))
	}
	images, err := d.docker.ListImages(ctx, imageRepo(svc.ID))
	errs = append(errs, err)
	for _, img := range images {
		errs = append(errs, d.docker.RemoveImage(ctx, img.Ref))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("deploy: tear down service %s: %w", svc.Name, err)
	}
	return nil
}

// DeleteVolume deletes a volume and its data. A volume still mounted by the
// running container is detached by the service's next deployment, which then
// removes its data. It returns ErrServiceBusy while the service is held.
func (d *Deployer) DeleteVolume(ctx context.Context, volumeID string) error {
	d.controlMu.Lock()
	defer d.controlMu.Unlock()
	v, err := d.store.Volume(ctx, volumeID)
	if err != nil {
		return err
	}
	if err := d.checkHeld(v.ServiceID); err != nil {
		return err
	}
	if err := d.docker.RemoveVolume(ctx, volumeName(v.ID)); err != nil {
		if !docker.IsConflict(err) && !docker.IsNotFound(err) {
			return fmt.Errorf("deploy: remove volume %s: %w", volumeID, err)
		}
		if docker.IsConflict(err) {
			d.log.Info("volume in use; it will be removed after the next deployment",
				"volume", volumeName(v.ID), "err", err)
		}
	}
	return d.store.DeleteVolume(ctx, volumeID)
}

func (d *Deployer) removeLogs(deps []store.Deployment) {
	for _, dep := range deps {
		if err := os.Remove(d.logPath(dep.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
			d.log.Warn("remove build log", "deployment", dep.ID, "err", err)
		}
	}
}

func (d *Deployer) applyRoutesLogged(ctx context.Context) {
	if err := d.ApplyRoutes(ctx); err != nil {
		d.log.Error("apply routes", "err", err)
	}
}

// ServiceStatus is the derived state of a service.
type ServiceStatus string

// Service statuses.
const (
	StatusOffline   ServiceStatus = "offline"
	StatusDeploying ServiceStatus = "deploying"
	StatusActive    ServiceStatus = "active"
	StatusFailed    ServiceStatus = "failed"
	StatusCrashed   ServiceStatus = "crashed"
	StatusStopped   ServiceStatus = "stopped"
)

// ServiceStatuses returns the status of every service of a project, keyed by
// service ID. A service is deploying while its latest deployment is in
// progress; otherwise stopped if the user stopped it; otherwise active or
// crashed depending on whether its active
// deployment's container is running; otherwise failed if its latest
// deployment failed; otherwise offline.
func (d *Deployer) ServiceStatuses(ctx context.Context, projectID string) (map[string]ServiceStatus, error) {
	services, err := d.store.Services(ctx, projectID)
	if err != nil {
		return nil, err
	}
	containers, err := d.docker.List(ctx, map[string]string{labelProject: projectID})
	if err != nil {
		return nil, err
	}
	running := make(map[string]bool) // by deployment ID
	for _, c := range containers {
		if c.Running {
			running[c.Labels[labelDeployment]] = true
		}
	}

	statuses := make(map[string]ServiceStatus, len(services))
	for _, svc := range services {
		latest, err := d.store.LatestDeployment(ctx, svc.ID)
		switch {
		case errors.Is(err, store.ErrNotFound) && svc.Stopped:
			statuses[svc.ID] = StatusStopped
			continue
		case errors.Is(err, store.ErrNotFound):
			statuses[svc.ID] = StatusOffline
			continue
		case err != nil:
			return nil, err
		case !latest.Status.Terminal():
			statuses[svc.ID] = StatusDeploying
			continue
		case svc.Stopped:
			statuses[svc.ID] = StatusStopped
			continue
		}
		active, err := d.store.ActiveDeployment(ctx, svc.ID)
		switch {
		case err == nil && running[active.ID]:
			statuses[svc.ID] = StatusActive
		case err == nil:
			statuses[svc.ID] = StatusCrashed
		case !errors.Is(err, store.ErrNotFound):
			return nil, err
		case latest.Status == store.StatusFailed:
			statuses[svc.ID] = StatusFailed
		default:
			statuses[svc.ID] = StatusOffline
		}
	}
	return statuses, nil
}
