package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPRequiresBearer(t *testing.T) {
	f := newFixture(t)
	const metadata = `resource_metadata="http://localhost/.well-known/oauth-protected-resource/mcp"`
	tests := []struct {
		name      string
		header    map[string]string
		cookie    bool
		status    int
		challenge string // substring of WWW-Authenticate
	}{
		{"no token", nil, false, http.StatusUnauthorized, metadata},
		{"session cookie only", nil, true, http.StatusUnauthorized, metadata},
		{"unknown token", map[string]string{"Authorization": "Bearer shed_at_nope"}, false, http.StatusUnauthorized, `error="invalid_token"`},
		{"foreign origin", map[string]string{"Origin": "https://evil.example.com", "Authorization": "Bearer shed_at_nope"}, false, http.StatusForbidden, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			for k, v := range tt.header {
				req.Header.Set(k, v)
			}
			if tt.cookie {
				req.AddCookie(f.cookie)
			}
			rec := httptest.NewRecorder()
			f.handler.ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, tt.status, rec.Body)
			}
			if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(got, tt.challenge) {
				t.Errorf("WWW-Authenticate = %q, want it to contain %q", got, tt.challenge)
			}
		})
	}
}
