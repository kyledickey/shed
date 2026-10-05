package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/kyledickey/shed/internal/build"
	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/github"
	"github.com/kyledickey/shed/internal/store"
)

// errCIFailed marks a deployment skipped because its commit failed CI.
var errCIFailed = errors.New("CI failed")

// job is one run of the pipeline for one deployment.
type job struct {
	*Deployer
	dep     store.Deployment
	svc     store.Service
	project store.Project
	out     io.Writer // the build log, safe for concurrent use

	secrets []string

	container   string            // ID of the new container, once created
	stoppedPrev *store.Deployment // the previous deployment, if its container was stopped early
}

// run executes the pipeline for dep and records the outcome.
func (d *Deployer) run(ctx context.Context, dep store.Deployment) {
	f, err := os.OpenFile(d.logPath(dep.ID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		d.log.Error("open build log", "deployment", dep.ID, "err", err)
		d.abandon(dep, store.StatusFailed, "open build log: "+err.Error())
		return
	}
	defer f.Close()

	j := &job{Deployer: d, dep: dep, out: d.logWriter(f)}
	if err := j.execute(ctx); err != nil {
		j.fail(ctx, err)
	}
}

// execute runs the pipeline steps in order.
func (j *job) execute(ctx context.Context) error {
	var err error
	if j.svc, err = j.store.Service(ctx, j.dep.ServiceID); err != nil {
		return err
	}
	if j.project, err = j.store.Project(ctx, j.svc.ProjectID); err != nil {
		return err
	}
	now := time.Now()
	j.dep.StartedAt = &now
	j.save()
	j.log.Info("deployment started", "deployment", j.dep.ID, "service", j.svc.Name, "trigger", j.dep.Trigger)

	if err := j.waitForCI(ctx); err != nil {
		return err
	}
	env, err := j.environment(ctx, j.svc, j.project, j.dep.CommitSHA)
	if err != nil {
		return err
	}
	if j.secrets, err = j.logSecrets(ctx, j.svc, env); err != nil {
		return err
	}
	if err := j.buildImage(ctx, env); err != nil {
		return err
	}
	if j.svc.Port == 0 {
		if found, err := j.detectPort(ctx); err != nil {
			return err
		} else if found {
			// PORT is injected only once the service has a port.
			if env, err = j.environment(ctx, j.svc, j.project, j.dep.CommitSHA); err != nil {
				return err
			}
		}
	}
	if err := j.start(ctx, env); err != nil {
		return err
	}
	if err := j.checkHealth(ctx); err != nil {
		return err
	}
	return j.switchOver(ctx)
}

// waitForCI waits until the commit's checks pass, for apps that ask for it.
func (j *job) waitForCI(ctx context.Context) error {
	if j.svc.Kind != "app" || !j.svc.WaitForCI || j.svc.Repo == "" || j.dep.CommitSHA == "" || j.dep.Image != "" {
		return nil
	}
	gh, err := j.githubClient()
	if err != nil {
		return err
	}
	j.setStatus(store.StatusWaiting)
	j.step("Waiting for CI on %s", shortSHA(j.dep.CommitSHA))

	ctx, cancel := context.WithTimeoutCause(ctx, j.ciTimeout,
		fmt.Errorf("timed out after %s waiting for CI", j.ciTimeout))
	defer cancel()
	for {
		state, err := gh.CIStatus(ctx, j.svc.Repo, j.dep.CommitSHA)
		switch {
		case err != nil && ctx.Err() == nil:
			j.printf("Checking CI status: %v", err) // Transient; keep polling.
		case state == github.CISuccess:
			j.step("CI passed")
			return nil
		case state == github.CIFailure:
			return errCIFailed
		}
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-time.After(j.ciInterval):
		}
	}
}

