package deploy

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/proxy"
	"github.com/kyledickey/shed/internal/store"
)

func (f *fixture) routes() []proxy.Route {
	f.proxy.mu.Lock()
	defer f.proxy.mu.Unlock()
	return slices.Clone(f.proxy.routes)
}

// flakyInspectDocker fails to inspect one container, as a busy or restarting
// daemon might.
type flakyInspectDocker struct {
	*fakeDocker
	id string
}

func (d flakyInspectDocker) Inspect(ctx context.Context, id string) (docker.Container, error) {
	if id == d.id {
		return docker.Container{}, errors.New("context deadline exceeded")
	}
	return d.fakeDocker.Inspect(ctx, id)
}

func TestRoutesSurviveInspectionErrors(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	before := f.routes()
	if len(before) != 1 {
		t.Fatalf("routes = %+v, want one", before)
	}

	f.d.docker = flakyInspectDocker{f.docker, dep.ContainerID}
	if err := f.d.ApplyRoutes(ctx); err == nil {
		t.Error("ApplyRoutes succeeded without knowing where the service runs")
	}
	if got := f.routes(); !slices.Equal(got, before) {
		t.Errorf("routes = %+v, want the last good %+v", got, before)
	}

	// A container that is confirmed gone loses its route.
	f.d.docker = f.docker
	if err := f.docker.Remove(ctx, dep.ContainerID); err != nil {
		t.Fatal(err)
	}
	if err := f.d.ApplyRoutes(ctx); err != nil {
		t.Fatal(err)
	}
	if got := f.routes(); len(got) != 0 {
		t.Errorf("routes = %+v, want none", got)
	}
}

func TestRoutesFollowDeployedPort(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if dep.Port != 8080 {
		t.Fatalf("deployment port = %d, want 8080", dep.Port)
	}

	// Editing the port does not move traffic before the next deployment,
	// even when something else rebuilds the routes.
	f.svc.Port = 9000
	if err := f.st.UpdateService(ctx, f.svc); err != nil {
		t.Fatal(err)
	}
	if err := f.d.ApplyRoutes(ctx); err != nil {
		t.Fatal(err)
	}
	if r := f.routes(); len(r) != 1 || !strings.HasSuffix(r[0].Upstream, ":8080") {
		t.Fatalf("routes after editing the port = %+v, want port 8080", r)
	}

	// A container recreated for the deployment runs as it did.
	if err := f.d.StopService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.docker.Remove(ctx, dep.ContainerID); err != nil {
		t.Fatal(err)
	}
	if err := f.d.StartService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	if env := strings.Join(f.docker.runs[len(f.docker.runs)-1].Env, "\n"); !strings.Contains(env, "PORT=8080") {
		t.Errorf("recreated container env lacks PORT=8080:\n%s", env)
	}
	if r := f.routes(); len(r) != 1 || !strings.HasSuffix(r[0].Upstream, ":8080") {
		t.Fatalf("routes after restart = %+v, want port 8080", r)
	}

	next := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if next.Status != store.StatusActive || next.Port != 9000 {
		t.Fatalf("next = %s on port %d, want active on 9000", next.Status, next.Port)
	}
	if r := f.routes(); len(r) != 1 || !strings.HasSuffix(r[0].Upstream, ":9000") {
		t.Fatalf("routes after redeploying = %+v, want port 9000", r)
	}
}
