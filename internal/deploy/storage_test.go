package deploy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/store"
)

func TestLeftoverCandidateBlocksExclusiveDeploy(t *testing.T) {
	f := newFixture(t)
	if _, err := f.st.CreateVolume(context.Background(), f.svc.ID, "/data"); err != nil {
		t.Fatal(err)
	}
	old := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	f.d.docker = failedRemovalDocker{f.docker}
	f.healthy = func() bool { return false }
	failed := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if _, ok := f.docker.container(failed.ContainerID); !ok {
		t.Fatal("test did not retain the candidate")
	}

	// The leftover candidate cannot be removed, so nothing else may start.
	f.healthy = func() bool { return true }
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if dep.Status != store.StatusFailed || !strings.Contains(dep.Error, "remove leftover container") {
		t.Fatalf("status = %s (%q), want failed on the leftover container", dep.Status, dep.Error)
	}
	if n := len(f.docker.runs); n != 2 {
		t.Fatalf("%d containers started, want 2", n)
	}
	for _, id := range []string{old.ContainerID, failed.ContainerID} {
		if c, ok := f.docker.container(id); ok && c.Running {
			t.Errorf("container %s running", id)
		}
	}

	// Once Docker cooperates, the next deployment clears the leftover.
	f.d.docker = f.docker
	dep = f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if dep.Status != store.StatusActive {
		t.Fatalf("status = %s (%q), want active", dep.Status, dep.Error)
	}
	containers, _ := f.docker.List(context.Background(), map[string]string{labelService: f.svc.ID})
	if len(containers) != 1 || containers[0].ID != dep.ContainerID {
		t.Errorf("containers = %+v, want only the new one", containers)
	}
}

func TestRecoveryKeepsStorageExclusive(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	if _, err := f.st.CreateVolume(ctx, f.svc.ID, "/data"); err != nil {
		t.Fatal(err)
	}
	active := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	// A crash while the candidate ran: the candidate is up and the previous
	// container stopped.
	stale, err := f.st.CreateDeployment(ctx, store.Deployment{
		ServiceID: f.svc.ID, Status: store.StatusDeploying, Trigger: store.TriggerManual,
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := f.docker.Run(ctx, docker.RunSpec{Labels: map[string]string{
		labelProject: f.svc.ProjectID, labelService: f.svc.ID, labelDeployment: stale.ID,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.docker.Stop(ctx, active.ContainerID, 0); err != nil {
		t.Fatal(err)
	}
	f.d.docker = failedRemovalDocker{f.docker}

	if err := f.d.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if c, ok := f.docker.container(active.ContainerID); !ok || c.Running {
		t.Error("active container started while the candidate could not be removed")
	}
	if c, ok := f.docker.container(candidate); ok && c.Running {
		t.Error("interrupted candidate still running")
	}
	if err := f.d.StartService(ctx, f.svc.ID); err == nil || !strings.Contains(err.Error(), "may use its storage") {
		t.Errorf("start error = %v, want refusal", err)
	}

	f.d.docker = f.docker
	if err := f.d.StartService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	if c, ok := f.docker.container(active.ContainerID); !ok || !c.Running {
		t.Error("active container not started once the candidate was removed")
	}
	if _, ok := f.docker.container(candidate); ok {
		t.Error("interrupted candidate not removed")
	}
}

// ambiguousStartDocker starts containers but reports that the start failed,
// as when the connection to Docker drops mid-request, without returning the
// container's ID.
type ambiguousStartDocker struct{ *fakeDocker }

func (d ambiguousStartDocker) Run(ctx context.Context, spec docker.RunSpec) (string, error) {
	if _, err := d.fakeDocker.Run(ctx, spec); err != nil {
		return "", err
	}
	return "", errors.New("start container: connection reset")
}

// ambiguousStartStuckDocker is ambiguousStartDocker whose containers cannot
// be removed.
type ambiguousStartStuckDocker struct{ ambiguousStartDocker }

func (ambiguousStartStuckDocker) Remove(context.Context, string) error {
	return errors.New("daemon unavailable")
}

func TestAmbiguousStartFailureKeepsStorageExclusive(t *testing.T) {
	for _, tc := range []struct {
		name        string
		docker      func(*fakeDocker) Docker
		wantRestart bool
	}{
		{"removed", func(f *fakeDocker) Docker { return ambiguousStartDocker{f} }, true},
		{"stuck", func(f *fakeDocker) Docker { return ambiguousStartStuckDocker{ambiguousStartDocker{f}} }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if _, err := f.st.CreateVolume(context.Background(), f.svc.ID, "/data"); err != nil {
				t.Fatal(err)
			}
			old := f.wait(t, f.deploy(t).ID, terminal)
			f.settle(t)
			f.d.docker = tc.docker(f.docker)
			dep := f.wait(t, f.deploy(t).ID, terminal)
			f.settle(t)
			if dep.Status != store.StatusFailed {
				t.Fatalf("status = %s (%q), want failed", dep.Status, dep.Error)
			}

			containers, _ := f.docker.List(context.Background(), map[string]string{labelService: f.svc.ID})
			var running []string
			for _, c := range containers {
				if c.Running {
					running = append(running, c.ID)
				}
			}
			if tc.wantRestart {
				if len(running) != 1 || running[0] != old.ContainerID {
					t.Errorf("running = %v, want only the previous container %s", running, old.ContainerID)
				}
				return
			}
			if c, ok := f.docker.container(old.ContainerID); !ok || c.Running {
				t.Error("previous container restarted beside a replacement that may be running")
			}
			if !strings.Contains(dep.Error, "previous deployment left stopped") {
				t.Errorf("error = %q, want it to say the previous deployment is stopped", dep.Error)
			}
		})
	}
}
