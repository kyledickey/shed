package deploy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/github"
	"github.com/kyledickey/shed/internal/proxy"
	"github.com/kyledickey/shed/internal/store"
)

type stopFailureDocker struct {
	*fakeDocker
	inspectFailure bool
}

func (d stopFailureDocker) Stop(context.Context, string, time.Duration) error {
	return errors.New("stop denied")
}
func (d stopFailureDocker) Inspect(ctx context.Context, id string) (docker.Container, error) {
	if d.inspectFailure {
		return docker.Container{}, errors.New("daemon unavailable")
	}
	return d.fakeDocker.Inspect(ctx, id)
}

func TestReplacementRequiresPreviousStop(t *testing.T) {
	for _, inspectFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "stop", true: "inspect"}[inspectFailure], func(t *testing.T) {
			f := newFixture(t)
			old := f.wait(t, f.deploy(t).ID, terminal)
			f.settle(t)
			if _, err := f.st.CreateVolume(context.Background(), f.svc.ID, "/data"); err != nil {
				t.Fatal(err)
			}
			f.d.docker = stopFailureDocker{f.docker, inspectFailure}
			dep := f.wait(t, f.deploy(t).ID, terminal)
			f.settle(t)
			if dep.Status != store.StatusFailed {
				t.Fatalf("status = %s", dep.Status)
			}
			if c, ok := f.docker.container(old.ContainerID); !ok || !c.Running {
				t.Fatal("previous container not preserved")
			}
			if len(f.docker.runs) != 1 {
				t.Fatal("replacement started despite stop/inspection failure")
			}
		})
	}
}

type failingProxy struct{}

func (failingProxy) Apply([]proxy.Route) error { return errors.New("reload rejected") }

func TestProxyFailurePreservesPreviousDeployment(t *testing.T) {
	f := newFixture(t)
	old := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	f.d.proxy = failingProxy{}
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if dep.Status != store.StatusFailed {
		t.Fatalf("status = %s", dep.Status)
	}
	active, err := f.st.ActiveDeployment(context.Background(), f.svc.ID)
	if err != nil || active.ID != old.ID {
		t.Fatalf("active = %v, %v", active, err)
	}
	if c, ok := f.docker.container(old.ContainerID); !ok || !c.Running {
		t.Fatal("old container not preserved")
	}
	if _, ok := f.docker.container(dep.ContainerID); ok {
		t.Fatal("failed candidate remains")
	}
}

type failedRemovalDocker struct{ *fakeDocker }

func (d failedRemovalDocker) Remove(context.Context, string) error {
	return errors.New("daemon unavailable")
}

func TestFailedReplacementRemovalDoesNotShareStorage(t *testing.T) {
	f := newFixture(t)
	old := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if _, err := f.st.CreateVolume(context.Background(), f.svc.ID, "/data"); err != nil {
		t.Fatal(err)
	}
	f.d.docker = failedRemovalDocker{f.docker}
	f.healthy = func() bool { return false }
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if dep.Status != store.StatusFailed {
		t.Fatalf("status = %s", dep.Status)
	}
	if c, ok := f.docker.container(old.ContainerID); !ok || c.Running {
		t.Fatal("previous container restarted without exclusive storage")
	}
	if _, ok := f.docker.container(dep.ContainerID); !ok {
		t.Fatal("test did not retain the candidate")
	}
}

type cancelAfterRouteProxy struct {
	*fakeProxy
	cancel func()
}

func (p cancelAfterRouteProxy) Apply(routes []proxy.Route) error {
	if err := p.fakeProxy.Apply(routes); err != nil {
		return err
	}
	p.cancel()
	return nil
}

func TestCancellationAfterRoutingCompletesActivation(t *testing.T) {
	f := newFixture(t)
	old := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	f.d.proxy = cancelAfterRouteProxy{f.proxy, func() {
		f.d.mu.Lock()
		defer f.d.mu.Unlock()
		if w := f.d.workers[f.svc.ID]; w != nil && w.cancel != nil {
			w.cancel(errCanceled)
		}
	}}
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if dep.Status != store.StatusActive {
		t.Fatalf("status = %s, error = %s", dep.Status, dep.Error)
	}
	c, ok := f.docker.container(dep.ContainerID)
	if !ok || !c.Running {
		t.Fatal("routed candidate was removed")
	}
	if _, ok := f.docker.container(old.ContainerID); ok {
		t.Fatal("previous deployment not retired")
	}
	f.proxy.mu.Lock()
	defer f.proxy.mu.Unlock()
	if len(f.proxy.routes) != 1 || f.proxy.routes[0].Upstream != upstreamAddr(c.IPs[networkName(f.svc.ProjectID)], f.svc.Port) {
		t.Fatalf("routes = %v", f.proxy.routes)
	}
}

