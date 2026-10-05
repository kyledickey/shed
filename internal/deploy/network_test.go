package deploy

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/store"
)

func TestPrivateHostnameMovesOnlyWhenHealthy(t *testing.T) {
	f := newFixture(t)
	first := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)

	var mu sync.Mutex
	var seen [][]string // what "web" resolved to at each probe
	f.d.probe = func(context.Context, string, string) error {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, f.docker.resolve("web"))
		return nil
	}
	second := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if second.Status != store.StatusActive {
		t.Fatalf("status = %s (%q), want active", second.Status, second.Error)
	}

	mu.Lock()
	defer mu.Unlock()
	// The last probe rechecks the candidate once it has the hostname.
	if len(seen) < 2 {
		t.Fatalf("probed %d times, want the health check and a recheck", len(seen))
	}
	for _, got := range seen[:len(seen)-1] {
		if !slices.Equal(got, []string{first.ContainerID}) {
			t.Errorf("web resolved to %v during the health check, want only %s", got, first.ContainerID)
		}
	}
	if got := seen[len(seen)-1]; !slices.Contains(got, second.ContainerID) {
		t.Errorf("web resolved to %v at the recheck, want it to include %s", got, second.ContainerID)
	}
	if got := f.docker.resolve("web"); !slices.Equal(got, []string{second.ContainerID}) {
		t.Errorf("web resolves to %v after the switch, want only %s", got, second.ContainerID)
	}
}

func TestUnhealthyCandidateNeverGetsHostname(t *testing.T) {
	f := newFixture(t)
	first := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	f.healthy = func() bool { return false }
	if dep := f.wait(t, f.deploy(t).ID, terminal); dep.Status != store.StatusFailed {
		t.Fatalf("status = %s, want failed", dep.Status)
	}
	f.settle(t)
	if got := f.docker.resolve("web"); !slices.Equal(got, []string{first.ContainerID}) {
		t.Errorf("web resolves to %v, want only %s", got, first.ContainerID)
	}
}

// movingAddressDocker gives containers a new address when they are
// reconnected, as Docker would if the old one could not be kept.
type movingAddressDocker struct{ *fakeDocker }

func (d movingAddressDocker) ReconnectNetwork(ctx context.Context, network, id string, aliases []string) error {
	if err := d.fakeDocker.ReconnectNetwork(ctx, network, id, aliases); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.containers[id].IPs[network] = "10.0.1.1"
	return nil
}

func TestCandidateUnreachableAfterPromotionFails(t *testing.T) {
	f := newFixture(t)
	first := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	f.proxy.mu.Lock()
	routes := slices.Clone(f.proxy.routes)
	f.proxy.mu.Unlock()
	f.d.recheckTimeout = 50 * time.Millisecond

	// The new process listens only on the address it started with.
	f.d.docker = movingAddressDocker{f.docker}
	f.d.probe = func(_ context.Context, addr, _ string) error {
		if strings.HasPrefix(addr, "10.0.1.1:") {
			return errors.New("connection refused")
		}
		return nil
	}
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if dep.Status != store.StatusFailed || !strings.Contains(dep.Error, "after adding private hostname") {
		t.Fatalf("status = %s (%q), want failed after promotion", dep.Status, dep.Error)
	}
	if active, err := f.st.ActiveDeployment(context.Background(), f.svc.ID); err != nil || active.ID != first.ID {
		t.Errorf("active = %s, %v; want %s", active.ID, err, first.ID)
	}
	if got := f.docker.resolve("web"); !slices.Equal(got, []string{first.ContainerID}) {
		t.Errorf("web resolves to %v, want only %s", got, first.ContainerID)
	}
	if c, ok := f.docker.container(first.ContainerID); !ok || !c.Running {
		t.Error("previous container not running")
	}
	f.proxy.mu.Lock()
	defer f.proxy.mu.Unlock()
	if !slices.Equal(f.proxy.routes, routes) {
		t.Errorf("routes = %+v, want unchanged %+v", f.proxy.routes, routes)
	}
}
