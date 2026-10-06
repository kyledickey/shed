package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	start := time.Now()
	l := newLimiter(2, time.Minute, 2)
	steps := []struct {
		key   string
		after time.Duration
		want  bool
	}{
		{"a", 0, true},
		{"a", 0, true},
		{"a", 0, false},               // burst spent
		{"b", 0, true},                // keys are independent
		{"c", 0, false},               // no room for another key
		{"a", 30 * time.Second, true}, // one token back
		{"a", 30 * time.Second, false},
		{"c", 2 * time.Minute, true}, // a and b refilled and were forgotten
	}
	for i, s := range steps {
		if got := l.allow(s.key, start.Add(s.after)); got != s.want {
			t.Errorf("step %d: allow(%q, +%s) = %t, want %t", i, s.key, s.after, got, s.want)
		}
	}
}

func TestClientAddr(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		xff    []string
		want   string
	}{
		{"direct", "203.0.113.7:5000", nil, "203.0.113.7"},
		{"direct ignores forwarded", "203.0.113.7:5000", []string{"198.51.100.1"}, "203.0.113.7"},
		{"behind proxy", "127.0.0.1:5000", []string{"198.51.100.1"}, "198.51.100.1"},
		{"behind proxy, last entry", "127.0.0.1:5000", []string{"10.0.0.1, 198.51.100.1"}, "198.51.100.1"},
		{"behind proxy, last header", "[::1]:5000", []string{"10.0.0.1", "198.51.100.1"}, "198.51.100.1"},
		{"behind proxy, garbage", "127.0.0.1:5000", []string{"nope"}, "127.0.0.1"},
		{"ipv6 by /64", "[2001:db8:1:2:3:4:5:6]:5000", nil, "2001:db8:1:2::"},
		{"forwarded ipv6 by /64", "127.0.0.1:5000", []string{"2001:db8:1:2::9"}, "2001:db8:1:2::"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remote
			for _, v := range tt.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := clientAddr(r); got != tt.want {
				t.Errorf("clientAddr = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRegisterRateLimit(t *testing.T) {
	e := newOAuthEnv(t)
	register := func(from string) int {
		req := httptest.NewRequest("POST", "/oauth/register", strings.NewReader(`{"redirect_uris":["https://a/cb"]}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "127.0.0.1:4000"
		req.Header.Set("X-Forwarded-For", from)
		return e.serve(req).Code
	}
	for i := range registerBurst {
		if code := register("198.51.100.1"); code != http.StatusCreated {
			t.Fatalf("registration %d = %d", i, code)
		}
	}
	if code := register("198.51.100.1"); code != http.StatusTooManyRequests {
		t.Errorf("registration past burst = %d, want 429", code)
	}
	if code := register("198.51.100.2"); code != http.StatusCreated {
		t.Errorf("registration from another address = %d", code)
	}
}
