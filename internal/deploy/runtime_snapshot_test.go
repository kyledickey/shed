package deploy

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/store"
)

// lastRun returns the spec of the most recently run container.
func (f *fakeDocker) lastRun(t *testing.T) docker.RunSpec {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.runs) == 0 {
		t.Fatal("no container was run")
	}
	return f.runs[len(f.runs)-1]
}

func TestRecreateUsesDeployedRuntime(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	kept, err := f.st.CreateVolume(ctx, f.svc.ID, "/data")
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := f.st.CreateVolume(ctx, f.svc.ID, "/cache")
	if err != nil {
		t.Fatal(err)
	}
	f.svc.StartCommand, f.svc.CPULimit, f.svc.MemoryLimit, f.svc.PublicPort = "serve", 0.5, 256<<20, 9000
	if err := f.st.UpdateService(ctx, f.svc); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetVariables(ctx, f.svc.ID, map[string]string{"API_SECRET": "deployed"}); err != nil {
		t.Fatal(err)
	}
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if dep.Status != store.StatusActive {
		t.Fatalf("status = %s (%s)", dep.Status, dep.Error)
	}
	deployed := f.docker.lastRun(t)

	// Saved but not deployed: a broken command, new variables and limits, a
	// new volume, and one of the deployed volumes deleted.
	f.svc.StartCommand, f.svc.CPULimit, f.svc.MemoryLimit, f.svc.PublicPort = "exit 1", 2, 1<<30, 9001
	if err := f.st.UpdateService(ctx, f.svc); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetVariables(ctx, f.svc.ID, map[string]string{"API_SECRET": "saved", "EXTRA": "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.CreateVolume(ctx, f.svc.ID, "/new"); err != nil {
		t.Fatal(err)
	}
	if err := f.d.DeleteVolume(ctx, deleted.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.docker.Remove(ctx, dep.ContainerID); err != nil {
		t.Fatal(err)
	}

	if err := f.d.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	got := f.docker.lastRun(t)
	if got.Name != deployed.Name || got.Image != dep.Image {
		t.Fatalf("recreated %s from %s, want %s from %s", got.Name, got.Image, deployed.Name, dep.Image)
	}
	if !slices.Equal(got.Cmd, []string{"sh", "-c", "serve"}) {
		t.Errorf("cmd = %q, want the deployed start command", got.Cmd)
	}
	if !slices.Equal(got.Env, deployed.Env) {
		t.Errorf("env = %q, want the deployed %q", got.Env, deployed.Env)
	}
	if got.CPUs != 0.5 || got.Memory != 256<<20 {
		t.Errorf("limits = %v CPUs, %d bytes; want the deployed 0.5, %d", got.CPUs, got.Memory, 256<<20)
	}
	if len(got.Publish) != 1 || got.Publish[0].HostPort != 9000 {
		t.Errorf("publish = %+v, want host port 9000", got.Publish)
	}
	want := []docker.Mount{{Volume: volumeName(kept.ID), Target: "/data"}}
	if !slices.Equal(got.Mounts, want) {
		t.Errorf("mounts = %+v, want only the deployed volume that still exists: %+v", got.Mounts, want)
	}
	if !slices.Contains(f.docker.removedVolumes, volumeName(deleted.ID)) {
		t.Errorf("deleted volume %s was not removed", volumeName(deleted.ID))
	}
	if c, ok := f.docker.container(f.activeContainer(t)); !ok || !c.Running {
		t.Error("recreated container is not running")
	}
}

func TestRecreateWithoutRecordedRuntimeUsesCurrentSettings(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	// A deployment activated before runtimes were recorded.
	dep.Runtime = nil
	if err := f.st.ActivateDeployment(ctx, dep); err != nil {
		t.Fatal(err)
	}
	f.svc.StartCommand = "serve --v2"
	if err := f.st.UpdateService(ctx, f.svc); err != nil {
		t.Fatal(err)
	}
	if err := f.docker.Remove(ctx, dep.ContainerID); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	got := f.docker.lastRun(t)
	if !slices.Equal(got.Cmd, []string{"sh", "-c", "serve --v2"}) || !slices.Contains(got.Env, "GREETING=hi from web") {
		t.Errorf("recreated with cmd %q, env %q; want the current settings", got.Cmd, got.Env)
	}
}

func TestRuntimeLogsMaskDeployedSecrets(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	const old, saved = "old-secret-review", "new-secret-saved"
	if err := f.st.SetVariables(ctx, f.svc.ID, map[string]string{"API_SECRET": old}); err != nil {
		t.Fatal(err)
	}
	f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	// The new value is saved but not deployed, so the container still has
	// the old one.
	if err := f.st.SetVariables(ctx, f.svc.ID, map[string]string{"API_SECRET": saved}); err != nil {
		t.Fatal(err)
	}
	f.docker.mu.Lock()
	f.docker.output = "token=" + old + "\n"
	f.docker.mu.Unlock()

	var out strings.Builder
	ctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := f.d.RuntimeLogs(ctx, f.svc.ID, 100, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), old) || !strings.Contains(out.String(), "token=***") {
		t.Fatalf("runtime log = %q, want the deployed secret masked", out.String())
	}
}

// activeContainer returns the container ID of the service's active
// deployment.
func (f *fixture) activeContainer(t *testing.T) string {
	t.Helper()
	dep, err := f.st.ActiveDeployment(context.Background(), f.svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	return dep.ContainerID
}