// buildImage builds repo apps and pulls the image of everything else. A
// redeploy already has its image.
func (j *job) buildImage(ctx context.Context, env map[string]string) error {
	if j.dep.Image != "" {
		j.step("Reusing image %s", j.dep.Image)
		return nil
	}
	j.setStatus(store.StatusBuilding)

	if j.svc.Kind == "app" && j.svc.Repo != "" {
		if j.dep.CommitSHA == "" {
			return errors.New("no commit to build")
		}
		gh, err := j.githubClient()
		if err != nil {
			return err
		}
		url, err := gh.CloneURL(ctx, j.svc.Repo)
		if err != nil {
			return err
		}
		j.step("Building %s@%s", j.svc.Repo, shortSHA(j.dep.CommitSHA))
		image := imageRepo(j.svc.ID) + ":" + j.dep.ID
		err = j.builder.Build(ctx, j.dep.ID, build.Request{
			RepoURL:        url,
			Commit:         j.dep.CommitSHA,
			RootDir:        j.svc.RootDir,
			DockerfilePath: j.svc.DockerfilePath,
			Image:          image,
			Env:            env,
		}, j.out)
		if err != nil {
			return err
		}
		j.dep.Image = image
	} else {
		if j.svc.Image == "" {
			return errors.New("service has no image or repository")
		}
		j.step("Pulling %s", j.svc.Image)
		if err := j.docker.PullImage(ctx, j.svc.Image, j.out); err != nil {
			return err
		}
		image, err := j.docker.ResolveImage(ctx, j.svc.Image)
		if err != nil {
			return err
		}
		j.dep.Image = image
	}
	j.save()
	return nil
}

// detectPort gives a service without a port the lowest TCP port its image
// exposes, and reports whether it found one.
func (j *job) detectPort(ctx context.Context) (bool, error) {
	ports, err := j.docker.ExposedPorts(ctx, j.dep.Image)
	if err != nil || len(ports) == 0 {
		return false, err
	}
	// Patches may have landed since the job started; change only the port.
	svc, err := j.store.Service(ctx, j.svc.ID)
	if err != nil {
		return false, err
	}
	if svc.Port == 0 {
		svc.Port = ports[0]
		if err := j.store.UpdateService(ctx, svc); err != nil {
			return false, err
		}
	}
	j.svc.Port = svc.Port
	j.step("Detected port %d from the image", svc.Port)
	return true, nil
}

// start runs the new container. Services with volumes or a published port
// cannot run two containers at once, so every other container of the
// service is stopped first.
func (j *job) start(ctx context.Context, env map[string]string) error {
	j.setStatus(store.StatusDeploying)
	vols, err := j.store.Volumes(ctx, j.svc.ID)
	if err != nil {
		return err
	}
	if exclusive(j.svc, vols) {
		if err := j.takeOver(ctx); err != nil {
			return err
		}
	}

	j.step("Starting container")
	j.dep.Port = j.svc.Port
	spec := containerSpec(j.svc, j.dep, env, vols)
	// Private traffic must not reach the new container before it is healthy,
	// so it gets the service's hostname only in switchOver.
	spec.Aliases = nil
	j.describe(spec)
	id, err := j.runContainer(ctx, spec)
	// A failed start may still leave the container behind, so fail must
	// remove it.
	j.container = id
	if err != nil {
		return err
	}
	j.printf("Started container %s", shortID(id))
	j.dep.ContainerID = id
	j.save()
	return nil
}

// takeOver gives the new container sole use of the service's volumes and
// published port. Containers left over from other deployments, such as a
// failed candidate whose removal failed, are removed, and the active
// deployment's container is stopped. The deployment fails, before the new
// container starts, unless every container of the service is then
// confirmed stopped.
func (j *job) takeOver(ctx context.Context) error {
	prev, err := j.store.ActiveDeployment(ctx, j.svc.ID)
	hasPrev := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	keep := ""
	if hasPrev {
		keep = prev.ID
	}
	if err := j.clearStrays(ctx, j.svc.ID, keep); err != nil {
		return err
	}
	if hasPrev && prev.ContainerID != "" {
		if err := j.stopPrevious(ctx, prev); err != nil {
			return err
		}
	}
	containers, err := j.docker.List(ctx, map[string]string{labelService: j.svc.ID})
	if err != nil {
		return fmt.Errorf("list containers of the service: %w", err)
	}
	for _, c := range containers {
		if !idle(c) {
			// Starting the previous container again could share storage too.
			j.stoppedPrev = nil
			return fmt.Errorf("container %s of the service is still %s, so its storage is not free", c.Name, c.State)
		}
	}
	return nil
}

