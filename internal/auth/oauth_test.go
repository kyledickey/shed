package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/github"
)

// oauthMem is the OAuth half of memStore.
type oauthMem struct {
	clients map[string]Client
	codes   map[string]*memCode
	grants  map[string]Grant
	tokens  map[string]*memToken
	nextID  int
}

type memCode struct {
	Code
	grantID string
}

type memToken struct {
	Token
	grantID string
	rotated bool
}

func newOAuthMem() oauthMem {
	return oauthMem{clients: map[string]Client{}, codes: map[string]*memCode{}, grants: map[string]Grant{}, tokens: map[string]*memToken{}}
}

func (m *memStore) id() string {
	m.nextID++
	return fmt.Sprintf("id%d", m.nextID)
}

func (m *memStore) CreateClient(_ context.Context, c Client) (Client, error) {
	c.ID, c.CreatedAt = m.id(), time.Now()
	m.clients[c.ID] = c
	return c, nil
}

func (m *memStore) Client(_ context.Context, id string) (Client, bool, error) {
	c, ok := m.clients[id]
	return c, ok, nil
}

func (m *memStore) CountClients(context.Context) (int, error) { return len(m.clients), nil }

func (m *memStore) CreateCode(_ context.Context, c Code) error {
	m.codes[c.Hash] = &memCode{Code: c}
	return nil
}

func (m *memStore) deleteGrant(id string) {
	delete(m.grants, id)
	maps.DeleteFunc(m.tokens, func(_ string, t *memToken) bool { return t.grantID == id })
}

func (m *memStore) RedeemCode(_ context.Context, hash string, issue func(Code) (Grant, []Token, error)) (Grant, bool, error) {
	c, ok := m.codes[hash]
	if !ok || time.Now().After(c.ExpiresAt) {
		return Grant{}, false, nil
	}
	if c.grantID != "" {
		m.deleteGrant(c.grantID)
		return Grant{}, false, ErrReplayed
	}
	g, tokens, err := issue(c.Code)
	if err != nil {
		delete(m.codes, hash)
		return Grant{}, false, err
	}
	g.ID, g.CreatedAt, g.ClientName = m.id(), time.Now(), m.clients[g.ClientID].Name
	m.grants[g.ID] = g
	for _, t := range tokens {
		m.tokens[t.Hash] = &memToken{Token: t, grantID: g.ID}
	}
	c.grantID = g.ID
	return g, true, nil
}

func (m *memStore) RotateRefreshToken(_ context.Context, hash string, issue func(Grant, User) ([]Token, error)) (Grant, bool, error) {
	t, ok := m.tokens[hash]
	if !ok || !t.Refresh || time.Now().After(t.ExpiresAt) {
		return Grant{}, false, nil
	}
	if t.rotated {
		m.deleteGrant(t.grantID)
		return Grant{}, false, ErrReplayed
	}
	g := m.grants[t.grantID]
	tokens, err := issue(g, m.users[g.GitHubID])
	if err != nil {
		return Grant{}, false, err
	}
	t.rotated = true
	for _, nt := range tokens {
		m.tokens[nt.Hash] = &memToken{Token: nt, grantID: g.ID}
	}
	return g, true, nil
}

func (m *memStore) AccessToken(_ context.Context, hash string) (Grant, User, bool, error) {
	t, ok := m.tokens[hash]
	if !ok || t.Refresh || time.Now().After(t.ExpiresAt) {
		return Grant{}, User{}, false, nil
	}
	g := m.grants[t.grantID]
	return g, m.users[g.GitHubID], true, nil
}

func (m *memStore) TouchGrant(_ context.Context, id string, at time.Time) error {
	g := m.grants[id]
	g.LastUsedAt = &at
	m.grants[id] = g
	return nil
}

func (m *memStore) Grants(_ context.Context, githubID int64) ([]Grant, error) {
	var out []Grant
	for _, g := range m.grants {
		if g.GitHubID == githubID {
			out = append(out, g)
		}
	}
	return out, nil
}

func (m *memStore) DeleteGrant(_ context.Context, id string, githubID int64) (bool, error) {
	g, ok := m.grants[id]
	if !ok || g.GitHubID != githubID {
		return false, nil
	}
	m.deleteGrant(id)
	return true, nil
}

func (m *memStore) RevokeToken(_ context.Context, hash, clientID string) error {
	if t, ok := m.tokens[hash]; ok && (clientID == "" || m.grants[t.grantID].ClientID == clientID) {
		m.deleteGrant(t.grantID)
	}
	return nil
}

