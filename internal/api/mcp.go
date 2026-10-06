package api

import (
	"net/http"
	"net/url"
	"slices"

	"github.com/kyledickey/shed/internal/auth"
)

// rejectForeignOrigins refuses requests from web pages on other origins
// than the dashboard. Requests without an Origin header, which agents send,
// pass.
func rejectForeignOrigins(baseURL string, next http.Handler) http.Handler {
	u, _ := url.Parse(baseURL)
	origin := u.Scheme + "://" + u.Host
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Origin"); got != "" && got != origin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "untrusted request origin"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireScope refuses requests whose access token, accepted by
// auth.RequireBearer, lacks scope.
func requireScope(scope string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !slices.Contains(auth.ScopesFrom(r.Context()), scope) {
			w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="`+scope+`"`)
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient scope"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