// stopPrevious stops the running container of the active deployment prev, so
// fail can start it again.
func (j *job) stopPrevious(ctx context.Context, prev store.Deployment) error {
	c, err := j.docker.Inspect(ctx, prev.ContainerID)
	if docker.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect previous container: %w", err)
	}
	if !c.Running {
		return nil
	}
	j.step("Stopping previous deployment %s", prev.ID)
	j.printf("Volumes and published ports cannot be shared, so the previous container stops first")
	if err := j.docker.Stop(ctx, prev.ContainerID, j.stopTimeout); err != nil {
		return fmt.Errorf("stop previous container %s: %w", c.Name, err)
	}
	j.printf("Stopped container %s", c.Name)
	j.stoppedPrev = &prev
	return nil
}

// describe writes what the container will run with to the build log. It
// prints the names of variables at most, never their values.
func (j *job) describe(spec docker.RunSpec) {
	j.printf("Name: %s", spec.Name)
	j.printf("Image: %s", spec.Image)
	if j.svc.Port > 0 {
		j.printf("Network: %s, private address %s:%d once healthy", spec.Network, j.svc.Name, j.svc.Port)
	} else {
		j.printf("Network: %s, private host %s once healthy", spec.Network, j.svc.Name)
	}
	for _, m := range spec.Mounts {
		j.printf("Volume: %s → %s", m.Volume, m.Target)
	}
	for _, p := range spec.Publish {
		j.printf("Publishing host port %d → container port %d", p.HostPort, p.ContainerPort)
	}
	if len(spec.Env) == 1 {
		j.printf("Environment: 1 variable")
	} else {
		j.printf("Environment: %d variables", len(spec.Env))
	}
	if j.svc.StartCommand != "" {
		j.printf("Start command: %s", j.svc.StartCommand)
	}
}

// checkHealth waits until the new container accepts connections on its port,
// or answers its health check path, copying the container's output to the
// build log meanwhile. A service without a port is watched briefly instead.
func (j *job) checkHealth(ctx context.Context) error {
	if j.svc.Port <= 0 {
		return j.watchStartup(ctx)
	}
	what, how := fmt.Sprintf("port %d", j.svc.Port), "TCP connect to"
	if j.svc.HealthcheckPath != "" {
		what, how = j.svc.HealthcheckPath, "GET "+j.svc.HealthcheckPath+" on"
	}
	j.step("Waiting for %s to become healthy", what)

	start := time.Now()
	logs := j.follow(ctx, j.container)
	defer logs.stop()
	ctx, cancel := context.WithTimeoutCause(ctx, j.healthTimeout,
		fmt.Errorf("health check timed out after %s", j.healthTimeout))
	defer cancel()
	var lastErr error
	lastReport := start
	probing := ""
	for {
		c, err := j.docker.Inspect(ctx, j.container)
		switch {
		case err != nil:
			lastErr = err
		case c.State == "exited" || c.State == "dead" || c.State == "restarting":
			logs.drain(j.logDrain)
			return fmt.Errorf("container exited with code %d", c.ExitCode)
		case c.IPs[networkName(j.svc.ProjectID)] == "":
			lastErr = errors.New("container has no IP address yet")
		default:
			addr := upstreamAddr(c.IPs[networkName(j.svc.ProjectID)], j.svc.Port)
			if addr != probing {
				probing = addr
				j.printf("Probing %s %s (timeout %s)", how, addr, j.healthTimeout)
			}
			if lastErr = j.probe(ctx, addr, j.svc.HealthcheckPath); lastErr == nil {
				logs.stop()
				j.printf("Healthy after %s", roundDuration(time.Since(start)))
				return nil
			}
		}
		if time.Since(lastReport) >= j.healthReport {
			lastReport = time.Now()
			j.printf("Not ready yet: %v", lastErr)
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("%w (last error: %v)", context.Cause(ctx), lastErr)
			}
			return context.Cause(ctx)
		case <-time.After(j.healthInterval):
		}
	}
}

