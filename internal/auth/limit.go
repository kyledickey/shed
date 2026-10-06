package auth

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// limiter is a token bucket per key: each key may spend burst tokens at once,
// and gets them back at burst per period.
type limiter struct {
	burst   float64
	period  time.Duration
	maxKeys int

	mu      sync.Mutex
	buckets map[string]bucket
}

type bucket struct {
	tokens float64
	at     time.Time
}

func newLimiter(burst int, period time.Duration, maxKeys int) *limiter {
	return &limiter{
		burst:   float64(burst),
		period:  period,
		maxKeys: maxKeys,
		buckets: make(map[string]bucket),
	}
}

// allow spends a token of key at now, reporting whether one was left.
func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= l.maxKeys {
			// Forget keys whose buckets have refilled; they behave as new.
			for k, b := range l.buckets {
				if l.refill(b, now) >= l.burst {
					delete(l.buckets, k)
				}
			}
			if len(l.buckets) >= l.maxKeys {
				return false
			}
		}
		b = bucket{tokens: l.burst, at: now}
	}
	tokens := l.refill(b, now)
	if tokens < 1 {
		return false
	}
	l.buckets[key] = bucket{tokens: tokens - 1, at: now}
	return true
}

func (l *limiter) refill(b bucket, now time.Time) float64 {
	return min(l.burst, b.tokens+now.Sub(b.at).Seconds()*l.burst/l.period.Seconds())
}

// clientAddr returns the address a request came from, for rate limiting.
// Behind the embedded proxy the connection is from loopback and the proxy
// sets X-Forwarded-For, overwriting what the client sent, so its last entry
// is trusted then. IPv6 addresses are reduced to their /64, which a single
// host usually controls in full.
func clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
			last := xff[len(xff)-1]
			last = strings.TrimSpace(last[strings.LastIndexByte(last, ',')+1:])
			if fwd := net.ParseIP(last); fwd != nil {
				ip = fwd
			}
		}
	}
	switch {
	case ip == nil:
		return host
	case ip.To4() != nil:
		return ip.String()
	default:
		return ip.Mask(net.CIDRMask(64, 128)).String()
	}
}
