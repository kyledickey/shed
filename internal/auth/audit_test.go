package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/github"
)

// recorder keeps audit events with the actor of their context.
type recorder struct {
	mu     sync.Mutex
	events []recorded
}

type recorded struct {
	audit.Event
	Actor audit.Actor
}

func (r *recorder) Record(ctx context.Context, e audit.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recorded{e, audit.ActorFrom(ctx)})
}

// actions returns "action outcome actor-kind" of each event.
func (r *recorder) actions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, e := range r.events {
		out = append(out, fmt.Sprintf("%s %s %s", e.Action, e.Outcome, e.Actor.Kind))
	}
	return out
}

func (r *recorder) last() recorded {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.events[len(r.events)-1]
}

func recorderOf(a *Auth) *recorder { return a.audit.(*recorder) }

func TestSignInAudit(t *testing.T) {
	st := newMemStore()
	a := newAuth(st, fakeOAuth{user: github.User{ID: 1, Login: "alice"}}, "http://x", "alice")
	sess := cookieNamed(signIn(t, a, "code"), sessionCookie)
	req := httptest.NewRequest("POST", "/api/auth/logout", nil)
	req.AddCookie(sess)
	a.Logout(httptest.NewRecorder(), req)
	denied := newAuth(st, fakeOAuth{user: github.User{ID: 2, Login: "mallory"}}, "http://x", "alice")
	signIn(t, denied, "code")

	if got, want := recorderOf(a).actions(), []string{"auth.signin ok user", "auth.logout ok user"}; !slices.Equal(got, want) {
		t.Errorf("events = %q, want %q", got, want)
	}
	if e := recorderOf(a).last(); e.Actor.Name != "alice" || e.Actor.IP != "192.0.2.1" {
		t.Errorf("logout actor = %+v, want alice at the request's address", e.Actor)
	}
	e := recorderOf(denied).last()
	if e.Action != "auth.signin.denied" || e.Outcome != audit.Denied || e.Actor.Kind != audit.Anonymous ||
		e.Detail["login"] != "mallory" {
		t.Errorf("denied sign-in = %+v", e)
	}
}

func TestRequireSetsActor(t *testing.T) {
	st := newMemStore()
	a := newAuth(st, fakeOAuth{user: github.User{ID: 1, Login: "alice"}}, "http://x", "alice")
	sess := cookieNamed(signIn(t, a, "code"), sessionCookie)
	var got audit.Actor
	h := a.Require(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = audit.ActorFrom(r.Context()) }))
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.AddCookie(sess)
	h.ServeHTTP(httptest.NewRecorder(), req)
	if want := (audit.Actor{Kind: audit.User, Name: "alice", IP: "203.0.113.9"}); got != want {
		t.Errorf("actor = %+v, want %+v", got, want)
	}
}

func TestOAuthAudit(t *testing.T) {
	e := newOAuthEnv(t)
	rec := recorderOf(e.auth)
	clientID, tok := e.tokens()

	var actor audit.Actor
	e.handler.(*http.ServeMux).Handle("/actor", e.auth.RequireBearer(e.auth.Resource())(
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { actor = audit.ActorFrom(r.Context()) })))
	req := httptest.NewRequest("POST", "/actor", nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	e.serve(req)
	if actor.Kind != audit.Agent || actor.Name != "alice" || actor.ClientID != clientID || actor.ClientName != "Claude Code" {
		t.Errorf("bearer actor = %+v, want alice's agent Claude Code", actor)
	}

	// Denying consent, a bad code, and a bad token are denials.
	id := e.requestID(authorizeQuery(clientID))
	e.decide(id, false, e.cookie)
	e.token(codeForm(clientID, "nope"))
	e.call("shed_at_nope")
	var grant string
	for k := range e.store.grants {
		grant = k
	}
	del := httptest.NewRequest("DELETE", "/api/oauth/grants/"+grant, nil)
	del.AddCookie(e.cookie)
	e.serve(del)

	want := []string{
		"oauth.grant ok user",
		"oauth.denied denied user",
		"oauth.denied denied anonymous",
		"oauth.denied denied anonymous",
		"oauth.revoke ok user",
	}
	if got := rec.actions(); !slices.Equal(got, want) {
		t.Errorf("events = %q, want %q", got, want)
	}
	if last := rec.last(); last.Target != grant || last.Detail["client"] != "Claude Code" {
		t.Errorf("revoke = %+v, want the grant and its client", last)
	}
}
