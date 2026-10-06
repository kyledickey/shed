package control

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDomainRoutesRetried(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	f.plane.routes.retryMin, f.plane.routes.retryMax = time.Millisecond, 4*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.plane.SyncRoutes(ctx)
	}()
	defer func() {
		cancel()
		<-done
	}()

	// The first apply and two retries fail; the third retry succeeds without
	// another request.
	f.deployer.mu.Lock()
	f.deployer.routeFails = 3
	f.deployer.mu.Unlock()
	d, pending, err := f.plane.CreateDomain(context.Background(), svc.ID, "app.example.org")
	if err != nil || !pending {
		t.Fatalf("CreateDomain() = %v, pending %v; want pending", err, pending)
	}
	deadline := time.Now().Add(5 * time.Second)
	for f.plane.routes.isPending() {
		if time.Now().After(deadline) {
			t.Fatal("routes still pending")
		}
		time.Sleep(time.Millisecond)
	}
	if calls, fails := f.deployer.routes(); calls != 4 || fails != 0 {
		t.Errorf("ApplyRoutes calls = %d with %d failures left, want 4 and 0", calls, fails)
	}

	// Once applied, nothing reapplies routes until the next change.
	time.Sleep(20 * time.Millisecond)
	if calls, _ := f.deployer.routes(); calls != 4 {
		t.Errorf("ApplyRoutes calls = %d while idle, want 4", calls)
	}
	if pending, err := f.plane.DeleteDomain(context.Background(), d.ID); err != nil || pending {
		t.Errorf("DeleteDomain() = %v, pending %v; want applied", err, pending)
	}
	if calls, _ := f.deployer.routes(); calls != 5 {
		t.Errorf("ApplyRoutes calls = %d, want 5", calls)
	}
}

func TestSyncRoutesStopsWhilePending(t *testing.T) {
	f := newFixture(t)
	f.plane.routes.retryMin, f.plane.routes.retryMax = time.Hour, time.Hour
	f.plane.routes.end(f.plane.routes.begin(), errors.New("boom"))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.plane.SyncRoutes(ctx)
	}()
	// SyncRoutes applies once, succeeds, and then waits for a change.
	deadline := time.Now().Add(5 * time.Second)
	for f.plane.routes.isPending() {
		if time.Now().After(deadline) {
			t.Fatal("routes still pending")
		}
		time.Sleep(time.Millisecond)
	}
	f.deployer.mu.Lock()
	f.deployer.routeFails = 1
	f.deployer.mu.Unlock()
	if err := f.plane.applyRoutes(context.Background()); err == nil {
		t.Fatal("applyRoutes succeeded")
	}
	// Now SyncRoutes fails again and waits an hour; cancellation ends it.
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SyncRoutes did not return after cancellation")
	}
}

func TestRouteStateKeepsNewestResult(t *testing.T) {
	var r routeState
	r.wake = make(chan struct{}, 1)
	older, newer := r.begin(), r.begin()
	r.end(newer, errors.New("boom"))
	r.end(older, nil) // read older domains, so it cannot clear the failure
	if !r.isPending() {
		t.Error("an older success cleared a newer failure")
	}
	r.end(r.begin(), nil)
	if r.isPending() {
		t.Error("a newer success left routes pending")
	}
}
