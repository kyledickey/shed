package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/kyledickey/shed/internal/build"
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
	out     io.Writer // the build log

	container   string // ID of the new container, once created
	stoppedPrev string // ID of the previous container, if it was stopped early
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

	j := &job{Deployer: d, dep: dep, out: f}
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

	start := time.Now()
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
		case state == github.CINone && time.Since(start) >= j.ciGrace:
			j.step("No CI checks found; continuing")
			return nil
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
		j.dep.Image = j.svc.Image
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
// cannot run two containers at once, so their previous container is stopped
// first.
func (j *job) start(ctx context.Context, env map[string]string) error {
	j.setStatus(store.StatusDeploying)
	vols, err := j.store.Volumes(ctx, j.svc.ID)
	if err != nil {
		return err
	}
	if len(vols) > 0 || j.svc.PublicPort > 0 {
		prev, err := j.store.ActiveDeployment(ctx, j.svc.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err == nil && prev.ContainerID != "" {
			j.step("Stopping previous deployment")
			if err := j.docker.Stop(ctx, prev.ContainerID, j.stopTimeout); err != nil {
				j.printf("Stopping previous container: %v", err)
			}
			j.stoppedPrev = prev.ContainerID
		}
	}

	j.step("Starting container")
	id, err := j.runContainer(ctx, j.svc, j.dep, env, vols)
	if err != nil {
		return err
	}
	j.container = id
	j.dep.ContainerID = id
	j.save()
	return nil
}

// checkHealth waits until the new container accepts connections on its port,
// or answers its health check path.
func (j *job) checkHealth(ctx context.Context) error {
	if j.svc.Port <= 0 {
		return nil
	}
	what := fmt.Sprintf("port %d", j.svc.Port)
	if j.svc.HealthcheckPath != "" {
		what = j.svc.HealthcheckPath
	}
	j.step("Waiting for %s to become healthy", what)

	ctx, cancel := context.WithTimeoutCause(ctx, j.healthTimeout,
		fmt.Errorf("health check timed out after %s", j.healthTimeout))
	defer cancel()
	var lastErr error
	for {
		c, err := j.docker.Inspect(ctx, j.container)
		switch {
		case err != nil:
			lastErr = err
		case c.State == "exited" || c.State == "dead" || c.State == "restarting":
			return fmt.Errorf("container exited with code %d", c.ExitCode)
		case c.IPs[networkName(j.svc.ProjectID)] == "":
			lastErr = errors.New("container has no IP address yet")
		default:
			addr := upstreamAddr(c.IPs[networkName(j.svc.ProjectID)], j.svc.Port)
			if lastErr = j.probe(ctx, addr, j.svc.HealthcheckPath); lastErr == nil {
				j.step("Healthy")
				return nil
			}
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

// switchOver makes the new deployment the active one: routes point at it, and
// the previous container is removed.
func (j *job) switchOver(ctx context.Context) error {
	prev, err := j.store.ActiveDeployment(ctx, j.svc.ID)
	hasPrev := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}

	j.step("Switching traffic")
	now := time.Now()
	j.dep.Status, j.dep.FinishedAt = store.StatusActive, &now
	j.save()
	j.log.Info("deployment active", "deployment", j.dep.ID, "service", j.svc.Name)

	// The new deployment is live; the rest is cleanup that must not be
	// interrupted by a cancellation.
	ctx = context.WithoutCancel(ctx)
	if err := j.ApplyRoutes(ctx); err != nil {
		j.log.Error("apply routes", "err", err)
		j.printf("Applying routes: %v", err)
	}
	if hasPrev {
		prev.Status = store.StatusRemoved
		if err := j.store.UpdateDeployment(ctx, prev); err != nil {
			j.log.Error("mark deployment removed", "deployment", prev.ID, "err", err)
		}
	}
	j.removeOthers(ctx, j.svc.ID, j.container)
	j.pruneImages(ctx, j.svc.ID, j.dep.Image)
	return nil
}

// fail records that the deployment did not go live, removes its container,
// and restarts the previous container if it was stopped.
func (j *job) fail(ctx context.Context, err error) {
	status, msg := store.StatusFailed, err.Error()
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
		if err := j.docker.Remove(cleanup, j.container); err != nil {
			j.log.Error("remove failed container", "container", j.container, "err", err)
		}
	}
	if j.stoppedPrev != "" {
		if err := j.docker.Start(cleanup, j.stoppedPrev); err != nil {
			j.log.Error("restart previous container", "container", j.stoppedPrev, "err", err)
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

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
