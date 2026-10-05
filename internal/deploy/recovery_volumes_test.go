package deploy

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/store"
)

// VolumeExists reports every volume as present, so that tests that don't
// track volumes behave as before.
func (f *fakeDocker) VolumeExists(context.Context, string) (bool, error) { return true, nil }

// volumeDocker is a fakeDocker that tracks which volumes exist.
type volumeDocker struct {
	*fakeDocker
	mu         sync.Mutex
	volumes    map[string]bool
	inspectErr error    // if set, returned by VolumeExists
	checked    []string // volumes VolumeExists was asked about
}

func (v *volumeDocker) EnsureVolume(ctx context.Context, name string) error {
	v.mu.Lock()
	v.volumes[name] = true
	v.mu.Unlock()
	return v.fakeDocker.EnsureVolume(ctx, name)
}

func (v *volumeDocker) RemoveVolume(ctx context.Context, name string) error {
	v.mu.Lock()
	delete(v.volumes, name)
	v.mu.Unlock()
	return v.fakeDocker.RemoveVolume(ctx, name)
}

func (v *volumeDocker) VolumeExists(_ context.Context, name string) (bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.checked = append(v.checked, name)
	if v.inspectErr != nil {
		return false, v.inspectErr
	}
	return v.volumes[name], nil
}

// lose removes a volume behind shed's back, as `docker volume rm` or a new
// host does.
func (v *volumeDocker) lose(name string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.volumes, name)
}

func (v *volumeDocker) has(name string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.volumes[name]
}

func (v *volumeDocker) wasChecked(name string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return slices.Contains(v.checked, name)
}

// volumeFixture returns a fixture whose Docker tracks volumes, with an
// active deployment that mounts a volume at /data.
func volumeFixture(t *testing.T) (*fixture, *volumeDocker, store.Volume, store.Deployment) {
	t.Helper()
	f := newFixture(t)
	vd := &volumeDocker{fakeDocker: f.docker, volumes: make(map[string]bool)}
	f.d.docker = vd // Nothing runs yet.
	vol, err := f.st.CreateVolume(context.Background(), f.svc.ID, "/data")
	if err != nil {
		t.Fatal(err)
	}
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if dep.Status != store.StatusActive {
		t.Fatalf("status = %s (%s)", dep.Status, dep.Error)
	}
	if !vd.has(volumeName(vol.ID)) {
		t.Fatal("the first deployment did not create its volume")
	}
	if vd.wasChecked(volumeName(vol.ID)) {
		t.Fatal("a deployment checked its volume instead of creating it")
	}
	return f, vd, vol, dep
}

// counts returns how many containers were run and volumes created.
func (f *fixture) counts() (runs, volumes int) {
	f.docker.mu.Lock()
	defer f.docker.mu.Unlock()
	return len(f.docker.runs), len(f.docker.ensuredVolumes)
}

// requireNothingCreated fails unless no container was run and no volume
// created since counts returned runs and volumes.
func (f *fixture) requireNothingCreated(t *testing.T, runs, volumes int) {
	t.Helper()
	if r, v := f.counts(); r != runs || v != volumes {
		t.Fatalf("ran %d containers and created %d volumes, want none", r-runs, v-volumes)
	}
}

