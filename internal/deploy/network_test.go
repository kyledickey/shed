package deploy

import (
	"context"
	"slices"
	"sync"
	"testing"

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
	if len(seen) == 0 {
		t.Fatal("never probed")
	}
	for _, got := range seen {
		if !slices.Equal(got, []string{first.ContainerID}) {
			t.Errorf("web resolved to %v during the health check, want only %s", got, first.ContainerID)
		}
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
