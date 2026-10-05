package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/kyledickey/shed/internal/store"
)

// fenceAfterFailedRestore leaves the fixture's service as a restore that
// failed and could not put the previous data back does: held, its
// containers removed, fenced, and released.
func (f *fixture) fenceAfterFailedRestore(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	h := f.hold(t)
	if err := h.StopAndRemove(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.CreateRestoreFence(ctx, store.RestoreFence{
		ServiceID: f.svc.ID, RestoreID: "r1", Phase: store.RestoreReplacing, Image: "img",
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.Release(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestFencedServiceDoesNotRun(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	f.fenceAfterFailedRestore(t)

	tests := []struct {
		name string
		op   func() error
	}{
		{"push", func() error {
			_, err := f.d.Deploy(ctx, f.svc.ID, store.TriggerPush, Commit{SHA: "next"})
			return err
		}},
		{"manual deploy", func() error {
			_, err := f.d.Deploy(ctx, f.svc.ID, store.TriggerManual, Commit{SHA: "next"})
			return err
		}},
		{"rollback", func() error { _, err := f.d.Redeploy(ctx, dep.ID); return err }},
		{"start", func() error { return f.d.StartService(ctx, f.svc.ID) }},
		{"restart", func() error { return f.d.RestartService(ctx, f.svc.ID) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.op(); !errors.Is(err, ErrFenced) {
				t.Errorf("err = %v, want ErrFenced", err)
			}
		})
	}
	if deps, err := f.st.Deployments(ctx, f.svc.ID, 0); err != nil || len(deps) != 1 {
		t.Errorf("deployments = %d (%v), want 1", len(deps), err)
	}
	if !f.stopped(t) {
		t.Error("fenced service is not stopped")
	}

	// Even if the service is not marked stopped, as an older shed allowed,
	// Reconcile does not recreate its container.
	if err := f.st.SetServiceStopped(ctx, f.svc.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := f.d.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if cs := f.serviceContainers(t); len(cs) != 0 {
		t.Errorf("reconcile ran %d containers of a fenced service", len(cs))
	}
	if err := f.st.SetServiceStopped(ctx, f.svc.ID, true); err != nil {
		t.Fatal(err)
	}

	// Clearing the fence keeps the service stopped, and it can then start.
	if err := f.d.ClearRestoreFence(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.RestoreFence(ctx, f.svc.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("fence after clearing: err = %v, want ErrNotFound", err)
	}
	if !f.stopped(t) {
		t.Error("clearing the fence started the service")
	}
	if err := f.d.StartService(ctx, f.svc.ID); err != nil {
		t.Fatalf("start after clearing: %v", err)
	}
	if cs := f.serviceContainers(t); len(cs) != 1 || !cs[0].Running {
		t.Errorf("containers after start = %+v, want one running", cs)
	}
}

func TestClearRestoreFence(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)

	if err := f.d.ClearRestoreFence(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown service: err = %v, want ErrNotFound", err)
	}
	if err := f.d.ClearRestoreFence(ctx, f.svc.ID); err != nil {
		t.Errorf("service without a fence: %v", err)
	}

	// A restore in progress holds the service and fences it: its fence
	// cannot be cleared.
	h := f.hold(t)
	if _, err := f.st.CreateRestoreFence(ctx, store.RestoreFence{
		ServiceID: f.svc.ID, RestoreID: "r1", Phase: store.RestoreLoading,
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.d.ClearRestoreFence(ctx, f.svc.ID); !errors.Is(err, ErrServiceBusy) {
		t.Errorf("held service: err = %v, want ErrServiceBusy", err)
	}
	if _, err := f.st.RestoreFence(ctx, f.svc.ID); err != nil {
		t.Errorf("fence of a held service: %v", err)
	}
	if err := h.Release(ctx); err != nil {
		t.Fatal(err)
	}
}