func (m *memStore) DeleteExpiredOAuth(context.Context, time.Time) error { return nil }

func (m *memStore) DeleteDisallowedGrants(_ context.Context, allowed []string) error {
	for id, g := range m.grants {
		if !slices.ContainsFunc(allowed, func(l string) bool { return strings.EqualFold(l, m.users[g.GitHubID].Login) }) {
			m.deleteGrant(id)
		}
	}
	return nil
}

const (
	testBase     = "https://shed.example.com"
	testResource = testBase + "/mcp"
	testRedirect = "http://127.0.0.1:4000/callback"
	testVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
)

func testChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// oauthEnv is an Auth with its routes mounted as the api package mounts them,
// and a signed-in user.
type oauthEnv struct {
	t       *testing.T
	auth    *Auth
	store   *memStore
	handler http.Handler
	cookie  *http.Cookie // alice's session
}

func newOAuthEnv(t *testing.T) *oauthEnv {
	t.Helper()
	st := newMemStore()
	return newOAuthEnvWith(t, st, "alice", "bob")
}

func newOAuthEnvWith(t *testing.T, st *memStore, allowed ...string) *oauthEnv {
	t.Helper()
	a := newAuth(st, nil, testBase, allowed...)
	st.users[1] = User{GitHubID: 1, Login: "alice"}
	st.users[2] = User{GitHubID: 2, Login: "bob"}
	st.sessions[hashToken("alice-session")] = 1
	st.sessions[hashToken("bob-session")] = 2

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-protected-resource", a.ProtectedResourceMetadata)
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", a.ProtectedResourceMetadata)
	mux.HandleFunc("/.well-known/oauth-authorization-server", a.AuthorizationServerMetadata)
	mux.HandleFunc("/oauth/register", a.Register)
	mux.HandleFunc("GET /oauth/authorize", a.Authorize)
	mux.HandleFunc("/oauth/token", a.Token)
	mux.HandleFunc("/oauth/revoke", a.Revoke)
	mux.Handle("GET /api/oauth/requests/{id}", a.Require(http.HandlerFunc(a.AuthorizationRequest)))
	mux.Handle("POST /api/oauth/requests/{id}", a.Require(http.HandlerFunc(a.DecideAuthorization)))
	mux.Handle("GET /api/oauth/grants", a.Require(http.HandlerFunc(a.Grants)))
	mux.Handle("DELETE /api/oauth/grants/{id}", a.Require(http.HandlerFunc(a.RevokeGrant)))
	mux.Handle("/mcp", a.RequireBearer(a.Resource())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := UserFrom(r.Context())
		fmt.Fprintf(w, "%s %s", u.Login, strings.Join(ScopesFrom(r.Context()), ","))
	})))
	return &oauthEnv{t: t, auth: a, store: st, handler: mux, cookie: &http.Cookie{Name: sessionCookie, Value: "alice-session"}}
}

func (e *oauthEnv) serve(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func (e *oauthEnv) register(body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/oauth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return e.serve(req)
}

// client registers a client with the test redirect URI and returns its ID.
func (e *oauthEnv) client() string {
	e.t.Helper()
	rec := e.register(`{"client_name":"Claude Code","redirect_uris":["` + testRedirect + `"]}`)
	var out struct {
		ClientID string `json:"client_id"`
	}
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.ClientID == "" {
		e.t.Fatalf("register = %d %s", rec.Code, rec.Body)
	}
	return out.ClientID
}

func authorizeQuery(clientID string) url.Values {
	return url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {testRedirect},
		"code_challenge":        {testChallenge(testVerifier)},
		"code_challenge_method": {"S256"},
		"state":                 {"xyz"},
		"resource":              {testResource},
	}
}

func (e *oauthEnv) authorize(q url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/oauth/authorize?"+q.Encode(), nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return e.serve(req)
}

