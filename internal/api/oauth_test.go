package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/kyledickey/shed/internal/auth"
)

// TestOAuthEndToEnd runs the OAuth flow through the real routes and store.
func TestOAuthEndToEnd(t *testing.T) {
	f := newFixture(t)
	serve := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		return rec
	}
	form := func(path string, v url.Values) map[string]any {
		t.Helper()
		req := httptest.NewRequest("POST", path, strings.NewReader(v.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "https://claude.ai") // cross-origin by nature
		rec := serve(req)
		var m map[string]any
		json.Unmarshal(rec.Body.Bytes(), &m)
		if m == nil {
			m = map[string]any{}
		}
		m["status"] = float64(rec.Code)
		return m
	}

	meta := f.decode(serve(httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil)), http.StatusOK)
	if meta["token_endpoint"] != "http://localhost/oauth/token" {
		t.Fatalf("metadata = %v", meta)
	}

	req := httptest.NewRequest("POST", "/oauth/register",
		strings.NewReader(`{"client_name":"agent","redirect_uris":["http://127.0.0.1/cb"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://claude.ai")
	clientID, _ := f.decode(serve(req), http.StatusCreated)["client_id"].(string)

	verifier := strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {"http://127.0.0.1:8123/cb"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"state": {"s1"}, "resource": {"http://localhost/mcp"},
	}
	rec := f.do("GET", "/oauth/authorize?"+q.Encode(), "")
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusFound || loc.Path != "/authorize" {
		t.Fatalf("authorize = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	id := loc.Query().Get("request")
	if r := f.decode(f.do("GET", "/api/oauth/requests/"+id, ""), http.StatusOK); r["clientName"] != "agent" {
		t.Errorf("request = %v", r)
	}
	redirect, _ := f.decode(f.do("POST", "/api/oauth/requests/"+id, `{"approve":true}`), http.StatusOK)["redirect"].(string)
	back, _ := url.Parse(redirect)

	tok := form("/oauth/token", url.Values{
		"grant_type": {"authorization_code"}, "code": {back.Query().Get("code")}, "client_id": {clientID},
		"redirect_uri": {"http://127.0.0.1:8123/cb"}, "code_verifier": {verifier},
	})
	access, _ := tok["access_token"].(string)
	refresh, _ := tok["refresh_token"].(string)
	if tok["status"] != 200.0 || access == "" || refresh == "" {
		t.Fatalf("token = %v", tok)
	}

	protected := f.server.auth.RequireBearer(f.server.auth.Resource())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := auth.UserFrom(r.Context())
		w.Write([]byte(u.Login))
	}))
	call := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/mcp", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		protected.ServeHTTP(rec, req)
		return rec
	}
	if rec := call(access); rec.Code != http.StatusOK || rec.Body.String() != "octocat" {
		t.Fatalf("bearer = %d %q", rec.Code, rec.Body)
	}

	next := form("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {clientID}})
	if next["status"] != 200.0 || next["refresh_token"] == refresh {
		t.Fatalf("refresh = %v", next)
	}
	grants := f.do("GET", "/api/oauth/grants", "")
	var list []map[string]any
	json.Unmarshal(grants.Body.Bytes(), &list)
	if len(list) != 1 || list[0]["redirectHost"] != "127.0.0.1:8123" || list[0]["lastUsedAt"] == nil {
		t.Fatalf("grants = %s", grants.Body)
	}

	reuse := form("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {clientID}})
	if reuse["error"] != "invalid_grant" {
		t.Fatalf("reuse = %v", reuse)
	}
	if rec := call(next["access_token"].(string)); rec.Code != http.StatusUnauthorized {
		t.Errorf("token after reuse = %d", rec.Code)
	}
	if rec := f.do("GET", "/api/oauth/grants", ""); strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("grants after reuse = %s", rec.Body)
	}

	if rec := f.do("DELETE", "/api/oauth/grants/nope", ""); rec.Code != http.StatusNotFound {
		t.Errorf("delete unknown grant = %d", rec.Code)
	}
	if r := form("/oauth/revoke", url.Values{"token": {"unknown"}}); r["status"] != 200.0 {
		t.Errorf("revoke = %v", r)
	}

	// Approving needs the dashboard's origin.
	req = httptest.NewRequest("POST", "/api/oauth/requests/"+id, strings.NewReader(`{"approve":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example.com")
	req.AddCookie(f.cookie)
	if rec := serve(req); rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin approval = %d", rec.Code)
	}
}
