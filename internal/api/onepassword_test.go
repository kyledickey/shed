package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/kyledickey/shed/internal/onepassword"
)

// fakeOnePassword accepts the token "good" and serves envs while connected.
type fakeOnePassword struct {
	token   string
	envs    map[string]map[string]string
	readErr error
}

func (f *fakeOnePassword) Connected(context.Context) (bool, error) { return f.token != "", nil }

func (f *fakeOnePassword) Connect(_ context.Context, token string) error {
	if token != "good" {
		return fmt.Errorf("%w: invalid token", onepassword.ErrInvalidToken)
	}
	f.token = token
	return nil
}

func (f *fakeOnePassword) Disconnect(context.Context) error {
	f.token = ""
	return nil
}

func (f *fakeOnePassword) Variables(_ context.Context, id string) (map[string]string, error) {
	if f.token == "" {
		return nil, onepassword.ErrNotConnected
	}
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.envs[id], nil
}

func TestOnePasswordConnection(t *testing.T) {
	f := newFixture(t)
	if got := f.decode(f.do("GET", "/api/onepassword", ""), http.StatusOK); got["connected"] != false {
		t.Errorf("GET = %v, want disconnected", got)
	}
	got := f.decode(f.do("PUT", "/api/onepassword", `{"token":"bad"}`), http.StatusBadRequest)
	if msg, _ := got["error"].(string); !strings.Contains(msg, "rejected") {
		t.Errorf("PUT bad token error = %q", msg)
	}
	f.decode(f.do("PUT", "/api/onepassword", `{}`), http.StatusBadRequest)
	if f.onePassword.token != "" {
		t.Fatalf("saved a rejected token %q", f.onePassword.token)
	}
	if got := f.decode(f.do("PUT", "/api/onepassword", `{"token":"good"}`), http.StatusOK); got["connected"] != true {
		t.Errorf("PUT good token = %v, want connected", got)
	}
	if rec := f.do("DELETE", "/api/onepassword", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", rec.Code)
	}
	if got := f.decode(f.do("GET", "/api/onepassword", ""), http.StatusOK); got["connected"] != false {
		t.Errorf("GET after DELETE = %v, want disconnected", got)
	}
}

func TestOnePasswordVariables(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	f.onePassword.envs = map[string]map[string]string{"env1": {"API_KEY": "k"}}
	target := "/api/services/" + svc.ID + "/variables/onepassword"

	if got := f.decode(f.do("GET", target, ""), http.StatusOK); len(got) != 0 {
		t.Errorf("unlinked = %v, want {}", got)
	}
	f.decode(f.do("PATCH", "/api/services/"+svc.ID, `{"onePasswordEnvironment":"Not Valid"}`), http.StatusBadRequest)
	if got := f.decode(f.do("PATCH", "/api/services/"+svc.ID, `{"onePasswordEnvironment":"env1"}`), http.StatusOK); got["onePasswordEnvironment"] != "env1" {
		t.Fatalf("PATCH = %v, want onePasswordEnvironment env1", got["onePasswordEnvironment"])
	}

	f.decode(f.do("GET", target, ""), http.StatusConflict)
	f.onePassword.token = "good"
	if got := f.decode(f.do("GET", target, ""), http.StatusOK); got["API_KEY"] != "k" {
		t.Errorf("linked = %v, want API_KEY", got)
	}
	f.onePassword.readErr = fmt.Errorf("%w env1: rate limited", onepassword.ErrRead)
	f.decode(f.do("GET", target, ""), http.StatusBadGateway)
	f.decode(f.do("GET", "/api/services/nope/variables/onepassword", ""), http.StatusNotFound)
}