// requestID runs the authorize step as alice and returns the pending request ID.
func (e *oauthEnv) requestID(q url.Values) string {
	e.t.Helper()
	rec := e.authorize(q, e.cookie)
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusFound || loc.Path != "/authorize" || loc.Query().Get("request") == "" {
		e.t.Fatalf("authorize = %d %q %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	return loc.Query().Get("request")
}

func (e *oauthEnv) decide(id string, approve bool, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/oauth/requests/"+id, strings.NewReader(fmt.Sprintf(`{"approve":%t}`, approve)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	return e.serve(req)
}

// code runs authorize and approval for clientID and returns the code.
func (e *oauthEnv) code(clientID string) string {
	e.t.Helper()
	rec := e.decide(e.requestID(authorizeQuery(clientID)), true, e.cookie)
	var out struct{ Redirect string }
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		e.t.Fatalf("approve = %d %s", rec.Code, rec.Body)
	}
	u, _ := url.Parse(out.Redirect)
	if u.Host != "127.0.0.1:4000" || u.Query().Get("state") != "xyz" || u.Query().Get("code") == "" {
		e.t.Fatalf("approval redirect = %q", out.Redirect)
	}
	return u.Query().Get("code")
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
}

func (e *oauthEnv) token(form url.Values) (int, tokenResponse) {
	e.t.Helper()
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := e.serve(req)
	var out tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		e.t.Fatalf("token response %d %q: %v", rec.Code, rec.Body, err)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		e.t.Errorf("token response Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
	return rec.Code, out
}

func codeForm(clientID, code string) url.Values {
	return url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {testRedirect},
		"code_verifier": {testVerifier},
		"resource":      {testResource},
	}
}

func refreshForm(clientID, refresh string) url.Values {
	return url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {clientID}}
}

// tokens runs the whole flow for a new client and returns its ID and tokens.
func (e *oauthEnv) tokens() (string, tokenResponse) {
	e.t.Helper()
	clientID := e.client()
	status, tok := e.token(codeForm(clientID, e.code(clientID)))
	if status != http.StatusOK {
		e.t.Fatalf("token exchange = %d %+v", status, tok)
	}
	return clientID, tok
}

// call requests /mcp with a bearer token and returns the response.
func (e *oauthEnv) call(token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/mcp", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return e.serve(req)
}

func TestOAuthFlow(t *testing.T) {
	e := newOAuthEnv(t)
	clientID := e.client()

	// Without a session, authorize sends the user to sign in and back.
	q := authorizeQuery(clientID)
	rec := e.authorize(q, nil)
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusFound || loc.Path != "/api/auth/login" {
		t.Fatalf("authorize without session = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if next := loc.Query().Get("next"); next != "/oauth/authorize?"+q.Encode() {
		t.Errorf("next = %q", next)
	}

	// The consent page reads the pending request.
	id := e.requestID(q)
	req := httptest.NewRequest("GET", "/api/oauth/requests/"+id, nil)
	req.AddCookie(e.cookie)
	rec = e.serve(req)
	var pending map[string]any
	json.Unmarshal(rec.Body.Bytes(), &pending)
	if rec.Code != http.StatusOK || pending["clientName"] != "Claude Code" || pending["redirectHost"] != "127.0.0.1:4000" ||
		pending["id"] != id || fmt.Sprint(pending["scopes"]) != "[read]" {
		t.Fatalf("request = %d %v", rec.Code, pending)
	}

	// Approve, then exchange the code.
	rec = e.decide(id, true, e.cookie)
	var decided struct{ Redirect string }
	json.Unmarshal(rec.Body.Bytes(), &decided)
	code, _ := url.Parse(decided.Redirect)
	status, tok := e.token(codeForm(clientID, code.Query().Get("code")))
	if status != http.StatusOK || tok.TokenType != "Bearer" || tok.ExpiresIn != 3600 || tok.Scope != "read" ||
		!strings.HasPrefix(tok.AccessToken, accessPrefix) || !strings.HasPrefix(tok.RefreshToken, refreshPrefix) {
		t.Fatalf("token = %d %+v", status, tok)
	}
	if rec := e.call(tok.AccessToken); rec.Code != http.StatusOK || rec.Body.String() != "alice read" {
		t.Fatalf("bearer call = %d %q", rec.Code, rec.Body)
	}
	if _, ok := e.store.codes[hashToken(code.Query().Get("code"))]; !ok {
		t.Error("redeemed code forgotten too early to detect replay")
	}

	// The request was single use.
	if rec := e.decide(id, true, e.cookie); rec.Code != http.StatusNotFound {
		t.Errorf("deciding twice = %d", rec.Code)
	}

	// Refresh rotates the refresh token.
	status, next := e.token(refreshForm(clientID, tok.RefreshToken))
	if status != http.StatusOK || next.RefreshToken == tok.RefreshToken || next.AccessToken == tok.AccessToken {
		t.Fatalf("refresh = %d %+v", status, next)
	}
	if rec := e.call(next.AccessToken); rec.Code != http.StatusOK {
		t.Fatalf("refreshed access token = %d", rec.Code)
	}

	// The grant is listed.
	req = httptest.NewRequest("GET", "/api/oauth/grants", nil)
	req.AddCookie(e.cookie)
	rec = e.serve(req)
	var grants []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &grants)
	if len(grants) != 1 || grants[0]["clientName"] != "Claude Code" || grants[0]["redirectHost"] != "127.0.0.1:4000" ||
		grants[0]["lastUsedAt"] == nil {
		t.Fatalf("grants = %s", rec.Body)
	}

	// Reusing the rotated refresh token revokes the whole grant.
	if status, out := e.token(refreshForm(clientID, tok.RefreshToken)); status != http.StatusBadRequest || out.Error != "invalid_grant" {
		t.Fatalf("reuse = %d %+v", status, out)
	}
	if rec := e.call(next.AccessToken); rec.Code != http.StatusUnauthorized {
		t.Errorf("access token after reuse = %d", rec.Code)
	}
	if status, _ := e.token(refreshForm(clientID, next.RefreshToken)); status != http.StatusBadRequest {
		t.Errorf("new refresh token after reuse = %d", status)
	}
}

