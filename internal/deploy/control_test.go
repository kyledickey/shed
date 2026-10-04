package deploy

import (
	"context"
	"errors"
	"testing"

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
