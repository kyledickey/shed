package control

import (
	"context"
	"sync"
	"time"
)

// routeTimeout bounds one attempt to apply routes.
const routeTimeout = 30 * time.Second

// routeState records whether the proxy is behind the stored domains. It is
// safe for concurrent use.
type routeState struct {
	mu       sync.Mutex
	started  uint64 // attempts started
	recorded uint64 // the attempt whose result is recorded
	pending  bool   // that attempt failed
	wake     chan struct{}

	// Backoff between retries, replaced in tests.
	retryMin, retryMax time.Duration
}

func newRouteState() *routeState {
	return &routeState{wake: make(chan struct{}, 1), retryMin: 2 * time.Second, retryMax: time.Minute}
}

// begin numbers a new attempt.
func (r *routeState) begin() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started++
	return r.started
}

// end records the result of attempt n, unless a later attempt already
// recorded one: that attempt read newer domains. A failure wakes SyncRoutes.
func (r *routeState) end(n uint64, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n > r.recorded {
		r.recorded, r.pending = n, err != nil
	}
	if r.pending {
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}
}

func (r *routeState) isPending() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pending
}

// applyRoutes updates the proxy after a domain change. A failure does not
// undo the change: SyncRoutes retries until routes apply.
func (p *Plane) applyRoutes(ctx context.Context) error {
	n := p.routes.begin()
	ctx, cancel := context.WithTimeout(ctx, routeTimeout)
	defer cancel()
	err := p.deployer.ApplyRoutes(ctx)
	p.routes.end(n, err)
	return err
}

// applyDomainRoutes applies routes after a domain change was stored, and
// reports whether that succeeded.
func (p *Plane) applyDomainRoutes(ctx context.Context) bool {
	if err := p.applyRoutes(ctx); err != nil {
		p.log.Error("apply routes; retrying in the background", "err", err)
		return false
	}
	return true
}

// SyncRoutes applies routes again after a domain change failed to, with
// backoff, until it succeeds. It returns when ctx is done.
func (p *Plane) SyncRoutes(ctx context.Context) {
	delay := p.routes.retryMin
	for {
		if p.routes.isPending() {
			err := p.applyRoutes(ctx)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				p.log.Warn("apply routes failed; retrying", "in", delay, "err", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
				delay = min(2*delay, p.routes.retryMax)
				continue
			}
			p.log.Info("routes applied after an earlier failure")
		}
		delay = p.routes.retryMin
		select {
		case <-ctx.Done():
			return
		case <-p.routes.wake:
		}
	}
}
