package deploy

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kyledickey/shed/internal/store"
)

// removeImage makes an image unavailable, as pruning or a new host does.
func (f *fakeDocker) removeImage(t *testing.T, ref string) {
	t.Helper()
	if err := f.RemoveImage(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
}

// exclusiveFixture returns a fixture whose service has a volume, so its
// containers cannot run side by side, with one deployment superseded by an
// active one.
func exclusiveFixture(t *testing.T) (f *fixture, old, active store.Deployment) {
	t.Helper()
	f = newFixture(t)
	if _, err := f.st.CreateVolume(context.Background(), f.svc.ID, "/data"); err != nil {
		t.Fatal(err)
	}
	old = f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	active = f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if active.Status != store.StatusActive {
		t.Fatalf("status = %s (%s)", active.Status, active.Error)
	}
	return f, old, active
}

func (f *fixture) requireRunning(t *testing.T, id string) {
	t.Helper()
	if c, ok := f.docker.container(id); !ok || !c.Running {
		t.Fatalf("container %s is not running", id)
	}
}

func TestRedeployRefusesUnavailableImage(t *testing.T) {
	f, old, active := exclusiveFixture(t)
	f.docker.removeImage(t, old.Image)
	before, err := f.st.Deployments(context.Background(), f.svc.ID, 0)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.d.Redeploy(context.Background(), old.ID); !errors.Is(err, ErrImageUnavailable) {
		t.Fatalf("redeploy error = %v, want ErrImageUnavailable", err)
	}
	after, err := f.st.Deployments(context.Background(), f.svc.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("deployments = %d, want %d: nothing queued", len(after), len(before))
	}
	f.requireRunning(t, active.ContainerID)
}

// vanishingImageDocker reports images present once, then gone, as when an
// image is removed after a redeploy was queued.
type vanishingImageDocker struct {
	*fakeDocker
	checks atomic.Int32
}

func (d *vanishingImageDocker) ResolveImage(ctx context.Context, ref string) (string, error) {
	if d.checks.Add(1) > 1 {
		_ = d.fakeDocker.RemoveImage(ctx, ref)
	}
	return d.fakeDocker.ResolveImage(ctx, ref)
}

func TestRedeployOfVanishedImageKeepsPreviousRunning(t *testing.T) {
	f, old, active := exclusiveFixture(t)
	f.d.docker = &vanishingImageDocker{fakeDocker: f.docker}
	runs := len(f.docker.runs)

	dep, err := f.d.Redeploy(context.Background(), old.ID)
	if err != nil {
		t.Fatal(err)
	}
	dep = f.wait(t, dep.ID, terminal)
	f.settle(t)
	if dep.Status != store.StatusFailed || !strings.Contains(dep.Error, "no longer on this host") {
		t.Fatalf("status = %s (%q), want failed for the missing image", dep.Status, dep.Error)
	}
	if strings.Contains(f.buildLog(t, dep.ID), "Stopping previous deployment") {
		t.Error("previous deployment was stopped for an image that is gone")
	}
	if len(f.docker.runs) != runs {
		t.Error("a container was run")
	}
	f.requireRunning(t, active.ContainerID)
	if got, _ := f.st.ActiveDeployment(context.Background(), f.svc.ID); got.ID != active.ID {
		t.Errorf("active deployment = %s, want %s", got.ID, active.ID)
	}
}

func TestRecoveryWithoutImageCreatesNothing(t *testing.T) {
	f, _, active := exclusiveFixture(t)
	ctx := context.Background()
	// A new host: the container and the image are gone.
	if err := f.docker.Remove(ctx, active.ContainerID); err != nil {
		t.Fatal(err)
	}
	f.docker.removeImage(t, active.Image)
	runs, volumes := len(f.docker.runs), len(f.docker.ensuredVolumes)

	if err := f.d.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.docker.runs) != runs || len(f.docker.ensuredVolumes) != volumes {
		t.Fatal("recovery ran a container or created volumes without the image")
	}
	statuses, err := f.d.ServiceStatuses(ctx, f.svc.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if statuses[f.svc.ID] != StatusCrashed {
		t.Errorf("status = %s, want crashed", statuses[f.svc.ID])
	}

	if err := f.d.StopService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.d.StartService(ctx, f.svc.ID); !errors.Is(err, ErrImageUnavailable) {
		t.Fatalf("start error = %v, want ErrImageUnavailable", err)
	}
	if svc, _ := f.st.Service(ctx, f.svc.ID); !svc.Stopped {
		t.Error("a failed start left the service marked running")
	}
	if len(f.docker.runs) != runs || len(f.docker.ensuredVolumes) != volumes {
		t.Fatal("start ran a container or created volumes without the image")
	}
}

func TestAvailableImages(t *testing.T) {
	f := newFixture(t)
	var deps []store.Deployment
	for range keepImages + 2 {
		deps = append(deps, f.wait(t, f.deploy(t).ID, terminal))
		f.settle(t)
	}
	const pulled, gone = "sha256:pulled", "sha256:gone"
	f.docker.removeImage(t, gone)
	images := []string{pulled, gone, ""}
	for _, d := range deps {
		images = append(images, d.Image)
	}

	got, err := f.d.AvailableImages(context.Background(), f.svc.ID, images)
	if err != nil {
		t.Fatal(err)
	}
	pruned := 0
	for _, d := range deps {
		if !got[d.Image] {
			pruned++
		}
	}
	if pruned != 2 || !got[deps[len(deps)-1].Image] {
		t.Errorf("unavailable built images = %d, want the 2 pruned ones; got %v", pruned, got)
	}
	if !got[pulled] || got[gone] {
		t.Errorf("pulled = %v, gone = %v; want true, false", got[pulled], got[gone])
	}
}
