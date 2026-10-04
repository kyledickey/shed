package api

import (
	"sync"
	"time"
)

// webhookGuard bounds concurrent bodies and admission rate across connections.
// A token bucket admits a burst of eight requests and refills once per second.
type webhookGuard struct {
	mu      sync.Mutex
	active  int
	tokens  float64
	updated time.Time
}

func (g *webhookGuard) acquire(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.updated.IsZero() {
		g.tokens = 8
	} else {
		g.tokens = min(8, g.tokens+now.Sub(g.updated).Seconds())
	}
	g.updated = now
	if g.active >= 4 || g.tokens < 1 {
		return false
	}
	g.active++
	g.tokens--
	return true
}
func (g *webhookGuard) release() { g.mu.Lock(); g.active--; g.mu.Unlock() }