// watchStartup copies the first moments of a container's output to the build
// log, failing if the container exits meanwhile. It stands in for the health
// check of services without a port.
func (j *job) watchStartup(ctx context.Context) error {
	j.step("Watching the container start")
	j.printf("No port, so no health check; watching for %s", j.startupWatch)
	start := time.Now()
	logs := j.follow(ctx, j.container)
	defer logs.stop()
	for {
		c, err := j.docker.Inspect(ctx, j.container)
		if err == nil && (c.State == "exited" || c.State == "dead" || c.State == "restarting") {
			logs.drain(j.logDrain)
			return fmt.Errorf("container exited with code %d", c.ExitCode)
		}
		if time.Since(start) >= j.startupWatch {
			break
		}
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-time.After(j.healthInterval):
		}
	}
	logs.stop()
	j.printf("Still running after %s", roundDuration(time.Since(start)))
	return nil
}

// switchOver makes the new deployment the active one: routes point at it, and
// the previous container is removed. Everything is written to the build log
// before the deployment turns active, since log followers stop reading then.
func (j *job) switchOver(ctx context.Context) error {
	prev, err := j.store.ActiveDeployment(ctx, j.svc.ID)
	hasPrev := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	svc, err := j.store.Service(ctx, j.svc.ID)
	if err != nil {
		return err
	}
	domains, err := j.store.Domains(ctx, j.svc.ID)
	if err != nil {
		return err
	}

	j.step("Switching traffic")
	if err := j.promote(ctx); err != nil {
		return err
	}
	switch {
	case len(domains) == 0:
		j.printf("No public domains")
	case j.svc.Port <= 0:
		j.printf("No port, so its domains are not routed")
	default:
		for _, d := range domains {
			j.printf("Routing %s → port %d", d.Host, j.svc.Port)
		}
	}
	if svc.Stopped {
		j.printf("The service was stopped; this deployment starts it again")
	}
	if hasPrev {
		if prev.ContainerID != "" {
			j.printf("Removing previous deployment %s (container %s)", prev.ID, shortID(prev.ContainerID))
		} else {
			j.printf("Removing previous deployment %s", prev.ID)
		}
	}

	// Keep route computation serialized through persistence so another update
	// cannot restore the old target between routing and activation.
	j.routesMu.Lock()
	defer j.routesMu.Unlock()
	if err := ctx.Err(); err != nil {
		return context.Cause(ctx)
	}
	if j.proxy != nil {
		routes, err := j.routesFor(ctx, &j.dep)
		if err != nil {
			return err
		}
		if err := j.proxy.Apply(routes); err != nil {
			return fmt.Errorf("switch traffic: %w", err)
		}
	}
	// Once routing succeeds, complete activation even if cancellation arrives.
	// Otherwise cleanup could remove the container now receiving traffic.
	ctx = context.WithoutCancel(ctx)
	now := time.Now()
	j.dep.Status, j.dep.FinishedAt = store.StatusActive, &now
	// One transaction retires the predecessor, so a crash cannot leave two
	// active deployments.
	if err := j.store.ActivateDeployment(ctx, j.dep); err != nil {
		if j.proxy != nil {
			if routes, routeErr := j.routes(ctx); routeErr == nil {
				_ = j.proxy.Apply(routes)
			}
		}
		return fmt.Errorf("activate deployment: %w", err)
	}
	j.log.Info("deployment active", "deployment", j.dep.ID, "service", j.svc.Name)

	if svc.Stopped {
		if err := j.store.SetServiceStopped(ctx, j.svc.ID, false); err != nil {
			j.log.Error("clear stopped flag", "service", j.svc.Name, "err", err)
		}
	}
	if hasPrev && prev.ContainerID != "" {
		// Take the service's hostname from the previous container before its
		// graceful stop, so private traffic reaches only the new one.
		err := j.docker.DisconnectNetwork(ctx, networkName(j.svc.ProjectID), prev.ContainerID)
		if err != nil && !docker.IsNotFound(err) {
			j.log.Warn("disconnect previous container", "container", prev.ContainerID, "err", err)
		}
	}
	j.removeOthers(ctx, j.svc.ID, j.container)
	j.pruneImages(ctx, j.svc.ID, j.dep.Image)
	return nil
}

