package deploy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/store"
)

func (f *fixture) serviceStatus(t *testing.T) ServiceStatus {
	t.Helper()
	statuses, err := f.d.ServiceStatuses(context.Background(), f.svc.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	return statuses[f.svc.ID]
}

func (f *fixture) routeCount() int {
	f.proxy.mu.Lock()
	defer f.proxy.mu.Unlock()
	return len(f.proxy.routes)
}

func (f *fixture) stopped(t *testing.T) bool {
	t.Helper()
	svc, err := f.st.Service(context.Background(), f.svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	return svc.Stopped
}

func TestStopStartRestart(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)

	if err := f.d.StopService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	if c, ok := f.docker.container(dep.ContainerID); !ok || c.Running {
		t.Fatalf("after stop: container exists %v, running %v; want kept and stopped", ok, c.Running)
	}
	if got := f.wait(t, dep.ID, terminal); got.Status != store.StatusActive {
		t.Errorf("after stop: deployment status = %s, want active", got.Status)
	}
	if !f.stopped(t) {
		t.Error("after stop: Stopped = false")
	}
	if got := f.serviceStatus(t); got != StatusStopped {
		t.Errorf("after stop: service status = %s, want stopped", got)
	}
	if n := f.routeCount(); n != 0 {
		t.Errorf("after stop: %d routes, want 0", n)
	}
	if err := f.d.RestartService(ctx, f.svc.ID); !errors.Is(err, ErrServiceStopped) {
		t.Errorf("restart while stopped: err = %v, want ErrServiceStopped", err)
	}

	if err := f.d.StartService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	if c, ok := f.docker.container(dep.ContainerID); !ok || !c.Running {
		t.Error("after start: container is not running")
	}
	if f.stopped(t) {
		t.Error("after start: Stopped = true")
	}
	if got := f.serviceStatus(t); got != StatusActive {
		t.Errorf("after start: service status = %s, want active", got)
	}
	if n := f.routeCount(); n != 1 {
		t.Errorf("after start: %d routes, want 1", n)
	}

	if err := f.d.RestartService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.docker.restarts) != 1 || f.docker.restarts[0] != dep.ContainerID {
		t.Errorf("restarts = %v, want [%s]", f.docker.restarts, dep.ContainerID)
	}
}

func TestStartRecreatesMissingContainer(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if err := f.d.StopService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	f.docker.Remove(ctx, dep.ContainerID)

	if err := f.d.StartService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	got := f.wait(t, dep.ID, terminal)
	if c, ok := f.docker.container(got.ContainerID); got.ContainerID == dep.ContainerID || !ok || !c.Running {
		t.Errorf("container %q not recreated and running", got.ContainerID)
	}
}

func TestControlWithoutDeployment(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	tests := []struct {
		name string
		op   func(context.Context, string) error
		want error
	}{
		{"start", f.d.StartService, ErrNoContainer},
		{"restart", f.d.RestartService, ErrNoContainer},
		{"stop unknown", func(ctx context.Context, _ string) error { return f.d.StopService(ctx, "nope") }, store.ErrNotFound},
		{"start unknown", func(ctx context.Context, _ string) error { return f.d.StartService(ctx, "nope") }, store.ErrNotFound},
		{"restart unknown", func(ctx context.Context, _ string) error { return f.d.RestartService(ctx, "nope") }, store.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.op(ctx, f.svc.ID); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
	if err := f.d.StopService(ctx, f.svc.ID); err != nil {
		t.Fatalf("stop without deployment: %v", err)
	}
	if got := f.serviceStatus(t); got != StatusStopped {
		t.Errorf("service status = %s, want stopped", got)
	}
}

func TestStopCancelsDeployment(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.builder.block.Store(true)
	dep := f.deploy(t)
	<-f.builder.blocked
	if got := f.serviceStatus(t); got != StatusDeploying {
		t.Errorf("while building: service status = %s, want deploying", got)
	}

	if err := f.d.StopService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	got := f.wait(t, dep.ID, terminal)
	if got.Status != store.StatusCanceled || got.Error != "service stopped" {
		t.Errorf("deployment = %s (%q), want canceled by stop", got.Status, got.Error)
	}
	if got := f.serviceStatus(t); got != StatusStopped {
		t.Errorf("service status = %s, want stopped", got)
	}
}

func TestDeployClearsStopped(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if err := f.d.StopService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}

	f.builder.block.Store(true)
	f.deploy(t)
	<-f.builder.blocked
	if got := f.serviceStatus(t); got != StatusDeploying {
		t.Errorf("deploying while stopped: service status = %s, want deploying", got)
	}

	f.builder.block.Store(false)
	second := f.wait(t, f.deploy(t).ID, terminal) // supersedes the blocked one
	f.settle(t)
	if second.Status != store.StatusActive {
		t.Fatalf("status = %s (%q), want active", second.Status, second.Error)
	}
	if f.stopped(t) {
		t.Error("Stopped still set after a deployment went live")
	}
	if got := f.serviceStatus(t); got != StatusActive {
		t.Errorf("service status = %s, want active", got)
	}
	if n := f.routeCount(); n != 1 {
		t.Errorf("%d routes, want 1", n)
	}
	assertLog(t, f.buildLog(t, second.ID), []string{"The service was stopped; this deployment starts it again"}, nil)
}

func TestFailedDeployKeepsStoppedPreviousStopped(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	if _, err := f.st.CreateVolume(ctx, f.svc.ID, "/data"); err != nil {
		t.Fatal(err)
	}
	first := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if err := f.d.StopService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}

	f.healthy = func() bool { return false }
	if second := f.wait(t, f.deploy(t).ID, terminal); second.Status != store.StatusFailed {
		t.Fatalf("second status = %s, want failed", second.Status)
	}
	f.settle(t)
	if c, ok := f.docker.container(first.ContainerID); !ok || c.Running {
		t.Error("stopped previous container was started by a failed deployment")
	}
	if got := f.serviceStatus(t); got != StatusStopped {
		t.Errorf("service status = %s, want stopped", got)
	}
}

