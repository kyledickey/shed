package auth

import (
	"errors"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/attribute"

	"github.com/kyledickey/shed/internal/github"
	"github.com/kyledickey/shed/internal/telemetry/telemetrytest"
)

func TestSignInsAreCountedByOutcome(t *testing.T) {
	rec := telemetrytest.Install(t)
	user := github.User{ID: 1, Login: "a"}
	signIn(t, newAuth(newMemStore(), fakeOAuth{user: user}, "http://x", "a"), "code")
	signIn(t, newAuth(newMemStore(), fakeOAuth{user: user}, "http://x", "someone-else"), "code")
	signIn(t, newAuth(newMemStore(), fakeOAuth{err: errors.New("boom")}, "http://x", "a"), "code")
	a := newAuth(newMemStore(), fakeOAuth{user: user}, "http://x", "a")
	a.Callback(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/auth/callback?code=c&state=s", nil))

	for outcome, want := range map[string]int64{"ok": 1, "denied": 1, "failed": 1, "invalid_state": 1} {
		if n := rec.Sum(t, "shed_signins_total", attribute.String("outcome", outcome)); n != want {
			t.Errorf("shed_signins_total{outcome=%q} = %d, want %d", outcome, n, want)
		}
	}
}