func TestRecoveryRefusesMissingVolume(t *testing.T) {
	f, vd, vol, dep := volumeFixture(t)
	ctx := context.Background()
	if err := f.docker.Remove(ctx, dep.ContainerID); err != nil {
		t.Fatal(err)
	}
	vd.lose(volumeName(vol.ID))
	runs, volumes := f.counts()

	if err := f.d.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	f.requireNothingCreated(t, runs, volumes)
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
	if err := f.d.StartService(ctx, f.svc.ID); !errors.Is(err, ErrVolumeMissing) {
		t.Fatalf("start error = %v, want ErrVolumeMissing", err)
	}
	if svc, _ := f.st.Service(ctx, f.svc.ID); !svc.Stopped {
		t.Error("a failed start left the service marked running")
	}
	f.requireNothingCreated(t, runs, volumes)

	// Releasing a hold, as a backup or restore does, starts nothing either.
	if err := f.st.SetServiceStopped(ctx, f.svc.ID, false); err != nil {
		t.Fatal(err)
	}
	held, err := f.d.Hold(ctx, f.svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := held.Release(ctx); !errors.Is(err, ErrVolumeMissing) {
		t.Fatalf("release error = %v, want ErrVolumeMissing", err)
	}
	f.requireNothingCreated(t, runs, volumes)

	// A new deployment is the explicit way to start over with an empty volume.
	next := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if next.Status != store.StatusActive {
		t.Fatalf("redeploy status = %s (%s)", next.Status, next.Error)
	}
	if !vd.has(volumeName(vol.ID)) {
		t.Error("a new deployment did not create the volume")
	}
}

func TestRecoveryVolumeInspectError(t *testing.T) {
	f, vd, _, dep := volumeFixture(t)
	ctx := context.Background()
	if err := f.docker.Remove(ctx, dep.ContainerID); err != nil {
		t.Fatal(err)
	}
	daemon := errors.New("daemon unavailable")
	vd.mu.Lock()
	vd.inspectErr = daemon
	vd.mu.Unlock()
	runs, volumes := f.counts()

	if err := f.d.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	f.requireNothingCreated(t, runs, volumes)

	if err := f.d.StopService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	err := f.d.StartService(ctx, f.svc.ID)
	if !errors.Is(err, daemon) || errors.Is(err, ErrVolumeMissing) {
		t.Fatalf("start error = %v, want the inspect error and not ErrVolumeMissing", err)
	}
	f.requireNothingCreated(t, runs, volumes)
}

// TestRecoveryVolumeChanges tells volumes deleted on purpose, which recovery
// leaves out, from deployed volumes that are lost, which it refuses to
// recreate empty.
func TestRecoveryVolumeChanges(t *testing.T) {
	tests := []struct {
		name        string
		loseDeleted bool // the deleted volume's data was already gone
		loseKept    bool
		wantErr     error
	}{
		{name: "deleted volume is left out", wantErr: nil},
		{name: "deleted volume already gone", loseDeleted: true, wantErr: nil},
		{name: "lost volume next to a deleted one", loseKept: true, wantErr: ErrVolumeMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, vd, kept, _ := volumeFixture(t)
			ctx := context.Background()
			// Deploy again with a second volume, then delete it.
			deleted, err := f.st.CreateVolume(ctx, f.svc.ID, "/cache")
			if err != nil {
				t.Fatal(err)
			}
			dep := f.wait(t, f.deploy(t).ID, terminal)
			f.settle(t)
			if dep.Status != store.StatusActive {
				t.Fatalf("status = %s (%s)", dep.Status, dep.Error)
			}
			if tt.loseDeleted {
				vd.lose(volumeName(deleted.ID))
			}
			if err := f.d.DeleteVolume(ctx, deleted.ID); err != nil {
				t.Fatal(err)
			}
			// Added since the deployment: not mounted, so not required.
			added, err := f.st.CreateVolume(ctx, f.svc.ID, "/new")
			if err != nil {
				t.Fatal(err)
			}
			if tt.loseKept {
				vd.lose(volumeName(kept.ID))
			}
			if err := f.docker.Remove(ctx, dep.ContainerID); err != nil {
				t.Fatal(err)
			}
			if err := f.d.StopService(ctx, f.svc.ID); err != nil {
				t.Fatal(err)
			}
			runs, volumes := f.counts()

			err = f.d.StartService(ctx, f.svc.ID)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("start error = %v, want %v", err, tt.wantErr)
				}
				f.requireNothingCreated(t, runs, volumes)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := []docker.Mount{{Volume: volumeName(kept.ID), Target: "/data"}}
			if got := f.docker.lastRun(t); !slices.Equal(got.Mounts, want) {
				t.Errorf("mounts = %+v, want %+v", got.Mounts, want)
			}
			for _, name := range []string{volumeName(deleted.ID), volumeName(added.ID)} {
				if vd.wasChecked(name) {
					t.Errorf("volume %s was required", name)
				}
				if vd.has(name) {
					t.Errorf("volume %s was created", name)
				}
			}
		})
	}
}

func TestRecoveryWithoutRecordedRuntimeRefusesMissingVolume(t *testing.T) {
	f, vd, vol, dep := volumeFixture(t)
	ctx := context.Background()
	// A deployment activated before runtimes were recorded mounts the
	// service's current volumes.
	dep.Runtime = nil
	if err := f.st.ActivateDeployment(ctx, dep); err != nil {
		t.Fatal(err)
	}
	if err := f.docker.Remove(ctx, dep.ContainerID); err != nil {
		t.Fatal(err)
	}
	vd.lose(volumeName(vol.ID))
	if err := f.d.StopService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	runs, volumes := f.counts()
	if err := f.d.StartService(ctx, f.svc.ID); !errors.Is(err, ErrVolumeMissing) {
		t.Fatalf("start error = %v, want ErrVolumeMissing", err)
	}
	f.requireNothingCreated(t, runs, volumes)
}
