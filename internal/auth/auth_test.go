package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/github"
)

type memStore struct {
	users    map[int64]User
	sessions map[string]int64 // token hash -> GitHub ID
}

func newMemStore() *memStore {
	return &memStore{users: map[int64]User{}, sessions: map[string]int64{}}
}

func (m *memStore) CountUsers(context.Context) (int, error) { return len(m.users), nil }

func (m *memStore) User(_ context.Context, id int64) (User, bool, error) {
	u, ok := m.users[id]
	return u, ok, nil
}

func (m *memStore) UpsertUser(_ context.Context, u User) error {
	m.users[u.GitHubID] = u
	return nil
}

func (m *memStore) CreateSession(_ context.Context, hash string, id int64, _ time.Time) error {
	m.sessions[hash] = id
	return nil
}

func (m *memStore) SessionUser(_ context.Context, hash string) (User, bool, error) {
	id, ok := m.sessions[hash]
	return m.users[id], ok, nil
}

func (m *memStore) DeleteSession(_ context.Context, hash string) error {
	delete(m.sessions, hash)
	return nil
}

type fakeOAuth struct {
	user github.User
	err  error
}

func (f fakeOAuth) AuthorizeURL(redirectURL, state string) string {
	return "https://github.test/authorize?redirect_uri=" + url.QueryEscape(redirectURL) + "&state=" + state
}

func (f fakeOAuth) ExchangeUser(context.Context, string, string) (github.User, error) {
	return f.user, f.err
}