func TestCodeReplayRevokesGrant(t *testing.T) {
	e := newOAuthEnv(t)
	clientID := e.client()
	code := e.code(clientID)
	_, tok := e.token(codeForm(clientID, code))
	if status, out := e.token(codeForm(clientID, code)); status != http.StatusBadRequest || out.Error != "invalid_grant" {
		t.Fatalf("second exchange = %d %+v", status, out)
	}
	if rec := e.call(tok.AccessToken); rec.Code != http.StatusUnauthorized {
		t.Errorf("token from replayed code = %d", rec.Code)
	}
}

func TestTokenErrors(t *testing.T) {
	tests := []struct {
		name   string
		change func(f url.Values, clientID string)
		status int
		err    string
	}{
		{"wrong verifier", func(f url.Values, _ string) { f.Set("code_verifier", strings.Repeat("a", 43)) }, 400, "invalid_grant"},
		{"short verifier", func(f url.Values, _ string) { f.Set("code_verifier", "abc") }, 400, "invalid_grant"},
		{"missing verifier", func(f url.Values, _ string) { f.Del("code_verifier") }, 400, "invalid_request"},
		{"redirect mismatch", func(f url.Values, _ string) { f.Set("redirect_uri", "http://127.0.0.1:4001/callback") }, 400, "invalid_grant"},
		{"other client", func(f url.Values, _ string) { f.Set("client_id", "someone-else") }, 400, "invalid_grant"},
		{"audience mismatch", func(f url.Values, _ string) { f.Set("resource", "https://evil.example.com/mcp") }, 400, "invalid_target"},
		{"unknown code", func(f url.Values, _ string) { f.Set("code", "nope") }, 400, "invalid_grant"},
		{"unsupported grant", func(f url.Values, _ string) { f.Set("grant_type", "password") }, 400, "unsupported_grant_type"},
		{"repeated parameter", func(f url.Values, _ string) { f.Add("code", "again") }, 400, "invalid_request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newOAuthEnv(t)
			clientID := e.client()
			code := e.code(clientID)
			form := codeForm(clientID, code)
			tt.change(form, clientID)
			status, out := e.token(form)
			if status != tt.status || out.Error != tt.err {
				t.Errorf("token = %d %+v, want %d %s", status, out, tt.status, tt.err)
			}
			if out.AccessToken != "" {
				t.Error("tokens issued")
			}
			// A failed exchange spends the code.
			if tt.err == "invalid_grant" && tt.name != "unknown code" {
				if status, _ := e.token(codeForm(clientID, code)); status != http.StatusBadRequest {
					t.Errorf("code usable after failed exchange: %d", status)
				}
			}
		})
	}

	e := newOAuthEnv(t)
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(`{"grant_type":"authorization_code"}`))
	req.Header.Set("Content-Type", "application/json")
	if rec := e.serve(req); rec.Code != http.StatusBadRequest {
		t.Errorf("JSON token request = %d", rec.Code)
	}
}

func TestExpiredCode(t *testing.T) {
	e := newOAuthEnv(t)
	clientID := e.client()
	code := e.code(clientID)
	e.store.codes[hashToken(code)].ExpiresAt = time.Now().Add(-time.Second)
	if status, out := e.token(codeForm(clientID, code)); status != http.StatusBadRequest || out.Error != "invalid_grant" {
		t.Errorf("expired code = %d %+v", status, out)
	}
}