func TestReconcileSkipsStopped(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if err := f.d.StopService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}

	if err := f.d.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if c, ok := f.docker.container(dep.ContainerID); !ok || c.Running {
		t.Error("reconcile started the container of a stopped service")
	}
	if n := f.routeCount(); n != 0 {
		t.Errorf("%d routes, want 0", n)
	}
}

func TestServiceStatusNotCrashedWhenStopped(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)

	f.docker.Stop(ctx, dep.ContainerID, 0)
	if got := f.serviceStatus(t); got != StatusCrashed {
		t.Errorf("container died: service status = %s, want crashed", got)
	}
	if err := f.d.StopService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.serviceStatus(t); got != StatusStopped {
		t.Errorf("stopped: service status = %s, want stopped", got)
	}
}

// serviceContainers returns the containers of the fixture's service.
func (f *fixture) serviceContainers(t *testing.T) []docker.Container {
	t.Helper()
	cs, err := f.docker.List(context.Background(), map[string]string{labelService: f.svc.ID})
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func (f *fixture) hold(t *testing.T) *Held {
	t.Helper()
	h, err := f.d.Hold(context.Background(), f.svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func (f *fixture) addVolume(t *testing.T) store.Volume {
	t.Helper()
	v, err := f.st.CreateVolume(context.Background(), f.svc.ID, "/data")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestHoldRejectsOperations(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	vol := f.addVolume(t)
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	h := f.hold(t)

	tests := []struct {
		name string
		op   func() error
	}{
		{"deploy", func() error {
			_, err := f.d.Deploy(ctx, f.svc.ID, store.TriggerManual, Commit{SHA: "next"})
			return err
		}},
		{"redeploy", func() error { _, err := f.d.Redeploy(ctx, dep.ID); return err }},
		{"start", func() error { return f.d.StartService(ctx, f.svc.ID) }},
		{"stop", func() error { return f.d.StopService(ctx, f.svc.ID) }},
		{"restart", func() error { return f.d.RestartService(ctx, f.svc.ID) }},
		{"delete service", func() error { return f.d.DeleteService(ctx, f.svc.ID) }},
		{"delete volume", func() error { return f.d.DeleteVolume(ctx, vol.ID) }},
		{"delete project", func() error { return f.d.DeleteProject(ctx, f.svc.ProjectID) }},
		{"hold", func() error { _, err := f.d.Hold(ctx, f.svc.ID); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.op(); !errors.Is(err, ErrServiceBusy) {
				t.Errorf("err = %v, want ErrServiceBusy", err)
			}
		})
	}

	if deps, err := f.st.Deployments(ctx, f.svc.ID, 0); err != nil || len(deps) != 1 {
		t.Errorf("deployments = %d (%v), want 1", len(deps), err)
	}
	if c, ok := f.docker.container(dep.ContainerID); !ok || !c.Running {
		t.Error("held container was changed")
	}
	if f.stopped(t) {
		t.Error("held service was stopped")
	}
	if _, err := f.st.Volume(ctx, vol.ID); err != nil {
		t.Errorf("volume: %v", err)
	}

	if err := h.Release(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.d.RestartService(ctx, f.svc.ID); err != nil {
		t.Errorf("restart after release: %v", err)
	}
	if err := f.d.DeleteVolume(ctx, vol.ID); err != nil {
		t.Errorf("delete volume after release: %v", err)
	}
}

func TestHoldAdmission(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	if _, err := f.d.Hold(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("hold unknown: err = %v, want ErrNotFound", err)
	}

	f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	entered, release := make(chan struct{}), make(chan struct{})
	f.d.docker = blockedRemovalDocker{f.docker, entered, release}
	done := make(chan error, 1)
	go func() { done <- f.d.DeleteService(ctx, f.svc.ID) }()
	<-entered
	_, err := f.d.Hold(ctx, f.svc.ID)
	close(release)
	if !errors.Is(err, ErrDeleting) {
		t.Errorf("hold while deleting: err = %v, want ErrDeleting", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestHoldCancelsDeployment(t *testing.T) {
	t.Run("building", func(t *testing.T) {
		f := newFixture(t)
		f.builder.block.Store(true)
		dep := f.deploy(t)
		<-f.builder.blocked

		f.hold(t)
		got := f.wait(t, dep.ID, terminal)
		if got.Status != store.StatusCanceled || got.Error != errHeld.Error() {
			t.Errorf("deployment = %s (%q), want canceled by hold", got.Status, got.Error)
		}
	})

	// A replacement that stopped its predecessor to share its volume is
	// canceled during its health check: Hold returns only after the
	// predecessor runs again and no worker is left to touch the containers.
	t.Run("replacing", func(t *testing.T) {
		ctx := context.Background()
		f := newFixture(t)
		f.addVolume(t)
		prev := f.wait(t, f.deploy(t).ID, terminal)
		f.settle(t)
		f.d.healthTimeout = 5 * time.Second
		f.healthy = func() bool { return false }
		dep := f.wait(t, f.deploy(t).ID, func(d store.Deployment) bool { return d.ContainerID != "" })

		h := f.hold(t)
		f.d.mu.Lock()
		w := f.d.workers[f.svc.ID]
		f.d.mu.Unlock()
		if w != nil {
			t.Fatal("worker still running after Hold")
		}
		if got := f.wait(t, dep.ID, terminal); got.Status != store.StatusCanceled {
			t.Errorf("replacement = %s, want canceled", got.Status)
		}
		cs := f.serviceContainers(t)
		if len(cs) != 1 || cs[0].ID != prev.ContainerID || !cs[0].Running {
			t.Fatalf("containers = %+v, want only the running predecessor", cs)
		}
		if running, err := h.Running(ctx); err != nil || !running {
			t.Errorf("Running = %v, %v; want true", running, err)
		}
	})
}

func TestHoldStopAndRemoveRelease(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.addVolume(t)
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	h := f.hold(t)

	if active, err := h.Active(ctx); err != nil || active.ID != dep.ID {
		t.Fatalf("Active = %s, %v; want %s", active.ID, err, dep.ID)
	}
	if running, err := h.Running(ctx); err != nil || !running {
		t.Fatalf("Running = %v, %v; want true", running, err)
	}
	if err := h.StopAndRemove(ctx); err != nil {
		t.Fatal(err)
	}
	if cs := f.serviceContainers(t); len(cs) != 0 {
		t.Errorf("containers after StopAndRemove = %+v, want none", cs)
	}
	if running, err := h.Running(ctx); err != nil || running {
		t.Errorf("Running after StopAndRemove = %v, %v; want false", running, err)
	}
	if active, err := h.Active(ctx); err != nil || active.Status != store.StatusActive || active.ContainerID != "" {
		t.Errorf("active after StopAndRemove = %s %q, %v; want active without container", active.Status, active.ContainerID, err)
	}
	if n := f.routeCount(); n != 0 {
		t.Errorf("routes after StopAndRemove = %d, want 0", n)
	}

	if err := h.Release(ctx); err != nil {
		t.Fatal(err)
	}
	active := f.wait(t, dep.ID, terminal)
	c, ok := f.docker.container(active.ContainerID)
	if active.ContainerID == dep.ContainerID || !ok || !c.Running {
		t.Fatalf("container %q not recreated and running", active.ContainerID)
	}
	if spec := f.docker.runs[len(f.docker.runs)-1]; len(spec.Mounts) != 1 || spec.Mounts[0].Target != "/data" {
		t.Errorf("recreated mounts = %+v, want the volume", spec.Mounts)
	}
	f.proxy.mu.Lock()
	routes := f.proxy.routes
	f.proxy.mu.Unlock()
	if want := upstreamAddr(c.IPs[networkName(f.svc.ProjectID)], f.svc.Port); len(routes) != 1 || routes[0].Upstream != want {
		t.Errorf("routes = %+v, want one to %s", routes, want)
	}

	if err := h.Release(ctx); err != nil {
		t.Errorf("second Release: %v", err)
	}
	if err := h.StopAndRemove(ctx); err == nil {
		t.Error("StopAndRemove after Release succeeded")
	}
	if got := f.wait(t, f.deploy(t).ID, terminal); got.Status != store.StatusActive {
		t.Errorf("deploy after release = %s (%q), want active", got.Status, got.Error)
	}
}

func TestReleaseKeepsUserStoppedService(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "kept", true: "removed"}[remove], func(t *testing.T) {
			ctx := context.Background()
			f := newFixture(t)
			f.addVolume(t)
			f.wait(t, f.deploy(t).ID, terminal)
			f.settle(t)
			if err := f.d.StopService(ctx, f.svc.ID); err != nil {
				t.Fatal(err)
			}
			h := f.hold(t)
			if running, err := h.Running(ctx); err != nil || running {
				t.Fatalf("Running = %v, %v; want false", running, err)
			}
			if remove {
				if err := h.StopAndRemove(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if err := h.Release(ctx); err != nil {
				t.Fatal(err)
			}
			for _, c := range f.serviceContainers(t) {
				if c.Running {
					t.Errorf("container %s started for a stopped service", c.ID)
				}
			}
			if !f.stopped(t) {
				t.Error("Stopped cleared by Release")
			}
			if n := f.routeCount(); n != 0 {
				t.Errorf("%d routes, want 0", n)
			}
		})
	}
}

type runFailureDocker struct{ *fakeDocker }

func (runFailureDocker) Run(context.Context, docker.RunSpec) (string, error) {
	return "", errors.New("daemon unavailable")
}

func TestReleaseLiftsHoldWhenStartFails(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	h := f.hold(t)
	if err := h.StopAndRemove(ctx); err != nil {
		t.Fatal(err)
	}
	f.d.docker = runFailureDocker{f.docker}

	if err := h.Release(ctx); err == nil {
		t.Fatal("Release succeeded without a container")
	}
	f.d.docker = f.docker
	if err := f.d.StartService(ctx, f.svc.ID); err != nil {
		t.Fatalf("start after failed release: %v", err)
	}
	if running, err := f.hold(t).Running(ctx); err != nil || !running {
		t.Errorf("Running = %v, %v; want true", running, err)
	}
}

func TestDeleteProjectRejectedWhileServiceHeld(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	db, err := f.st.CreateService(ctx, store.Service{ProjectID: f.svc.ProjectID, Name: "db", Kind: "app", Image: "redis"})
	if err != nil {
		t.Fatal(err)
	}
	h, err := f.d.Hold(ctx, db.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.d.DeleteProject(ctx, f.svc.ProjectID); !errors.Is(err, ErrServiceBusy) {
		t.Fatalf("delete project: err = %v, want ErrServiceBusy", err)
	}
	for _, id := range []string{f.svc.ID, db.ID} {
		if _, err := f.st.Service(ctx, id); err != nil {
			t.Errorf("service %s: %v", id, err)
		}
	}
	if err := f.d.DeleteService(ctx, f.svc.ID); err != nil {
		t.Errorf("delete unheld service: %v", err)
	}
	if err := h.Release(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.d.DeleteProject(ctx, f.svc.ProjectID); err != nil {
		t.Errorf("delete project after release: %v", err)
	}
}

// TestHoldExcludesConcurrentDeployments deploys continuously while the
// service is repeatedly held, checking that nothing runs a container while
// it is held and removed.
func TestHoldExcludesConcurrentDeployments(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.addVolume(t)
	f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)

	stop := make(chan struct{})
	errs := make(chan error, 1)
	go func() {
		defer close(errs)
		for {
			select {
			case <-stop:
				return
			default:
			}
			_, err := f.d.Deploy(ctx, f.svc.ID, store.TriggerManual, Commit{SHA: "next"})
			if err != nil && !errors.Is(err, ErrServiceBusy) {
				errs <- err
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()

	for range 5 {
		h := f.hold(t)
		if err := h.StopAndRemove(ctx); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
		if cs := f.serviceContainers(t); len(cs) != 0 {
			t.Fatalf("containers while held = %+v, want none", cs)
		}
		if err := h.Release(ctx); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(stop)
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	f.settle(t)
}