func newAuth(store Store, oauth OAuth, baseURL string, allowed ...string) *Auth {
	get := func() (OAuth, bool) { return oauth, oauth != nil }
	return New(store, get, baseURL, allowed, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// signIn runs Login then Callback and returns the callback response.
func signIn(t *testing.T, a *Auth, code string) *httptest.ResponseRecorder {
	t.Helper()
	login := httptest.NewRecorder()
	a.Login(login, httptest.NewRequest("GET", "/api/auth/login", nil))
	loc, err := url.Parse(login.Header().Get("Location"))
	if err != nil || login.Code != http.StatusFound {
		t.Fatalf("Login = %d %q, %v", login.Code, login.Header().Get("Location"), err)
	}
	req := httptest.NewRequest("GET", "/api/auth/callback?code="+code+"&state="+loc.Query().Get("state"), nil)
	for _, c := range login.Result().Cookies() {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	a.Callback(rec, req)
	return rec
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestLoginRedirectAndStateCookie(t *testing.T) {
	a := newAuth(newMemStore(), fakeOAuth{}, "https://shed.example.com/")
	rec := httptest.NewRecorder()
	a.Login(rec, httptest.NewRequest("GET", "/api/auth/login", nil))

	loc, _ := url.Parse(rec.Header().Get("Location"))
	if loc.Host != "github.test" || loc.Query().Get("redirect_uri") != "https://shed.example.com/api/auth/callback" {
		t.Errorf("Location = %v", loc)
	}
	c := cookieNamed(rec, stateCookie)
	if c == nil || c.Value != loc.Query().Get("state") || c.Value == "" {
		t.Fatalf("state cookie = %+v", c)
	}
	if !c.HttpOnly || !c.Secure || c.MaxAge != 600 {
		t.Errorf("state cookie flags = %+v", c)
	}
}

func TestLoginNotConfigured(t *testing.T) {
	rec := httptest.NewRecorder()
	newAuth(newMemStore(), nil, "http://x").Login(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestCallbackAccessRule(t *testing.T) {
	alice := github.User{ID: 1, Login: "Alice"}
	bob := github.User{ID: 2, Login: "bob"}
	tests := []struct {
		name     string
		existing []User
		allowed  []string
		who      github.User
		want     bool
	}{
		{"first user requires allow list", nil, nil, alice, false},
		{"second user denied without allow list", []User{{GitHubID: 1, Login: "Alice"}}, nil, bob, false},
		{"empty allowlist denies existing user", []User{{GitHubID: 2, Login: "bob"}}, nil, bob, false},
		{"removed user denied", []User{{GitHubID: 2, Login: "bob"}}, []string{"alice"}, bob, false},
		{"allow list matches case-insensitively", nil, []string{"ALICE"}, alice, true},
		{"not in allow list on empty db", nil, []string{"carol"}, alice, false},
		{"allow list admits new user despite existing", []User{{GitHubID: 9, Login: "x"}}, []string{"bob"}, bob, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMemStore()
			for _, u := range tt.existing {
				store.users[u.GitHubID] = u
			}
			rec := signIn(t, newAuth(store, fakeOAuth{user: tt.who}, "http://x", tt.allowed...), "code")
			loc := rec.Header().Get("Location")
			sess := cookieNamed(rec, sessionCookie)
			if tt.want {
				if loc != "/" || sess == nil || len(store.sessions) != 1 {
					t.Fatalf("allowed: Location=%q session=%v sessions=%d", loc, sess, len(store.sessions))
				}
				if _, ok := store.sessions[hashToken(sess.Value)]; !ok {
					t.Error("session stored under wrong hash")
				}
				if u := store.users[tt.who.ID]; u.Login != tt.who.Login {
					t.Errorf("user not saved: %+v", u)
				}
			} else if loc != "/login?error=denied" || sess != nil || len(store.sessions) != 0 {
				t.Errorf("denied: Location=%q session=%v sessions=%d", loc, sess, len(store.sessions))
			}
		})
	}
}

func TestCallbackSessionCookie(t *testing.T) {
	for base, secure := range map[string]bool{"https://x": true, "http://x": false} {
		rec := signIn(t, newAuth(newMemStore(), fakeOAuth{user: github.User{ID: 1, Login: "a"}}, base, "a"), "code")
		c := cookieNamed(rec, sessionCookie)
		if c == nil || c.Secure != secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode ||
			c.MaxAge != 30*24*3600 || len(c.Value) != 43 {
			t.Errorf("base %s: cookie = %+v", base, c)
		}
	}
}

func TestCallbackRejections(t *testing.T) {
	a := newAuth(newMemStore(), fakeOAuth{user: github.User{ID: 1, Login: "a"}}, "http://x")

	// State mismatch.
	req := httptest.NewRequest("GET", "/api/auth/callback?code=c&state=evil", nil)
	req.AddCookie(&http.Cookie{Name: stateCookie, Value: "real"})
	rec := httptest.NewRecorder()
	a.Callback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("state mismatch status = %d, want 400", rec.Code)
	}

	// No state cookie.
	rec = httptest.NewRecorder()
	a.Callback(rec, httptest.NewRequest("GET", "/api/auth/callback?code=c&state=s", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing cookie status = %d, want 400", rec.Code)
	}

	// User declined on GitHub.
	req = httptest.NewRequest("GET", "/api/auth/callback?error=access_denied&state=s", nil)
	req.AddCookie(&http.Cookie{Name: stateCookie, Value: "s"})
	rec = httptest.NewRecorder()
	a.Callback(rec, req)
	if loc := rec.Header().Get("Location"); loc != "/login?error=denied" {
		t.Errorf("declined Location = %q", loc)
	}

	// Exchange failure.
	failing := newAuth(newMemStore(), fakeOAuth{err: errors.New("boom")}, "http://x")
	rec = signIn(t, failing, "code")
	if loc := rec.Header().Get("Location"); loc != "/login?error=failed" {
		t.Errorf("exchange failure Location = %q", loc)
	}
}

func TestRequire(t *testing.T) {
	store := newMemStore()
	a := newAuth(store, fakeOAuth{user: github.User{ID: 1, Login: "alice"}}, "http://x", "alice")
	var seen User
	h := a.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = UserFrom(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/me", nil))
	if rec.Code != http.StatusUnauthorized || strings.TrimSpace(rec.Body.String()) != `{"error":"unauthorized"}` ||
		rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("no cookie: %d %q", rec.Code, rec.Body)
	}

	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "bogus"})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("bogus session status = %d", rec.Code)
	}

	sess := cookieNamed(signIn(t, a, "code"), sessionCookie)
	req = httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(sess)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || seen.Login != "alice" {
		t.Errorf("valid session: status %d, user %+v", rec.Code, seen)
	}
}

func TestLogout(t *testing.T) {
	store := newMemStore()
	a := newAuth(store, fakeOAuth{user: github.User{ID: 1, Login: "alice"}}, "http://x", "alice")
	sess := cookieNamed(signIn(t, a, "code"), sessionCookie)

	req := httptest.NewRequest("POST", "/api/auth/logout", nil)
	req.AddCookie(sess)
	rec := httptest.NewRecorder()
	a.Logout(rec, req)
	if rec.Code != http.StatusNoContent || len(store.sessions) != 0 {
		t.Errorf("Logout: status %d, sessions %d", rec.Code, len(store.sessions))
	}
	if c := cookieNamed(rec, sessionCookie); c == nil || c.MaxAge >= 0 {
		t.Errorf("cookie not cleared: %+v", c)
	}
}

func (m *memStore) DeleteDisallowedSessions(_ context.Context, allowed []string) error {
	for hash, id := range m.sessions {
		ok := false
		for _, login := range allowed {
			if strings.EqualFold(login, m.users[id].Login) {
				ok = true
			}
		}
		if !ok {
			delete(m.sessions, hash)
		}
	}
	return nil
}

func TestRemovedUserSessions(t *testing.T) {
	st := newMemStore()
	first := newAuth(st, fakeOAuth{user: github.User{ID: 1, Login: "alice"}}, "http://x", "alice")
	cookie := cookieNamed(signIn(t, first, "code"), sessionCookie)
	removed := newAuth(st, nil, "http://x", "bob")
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	removed.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("removed user admitted") })).ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatal(rec.Code)
	}
	if err := removed.RevokeDisallowedSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(st.sessions) != 0 {
		t.Fatal("removed sessions retained")
	}
}