func TestRefreshErrors(t *testing.T) {
	e := newOAuthEnv(t)
	clientID, tok := e.tokens()
	if status, out := e.token(refreshForm("other", tok.RefreshToken)); status != 400 || out.Error != "invalid_grant" {
		t.Errorf("refresh by another client = %d %+v", status, out)
	}
	if status, out := e.token(refreshForm(clientID, tok.AccessToken)); status != 400 || out.Error != "invalid_grant" {
		t.Errorf("refresh with access token = %d %+v", status, out)
	}
	form := refreshForm(clientID, tok.RefreshToken)
	form.Set("scope", "write")
	if status, out := e.token(form); status != 400 || out.Error != "invalid_scope" {
		t.Errorf("scope escalation = %d %+v", status, out)
	}
	// Failed refreshes leave the token usable.
	if status, _ := e.token(refreshForm(clientID, tok.RefreshToken)); status != http.StatusOK {
		t.Errorf("refresh after rejected attempts = %d", status)
	}
}

func TestAuthorizeValidation(t *testing.T) {
	// Every invalid request gets an error page, signed in or not: a
	// redirect to a dynamically registered URI would make shed an open
	// redirector.
	tests := []struct {
		name   string
		change func(q url.Values)
	}{
		{"unknown client", func(q url.Values) { q.Set("client_id", "nope") }},
		{"missing client", func(q url.Values) { q.Del("client_id") }},
		{"unregistered redirect", func(q url.Values) { q.Set("redirect_uri", "https://evil.example.com/callback") }},
		{"redirect path differs", func(q url.Values) { q.Set("redirect_uri", "http://127.0.0.1:4000/other") }},
		{"redirect host differs", func(q url.Values) { q.Set("redirect_uri", "http://localhost:4000/callback") }},
		{"missing redirect", func(q url.Values) { q.Del("redirect_uri") }},
		{"repeated parameter", func(q url.Values) { q.Add("state", "again") }},
		{"wrong response type", func(q url.Values) { q.Set("response_type", "token") }},
		{"missing response type", func(q url.Values) { q.Del("response_type") }},
		{"no PKCE", func(q url.Values) { q.Del("code_challenge"); q.Del("code_challenge_method") }},
		{"plain PKCE", func(q url.Values) { q.Set("code_challenge_method", "plain") }},
		{"malformed challenge", func(q url.Values) { q.Set("code_challenge", "short") }},
		{"unknown scope", func(q url.Values) { q.Set("scope", "read write") }},
		{"other resource", func(q url.Values) { q.Set("resource", "https://other.example.com/mcp") }},
	}
	for _, tt := range tests {
		for _, signedIn := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/signed in %t", tt.name, signedIn), func(t *testing.T) {
				e := newOAuthEnv(t)
				q := authorizeQuery(e.client())
				tt.change(q)
				var cookie *http.Cookie
				if signedIn {
					cookie = e.cookie
				}
				rec := e.authorize(q, cookie)
				if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" ||
					!strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
					t.Errorf("got %d Location=%q, want error page", rec.Code, rec.Header().Get("Location"))
				}
			})
		}
	}
}

func TestAuthorizeLoopbackPort(t *testing.T) {
	e := newOAuthEnv(t)
	clientID := e.client()
	q := authorizeQuery(clientID)
	q.Set("redirect_uri", "http://127.0.0.1:59123/callback")
	q.Del("resource")
	q.Del("scope")
	rec := e.decide(e.requestID(q), true, e.cookie)
	var out struct{ Redirect string }
	json.Unmarshal(rec.Body.Bytes(), &out)
	u, _ := url.Parse(out.Redirect)
	if u.Host != "127.0.0.1:59123" {
		t.Fatalf("redirect = %q", out.Redirect)
	}
	form := codeForm(clientID, u.Query().Get("code"))
	form.Set("redirect_uri", "http://127.0.0.1:59123/callback")
	form.Del("resource")
	if status, tok := e.token(form); status != http.StatusOK || tok.Scope != "read" {
		t.Fatalf("token = %d %+v", status, tok)
	}
}