type blockedRemovalDocker struct {
	*fakeDocker
	entered, release chan struct{}
}

func (d blockedRemovalDocker) Remove(ctx context.Context, id string) error {
	close(d.entered)
	<-d.release
	return d.fakeDocker.Remove(ctx, id)
}

func TestDeletionRejectsConcurrentDeployment(t *testing.T) {
	for _, project := range []bool{false, true} {
		t.Run(map[bool]string{false: "service", true: "project"}[project], func(t *testing.T) {
			f := newFixture(t)
			f.wait(t, f.deploy(t).ID, terminal)
			f.settle(t)
			entered, release := make(chan struct{}), make(chan struct{})
			f.d.docker = blockedRemovalDocker{f.docker, entered, release}
			done := make(chan error, 1)
			go func() {
				if project {
					done <- f.d.DeleteProject(context.Background(), f.svc.ProjectID)
				} else {
					done <- f.d.DeleteService(context.Background(), f.svc.ID)
				}
			}()
			<-entered
			_, err := f.d.Deploy(context.Background(), f.svc.ID, store.TriggerManual, Commit{SHA: "next"})
			close(release)
			if !errors.Is(err, ErrDeleting) {
				t.Errorf("enqueue error = %v", err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if len(f.docker.containers) != 0 {
				t.Fatal("orphan container remains")
			}
		})
	}
}

type noChecksGitHub struct{ fakeGitHub }

func (noChecksGitHub) CIStatus(context.Context, string, string) (github.CIState, error) {
	return github.CINone, nil
}
func TestWaitForCIRequiresChecks(t *testing.T) {
	f := newFixture(t)
	f.svc.WaitForCI = true
	if err := f.st.UpdateService(context.Background(), f.svc); err != nil {
		t.Fatal(err)
	}
	f.d.github = func() (GitHub, bool) { return noChecksGitHub{}, true }
	f.d.ciInterval = time.Millisecond
	f.d.ciTimeout = 15 * time.Millisecond
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if dep.Status != store.StatusFailed {
		t.Fatalf("status = %s", dep.Status)
	}
	if len(f.docker.runs) != 0 {
		t.Fatal("started container without CI approval")
	}
}

type movingImageDocker struct {
	*fakeDocker
	image string
}

func (d *movingImageDocker) ResolveImage(context.Context, string) (string, error) {
	return d.image, nil
}
func TestImageRollbackUsesOriginalIdentity(t *testing.T) {
	f := newFixture(t)
	f.svc.Repo = ""
	f.svc.Image = "example:latest"
	if err := f.st.UpdateService(context.Background(), f.svc); err != nil {
		t.Fatal(err)
	}
	docker := &movingImageDocker{f.docker, "sha256:original"}
	f.d.docker = docker
	old := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	docker.image = "sha256:replacement"
	f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	rollback, err := f.d.Redeploy(context.Background(), old.ID)
	if err != nil {
		t.Fatal(err)
	}
	restored := f.wait(t, rollback.ID, terminal)
	f.settle(t)
	if restored.Image != "sha256:original" || f.docker.runs[len(f.docker.runs)-1].Image != "sha256:original" {
		t.Fatal("rollback followed mutable tag")
	}
}
func TestRedeployRejectsLegacyMutableImage(t *testing.T) {
	f := newFixture(t)
	old, err := f.st.CreateDeployment(context.Background(), store.Deployment{ServiceID: f.svc.ID, Image: "example:latest", Status: store.StatusRemoved, Trigger: store.TriggerManual})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.Redeploy(context.Background(), old.ID); !errors.Is(err, ErrNoImage) {
		t.Fatalf("redeploy error = %v", err)
	}
}