// promote gives the healthy new container the service's private hostname.
// Docker cannot change the aliases of a connected container, so it is
// reconnected to the project network, which may change its address.
func (j *job) promote(ctx context.Context) error {
	network := networkName(j.svc.ProjectID)
	if err := j.docker.DisconnectNetwork(ctx, network, j.container); err != nil {
		return fmt.Errorf("add private hostname: %w", err)
	}
	if err := j.docker.ConnectNetwork(ctx, network, j.container, []string{j.svc.Name}); err != nil {
		return fmt.Errorf("add private hostname: %w", err)
	}
	j.printf("Private host %s resolves to the new container", j.svc.Name)
	return nil
}

// fail records that the deployment did not go live, removes its container,
// and restarts the previous container if it was stopped.
func (j *job) fail(ctx context.Context, err error) {
	status, msg := store.StatusFailed, build.NewRedactor(io.Discard, j.secrets).Redact(err.Error())
	switch cause := context.Cause(ctx); {
	case errors.Is(err, errCIFailed):
		status = store.StatusSkipped
	case ctx.Err() != nil && cause == errShutdown:
		msg = cause.Error()
	case ctx.Err() != nil:
		status, msg = store.StatusCanceled, cause.Error()
	}
	j.printf("==> Deployment %s: %s", status, msg)
	j.log.Info("deployment ended", "deployment", j.dep.ID, "status", status, "reason", msg)

	cleanup := context.WithoutCancel(ctx)
	if j.container != "" {
		if err := j.docker.Remove(cleanup, j.container); err != nil && !docker.IsNotFound(err) {
			j.log.Error("remove failed container", "container", j.container, "err", err)
		}
	}
	if prev := j.stoppedPrev; prev != nil {
		// The new container may exist even if its ID is unknown, as when a
		// start failed ambiguously, so restart the previous one only once no
		// other container of the service is left to share its storage.
		if err := j.clearStrays(cleanup, j.svc.ID, prev.ID); err != nil {
			j.printf("Previous deployment %s remains stopped: %v", prev.ID, err)
			j.log.Error("cannot safely restart previous container", "container", prev.ContainerID, "err", err)
			msg += "; previous deployment left stopped because the replacement could not be confirmed removed"
		} else if err := j.docker.Start(cleanup, prev.ContainerID); err != nil {
			j.log.Error("restart previous container", "container", prev.ContainerID, "err", err)
		}
	}

	now := time.Now()
	j.dep.Status, j.dep.Error, j.dep.FinishedAt = status, msg, &now
	j.save()
}

// githubClient returns the GitHub client, or an error if GitHub is not set up.
func (j *job) githubClient() (GitHub, error) {
	if j.github != nil {
		if gh, ok := j.github(); ok {
			return gh, nil
		}
	}
	return nil, errors.New("GitHub App is not configured")
}

func (j *job) setStatus(s store.DeploymentStatus) {
	j.dep.Status = s
	j.save()
}

// save persists the deployment. It is not canceled with the job, so the
// outcome of a canceled deployment is still recorded.
func (j *job) save() {
	if err := j.store.UpdateDeployment(context.Background(), j.dep); err != nil {
		j.log.Error("save deployment", "deployment", j.dep.ID, "err", err)
	}
}

// step writes a pipeline step heading to the build log.
func (j *job) step(format string, args ...any) {
	j.printf("==> "+format, args...)
}

// printf writes a line to the build log.
func (j *job) printf(format string, args ...any) {
	fmt.Fprintf(j.out, format+"\n", args...)
}

// shortID returns the short form of a container ID.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// roundDuration rounds d for display.
func roundDuration(d time.Duration) time.Duration {
	return d.Round(100 * time.Millisecond)
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