func TestDecideAuthorization(t *testing.T) {
	e := newOAuthEnv(t)
	clientID := e.client()
	bob := &http.Cookie{Name: sessionCookie, Value: "bob-session"}

	// Requests are bound to the user who started them.
	id := e.requestID(authorizeQuery(clientID))
	req := httptest.NewRequest("GET", "/api/oauth/requests/"+id, nil)
	req.AddCookie(bob)
	if rec := e.serve(req); rec.Code != http.StatusNotFound {
		t.Errorf("other user read request: %d", rec.Code)
	}
	if rec := e.decide(id, true, bob); rec.Code != http.StatusNotFound {
		t.Errorf("other user decided request: %d", rec.Code)
	}

	// Denying returns access_denied and spends the request.
	rec := e.decide(id, false, e.cookie)
	var out struct{ Redirect string }
	json.Unmarshal(rec.Body.Bytes(), &out)
	u, _ := url.Parse(out.Redirect)
	if rec.Code != http.StatusOK || u.Query().Get("error") != "access_denied" || u.Query().Get("state") != "xyz" || u.Query().Has("code") {
		t.Errorf("deny = %d %q", rec.Code, out.Redirect)
	}
	if rec := e.decide(id, true, e.cookie); rec.Code != http.StatusNotFound {
		t.Errorf("approve after deny = %d", rec.Code)
	}
	if len(e.store.codes) != 0 {
		t.Error("code issued on deny")
	}

	// Expired requests are gone.
	id = e.requestID(authorizeQuery(clientID))
	e.auth.requests.m[id].expires = time.Now().Add(-time.Second)
	if rec := e.decide(id, true, e.cookie); rec.Code != http.StatusNotFound {
		t.Errorf("expired request = %d", rec.Code)
	}

	// Without a session the API refuses.
	req = httptest.NewRequest("GET", "/api/oauth/requests/"+id, nil)
	if rec := e.serve(req); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session = %d", rec.Code)
	}
}

func TestPendingRequestsBounded(t *testing.T) {
	rs := newRequests()
	for range maxPending + 10 {
		if _, err := rs.add(&request{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(rs.m) != maxPending {
		t.Errorf("pending = %d, want %d", len(rs.m), maxPending)
	}
}

func TestRegisterValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
		err  string
	}{
		{"https", `{"redirect_uris":["https://claude.ai/api/mcp/auth_callback"],"client_name":"Claude"}`, 201, ""},
		{"loopback ipv4", `{"redirect_uris":["http://127.0.0.1:33418/callback"],"token_endpoint_auth_method":"none"}`, 201, ""},
		{"loopback ipv6", `{"redirect_uris":["http://[::1]/cb"]}`, 201, ""},
		{"localhost", `{"redirect_uris":["http://localhost:6274/oauth/callback"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"]}`, 201, ""},
		{"http non-loopback", `{"redirect_uris":["http://example.com/cb"]}`, 400, "invalid_redirect_uri"},
		{"custom scheme", `{"redirect_uris":["cursor://callback"]}`, 400, "invalid_redirect_uri"},
		{"fragment", `{"redirect_uris":["https://x.example.com/cb#frag"]}`, 400, "invalid_redirect_uri"},
		{"user info", `{"redirect_uris":["https://me@x.example.com/cb"]}`, 400, "invalid_redirect_uri"},
		{"relative", `{"redirect_uris":["/cb"]}`, 400, "invalid_redirect_uri"},
		{"no redirect", `{"client_name":"x"}`, 400, "invalid_redirect_uri"},
		{"too many redirects", `{"redirect_uris":["https://a/1","https://a/2","https://a/3","https://a/4","https://a/5","https://a/6","https://a/7","https://a/8","https://a/9","https://a/10","https://a/11"]}`, 400, "invalid_redirect_uri"},
		{"secret client registered as public", `{"redirect_uris":["https://a/cb"],"token_endpoint_auth_method":"client_secret_post","grant_types":["client_credentials"],"scope":"write"}`, 201, ""},
		{"long name", `{"redirect_uris":["https://a/cb"],"client_name":"` + strings.Repeat("x", 101) + `"}`, 400, "invalid_client_metadata"},
		{"control characters in name", `{"redirect_uris":["https://a/cb"],"client_name":"a\nb"}`, 400, "invalid_client_metadata"},
		{"http client uri", `{"redirect_uris":["https://a/cb"],"client_uri":"http://a"}`, 400, "invalid_client_metadata"},
		{"bad json", `{`, 400, "invalid_client_metadata"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newOAuthEnv(t)
			rec := e.register(tt.body)
			var out map[string]any
			json.Unmarshal(rec.Body.Bytes(), &out)
			if rec.Code != tt.want || tt.err != "" && out["error"] != tt.err {
				t.Fatalf("register = %d %s", rec.Code, rec.Body)
			}
			if tt.want == 201 && (out["client_id"] == "" || out["token_endpoint_auth_method"] != "none" ||
				out["client_secret"] != nil || out["scope"] != "read" ||
				fmt.Sprint(out["grant_types"]) != "[authorization_code refresh_token]") {
				t.Errorf("registration = %v", out)
			}
		})
	}
}

func TestRegisterCap(t *testing.T) {
	e := newOAuthEnv(t)
	for i := range maxClients {
		e.store.clients[fmt.Sprint("c", i)] = Client{}
	}
	if rec := e.register(`{"redirect_uris":["https://a/cb"]}`); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("register past cap = %d", rec.Code)
	}
}

