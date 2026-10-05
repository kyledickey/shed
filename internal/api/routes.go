package api

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// routeTimeout bounds one attempt to apply routes.
const routeTimeout = 30 * time.Second

// routesPendingHeader is set, to "pending", on the response to a domain
// change that is stored but not yet routed.
const routesPendingHeader = "Shed-Routes"

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
func (s *Server) applyRoutes(ctx context.Context) error {
	n := s.routes.begin()
	ctx, cancel := context.WithTimeout(ctx, routeTimeout)
	defer cancel()
	err := s.deployer.ApplyRoutes(ctx)
	s.routes.end(n, err)
	return err
}

// applyDomainRoutes applies routes after a domain change was stored, and
// marks the response pending if that failed.
func (s *Server) applyDomainRoutes(ctx context.Context, w http.ResponseWriter) {
	if err := s.applyRoutes(ctx); err != nil {
		s.log.Error("apply routes; retrying in the background", "err", err)
		w.Header().Set(routesPendingHeader, "pending")
	}
}

// SyncRoutes applies routes again after a domain change failed to, with
// backoff, until it succeeds. It returns when ctx is done.
func (s *Server) SyncRoutes(ctx context.Context) {
	delay := s.routes.retryMin
	for {
		if s.routes.isPending() {
			err := s.applyRoutes(ctx)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				s.log.Warn("apply routes failed; retrying", "in", delay, "err", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
				delay = min(2*delay, s.routes.retryMax)
				continue
			}
			s.log.Info("routes applied after an earlier failure")
		}
		delay = s.routes.retryMin
		select {
		case <-ctx.Done():
			return
		case <-s.routes.wake:
		}
	}
}
