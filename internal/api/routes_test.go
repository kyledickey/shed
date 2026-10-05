package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func (f *fakeDeployer) routes() (calls, fails int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.routeCalls, f.routeFails
}

func TestDomainRoutesRetried(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	f.server.routes.retryMin, f.server.routes.retryMax = time.Millisecond, 4*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.server.SyncRoutes(ctx)
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
	rec := f.do("POST", "/api/services/"+svc.ID+"/domains", `{"host":"app.example.org"}`)
	f.decode(rec, http.StatusCreated)
	if got := rec.Header().Get(routesPendingHeader); got != "pending" {
		t.Errorf("%s = %q, want pending", routesPendingHeader, got)
	}
	deadline := time.Now().Add(5 * time.Second)
	for f.server.routes.isPending() {
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
	rec = f.do("DELETE", "/api/domains/"+domainID(t, f, svc.ID), "")
	if rec.Code != http.StatusNoContent || rec.Header().Get(routesPendingHeader) != "" {
		t.Errorf("delete: status = %d, %s = %q; want 204 and no header",
			rec.Code, routesPendingHeader, rec.Header().Get(routesPendingHeader))
	}
	if calls, _ := f.deployer.routes(); calls != 5 {
		t.Errorf("ApplyRoutes calls = %d, want 5", calls)
	}
}

func TestSyncRoutesStopsWhilePending(t *testing.T) {
	f := newFixture(t)
	f.server.routes.retryMin, f.server.routes.retryMax = time.Hour, time.Hour
	f.server.routes.end(f.server.routes.begin(), errors.New("boom"))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.server.SyncRoutes(ctx)
	}()
	// SyncRoutes applies once, succeeds, and then waits for a change.
	deadline := time.Now().Add(5 * time.Second)
	for f.server.routes.isPending() {
		if time.Now().After(deadline) {
			t.Fatal("routes still pending")
		}
		time.Sleep(time.Millisecond)
	}
	f.deployer.mu.Lock()
	f.deployer.routeFails = 1
	f.deployer.mu.Unlock()
	if err := f.server.applyRoutes(context.Background()); err == nil {
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

func domainID(t *testing.T, f *fixture, serviceID string) string {
	t.Helper()
	ds, err := f.st.Domains(context.Background(), serviceID)
	if err != nil || len(ds) != 1 {
		t.Fatalf("Domains() = %v, %v", ds, err)
	}
	return ds[0].ID
}