func TestRequireBearer(t *testing.T) {
	e := newOAuthEnv(t)
	_, tok := e.tokens()
	metadata := `resource_metadata="https://shed.example.com/.well-known/oauth-protected-resource/mcp"`

	tests := []struct {
		name   string
		header string
		cookie bool
		want   int
		err    bool // WWW-Authenticate carries error="invalid_token"
	}{
		{"valid", "Bearer " + tok.AccessToken, false, 200, false},
		{"lowercase scheme", "bearer " + tok.AccessToken, false, 200, false},
		{"no token", "", false, 401, false},
		{"cookie only", "", true, 401, false},
		{"basic", "Basic " + tok.AccessToken, false, 401, false},
		{"unknown token", "Bearer shed_at_nope", false, 401, true},
		{"refresh token", "Bearer " + tok.RefreshToken, false, 401, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/mcp", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			if tt.cookie {
				req.AddCookie(e.cookie)
			}
			rec := e.serve(req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
			if tt.want != 401 {
				return
			}
			h := rec.Header().Get("WWW-Authenticate")
			if !strings.HasPrefix(h, "Bearer ") || !strings.Contains(h, metadata) ||
				strings.Contains(h, `error="invalid_token"`) != tt.err {
				t.Errorf("WWW-Authenticate = %q", h)
			}
		})
	}

	// A token for another resource is rejected.
	other := e.auth.RequireBearer("https://shed.example.com/other")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("token accepted for another audience")
	}))
	req := httptest.NewRequest("GET", "/other", nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	rec := httptest.NewRecorder()
	other.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Header().Get("WWW-Authenticate"), "oauth-protected-resource/other") {
		t.Errorf("audience mismatch = %d %q", rec.Code, rec.Header().Get("WWW-Authenticate"))
	}

	// Expired tokens are rejected.
	e.store.tokens[hashToken(tok.AccessToken)].ExpiresAt = time.Now().Add(-time.Second)
	if rec := e.call(tok.AccessToken); rec.Code != http.StatusUnauthorized {
		t.Errorf("expired token = %d", rec.Code)
	}
}

