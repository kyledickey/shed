package deploy

import (
	"context"
	"testing"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/store"
)

// crashAfterActivation leaves the state a crash right after a switchover's
// activation would: next is active, and the container of the previous active
// deployment still runs.
func crashAfterActivation(t *testing.T, f *fixture) (prev, next store.Deployment) {
	t.Helper()
	ctx := context.Background()
	prev = f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	next, err := f.st.CreateDeployment(ctx, store.Deployment{
		ServiceID: f.svc.ID, Status: store.StatusDeploying, Trigger: store.TriggerManual,
		Image: prev.Image, Port: prev.Port,
	})
	if err != nil {
		t.Fatal(err)
	}
	next.ContainerID, err = f.docker.Run(ctx, docker.RunSpec{
		Name: containerName(f.svc.ID, next.ID), Image: next.Image, Network: networkName(f.svc.ProjectID),
		Labels: map[string]string{labelProject: f.svc.ProjectID, labelService: f.svc.ID, labelDeployment: next.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.ActivateDeployment(ctx, next); err != nil {
		t.Fatal(err)
	}
	return prev, next
}

func TestReconcileRestoresOneDeploymentPerService(t *testing.T) {
	f := newFixture(t)
	prev, next := crashAfterActivation(t, f)
	// After a host reboot nothing runs until shed starts it.
	for _, id := range []string{prev.ContainerID, next.ContainerID} {
		if err := f.docker.Stop(context.Background(), id, 0); err != nil {
			t.Fatal(err)
		}
	}

	if err := f.d.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.st.Deployment(context.Background(), prev.ID); got.Status != store.StatusRemoved {
		t.Errorf("previous status = %s, want removed", got.Status)
	}
	if _, ok := f.docker.container(prev.ContainerID); ok {
		t.Error("previous container survived recovery")
	}
	if c, ok := f.docker.container(next.ContainerID); !ok || !c.Running {
		t.Error("active container was not started")
	}
	f.proxy.mu.Lock()
	defer f.proxy.mu.Unlock()
	c, _ := f.docker.container(next.ContainerID)
	if want := upstreamAddr(c.IPs[networkName(f.svc.ProjectID)], f.svc.Port); len(f.proxy.routes) != 1 || f.proxy.routes[0].Upstream != want {
		t.Errorf("routes = %+v, want one to %s", f.proxy.routes, want)
	}
}
