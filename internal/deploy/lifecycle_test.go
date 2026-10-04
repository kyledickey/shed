package deploy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/docker"
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
	if c, ok := f.docker.container(dep.ContainerID); !ok || !c.Running {
		t.Fatal("test did not retain running candidate")
	}
}