func TestDisallowedUser(t *testing.T) {
	e := newOAuthEnv(t)
	clientID, tok := e.tokens()

	// shed restarts without alice in the allowlist.
	removed := newOAuthEnvWith(t, e.store, "bob")
	if rec := removed.call(tok.AccessToken); rec.Code != http.StatusUnauthorized {
		t.Errorf("removed user's token = %d", rec.Code)
	}
	if status, out := removed.token(refreshForm(clientID, tok.RefreshToken)); status != 400 || out.Error != "invalid_grant" {
		t.Errorf("removed user's refresh = %d %+v", status, out)
	}
	if rec := removed.authorize(authorizeQuery(clientID), e.cookie); !strings.HasPrefix(rec.Header().Get("Location"), "/api/auth/login") {
		t.Errorf("removed user's authorize = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if err := removed.auth.RevokeDisallowedSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(e.store.grants) != 0 {
		t.Errorf("grants of removed user kept: %v", e.store.grants)
	}
}

func TestRevoke(t *testing.T) {
	revoke := func(e *oauthEnv, form url.Values) int {
		req := httptest.NewRequest("POST", "/oauth/revoke", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return e.serve(req).Code
	}
	e := newOAuthEnv(t)
	clientID, tok := e.tokens()
	if code := revoke(e, url.Values{"token": {tok.RefreshToken}, "client_id": {"other"}}); code != 200 {
		t.Errorf("revoke by other client = %d", code)
	}
	if rec := e.call(tok.AccessToken); rec.Code != http.StatusOK {
		t.Error("another client revoked the grant")
	}
	if code := revoke(e, url.Values{"token": {tok.RefreshToken}, "token_type_hint": {"refresh_token"}, "client_id": {clientID}}); code != 200 {
		t.Errorf("revoke = %d", code)
	}
	if rec := e.call(tok.AccessToken); rec.Code != http.StatusUnauthorized {
		t.Error("access token survived revocation")
	}
	if code := revoke(e, url.Values{"token": {"unknown"}}); code != 200 {
		t.Errorf("revoke unknown = %d", code)
	}
	if code := revoke(e, url.Values{}); code != 400 {
		t.Errorf("revoke without token = %d", code)
	}
}

func TestGrantsAPI(t *testing.T) {
	e := newOAuthEnv(t)
	_, tok := e.tokens()
	var id string
	for k := range e.store.grants {
		id = k
	}
	del := func(cookie string) int {
		req := httptest.NewRequest("DELETE", "/api/oauth/grants/"+id, nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
		return e.serve(req).Code
	}
	if code := del("bob-session"); code != http.StatusNotFound {
		t.Errorf("bob revoking alice's grant = %d", code)
	}
	if code := del("alice-session"); code != http.StatusNoContent {
		t.Errorf("revoke grant = %d", code)
	}
	if rec := e.call(tok.AccessToken); rec.Code != http.StatusUnauthorized {
		t.Error("token survived grant revocation")
	}
	req := httptest.NewRequest("GET", "/api/oauth/grants", nil)
	req.AddCookie(e.cookie)
	if rec := e.serve(req); strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("grants after revoke = %s", rec.Body)
	}
}

func TestOAuthMetadata(t *testing.T) {
	e := newOAuthEnv(t)
	get := func(path string) map[string]any {
		rec := e.serve(httptest.NewRequest("GET", path, nil))
		var m map[string]any
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &m) != nil || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Fatalf("%s = %d %s", path, rec.Code, rec.Body)
		}
		return m
	}
	for _, p := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		m := get(p)
		if m["resource"] != testResource || fmt.Sprint(m["authorization_servers"]) != "["+testBase+"]" ||
			fmt.Sprint(m["scopes_supported"]) != "[read]" || fmt.Sprint(m["bearer_methods_supported"]) != "[header]" {
			t.Errorf("%s = %v", p, m)
		}
	}
	m := get("/.well-known/oauth-authorization-server")
	want := map[string]string{
		"issuer":                                testBase,
		"authorization_endpoint":                testBase + "/oauth/authorize",
		"token_endpoint":                        testBase + "/oauth/token",
		"registration_endpoint":                 testBase + "/oauth/register",
		"revocation_endpoint":                   testBase + "/oauth/revoke",
		"response_types_supported":              "[code]",
		"grant_types_supported":                 "[authorization_code refresh_token]",
		"code_challenge_methods_supported":      "[S256]",
		"token_endpoint_auth_methods_supported": "[none]",
		"scopes_supported":                      "[read]",
	}
	for k, v := range want {
		if fmt.Sprint(m[k]) != v {
			t.Errorf("%s = %v, want %s", k, m[k], v)
		}
	}

	// Preflight requests are answered for cross-origin clients.
	req := httptest.NewRequest("OPTIONS", "/oauth/token", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := e.serve(req)
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "*" ||
		!strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), "POST") {
		t.Errorf("preflight = %d %v", rec.Code, rec.Header())
	}
	if rec := e.serve(httptest.NewRequest("GET", "/oauth/token", nil)); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /oauth/token = %d", rec.Code)
	}
}

func TestLoginNext(t *testing.T) {
	tests := []struct {
		next, want string
	}{
		{"/oauth/authorize?client_id=a&state=b", "/oauth/authorize?client_id=a&state=b"},
		{"/projects/x", "/projects/x"},
		{"", "/"},
		{"//evil.example.com", "/"},
		{"https://evil.example.com/", "/"},
		{"/\\evil.example.com", "/"},
		{"/\t/evil.example.com", "/"},
		{"evil.example.com", "/"},
		{"/" + strings.Repeat("a", maxNextLen), "/"},
	}
	for _, tt := range tests {
		t.Run(tt.next, func(t *testing.T) {
			a := newAuth(newMemStore(), fakeOAuth{user: github.User{ID: 1, Login: "alice"}}, "http://x", "alice")
			login := httptest.NewRecorder()
			a.Login(login, httptest.NewRequest("GET", "/api/auth/login?next="+url.QueryEscape(tt.next), nil))
			loc, _ := url.Parse(login.Header().Get("Location"))
			req := httptest.NewRequest("GET", "/api/auth/callback?code=c&state="+loc.Query().Get("state"), nil)
			for _, c := range login.Result().Cookies() {
				req.AddCookie(c)
			}
			rec := httptest.NewRecorder()
			a.Callback(rec, req)
			if got := rec.Header().Get("Location"); got != tt.want {
				t.Errorf("redirect = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLocalPath(t *testing.T) {
	for p, want := range map[string]bool{
		"/":                    true,
		"/a/b?c=d":             true,
		"//x":                  false,
		"/\\x":                 false,
		"/x\n":                 false,
		"http://x/":            false,
		"x":                    false,
		"/%2F%2Fevil.example/": true, // stays a path on this host
	} {
		if got := localPath(p); got != want {
			t.Errorf("localPath(%q) = %v, want %v", p, got, want)
		}
	}
}
