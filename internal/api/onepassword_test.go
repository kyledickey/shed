package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/kyledickey/shed/internal/onepassword"
)

// fakeOnePassword serves envs, each to the token "<id>-token" only.
type fakeOnePassword struct {
	envs    map[string]map[string]string
	readErr error
}

func (f *fakeOnePassword) EnvironmentVariables(_ context.Context, token, id string) (map[string]string, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	if token != id+"-token" {
		return nil, fmt.Errorf("%w: invalid service account token", onepassword.ErrInvalidToken)
	}
	return f.envs[id], nil
}

func TestOnePasswordLink(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	f.onePassword.envs = map[string]map[string]string{"env1": {"API_KEY": "k"}}
	target := "/api/services/" + svc.ID + "/onepassword"

	if got := f.decode(f.do("GET", "/api/services/"+svc.ID, ""), http.StatusOK); got["onePassword"] != nil {
		t.Fatalf("unlinked service onePassword = %v, want null", got["onePassword"])
	}
	got := f.decode(f.do("PUT", target, `{"environment":"env1","token":"wrong"}`), http.StatusBadRequest)
	if msg, _ := got["error"].(string); !strings.Contains(msg, "1Password rejected the token") {
		t.Errorf("error = %q, want the 1Password message", msg)
	}
	f.decode(f.do("PUT", target, `{"environment":"Not Valid","token":"env1-token"}`), http.StatusBadRequest)
	if got := f.decode(f.do("GET", "/api/services/"+svc.ID, ""), http.StatusOK); got["onePassword"] != nil {
		t.Fatalf("rejected link saved: %v", got["onePassword"])
	}

	rec := f.do("PUT", target, `{"environment":"env1","token":"env1-token"}`)
	if strings.Contains(rec.Body.String(), "env1-token") {
		t.Errorf("response contains the token: %s", rec.Body)
	}
	got = f.decode(rec, http.StatusOK)
	if link, _ := got["onePassword"].(map[string]any); link["environment"] != "env1" || link["hasToken"] != true || len(link) != 2 {
		t.Errorf("onePassword = %v, want env1 with a token", got["onePassword"])
	}
	for _, path := range []string{"/api/services/" + svc.ID, "/api/projects/" + svc.ProjectID} {
		if rec := f.do("GET", path, ""); rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "env1-token") {
			t.Errorf("GET %s = %d, contains the token: %s", path, rec.Code, rec.Body)
		}
	}
	// A blank token keeps the saved one.
	f.decode(f.do("PUT", target, `{"environment":"env1"}`), http.StatusOK)

	got = f.decode(f.do("DELETE", target, ""), http.StatusOK)
	if got["onePassword"] != nil {
		t.Errorf("after DELETE onePassword = %v, want null", got["onePassword"])
	}
	f.decode(f.do("PUT", "/api/services/nope/onepassword", `{"environment":"env1","token":"env1-token"}`), http.StatusNotFound)
}

func TestOnePasswordVariables(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	f.onePassword.envs = map[string]map[string]string{"env1": {"API_KEY": "k"}}
	target := "/api/services/" + svc.ID + "/variables/onepassword"

	if got := f.decode(f.do("GET", target, ""), http.StatusOK); len(got) != 0 {
		t.Errorf("unlinked = %v, want {}", got)
	}
	f.decode(f.do("PUT", "/api/services/"+svc.ID+"/onepassword", `{"environment":"env1","token":"env1-token"}`), http.StatusOK)
	if got := f.decode(f.do("GET", target, ""), http.StatusOK); got["API_KEY"] != "k" {
		t.Errorf("linked = %v, want API_KEY", got)
	}
	f.onePassword.readErr = fmt.Errorf("%w env1: rate limited", onepassword.ErrRequest)
	f.decode(f.do("GET", target, ""), http.StatusBadGateway)
	f.decode(f.do("GET", "/api/services/nope/variables/onepassword", ""), http.StatusNotFound)
}
