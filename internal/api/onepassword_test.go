package api

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"testing"

	"github.com/kyledickey/shed/internal/onepassword"
)

// fakeOnePassword is a 1Password account that accepts the token "tok".
type fakeOnePassword struct {
	items map[string]onepassword.Item
	err   error // fails every request but signing in
}

func (f *fakeOnePassword) Preload(context.Context, string) {}

func (f *fakeOnePassword) SignIn(_ context.Context, token string) error {
	if token != "tok" {
		return fmt.Errorf("%w: invalid service account token", onepassword.ErrInvalidToken)
	}
	return nil
}

func (f *fakeOnePassword) Vaults(ctx context.Context, token string) ([]onepassword.Vault, error) {
	if err := f.SignIn(ctx, token); err != nil {
		return nil, err
	}
	return []onepassword.Vault{{ID: "vault1", Name: "Infra"}}, f.err
}

func (f *fakeOnePassword) CreateItem(_ context.Context, _, _ string, item onepassword.Item) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	id := fmt.Sprintf("item%d", len(f.items)+1)
	item.Variables = maps.Clone(item.Variables)
	f.items[id] = item
	return id, nil
}

func (f *fakeOnePassword) Variables(_ context.Context, _, _ string, ids []string) (map[string]map[string]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string]map[string]string)
	for _, id := range ids {
		item, ok := f.items[id]
		if !ok {
			return nil, &onepassword.MissingItemError{ItemID: id}
		}
		out[id] = maps.Clone(item.Variables)
	}
	return out, nil
}

func (f *fakeOnePassword) SetVariables(_ context.Context, _, _, id string, vars map[string]string) error {
	if f.err != nil {
		return f.err
	}
	item, ok := f.items[id]
	if !ok {
		return &onepassword.MissingItemError{ItemID: id}
	}
	item.Variables = maps.Clone(vars)
	f.items[id] = item
	return nil
}

func (f *fakeOnePassword) Retitle(context.Context, string, string, string, string, []string) error {
	return f.err
}

func (f *fakeOnePassword) Archive(_ context.Context, _, _, id string) error {
	delete(f.items, id)
	return f.err
}

func TestOnePasswordConnection(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	f.decode(f.do("PUT", "/api/services/"+svc.ID+"/variables", `{"API_KEY":"k"}`), http.StatusOK)

	want := func(got map[string]any, connected bool, vault string, items float64) {
		t.Helper()
		if got["connected"] != connected || got["vault"] != vault || got["items"] != items || len(got) != 3 {
			t.Errorf("OnePassword = %v, want connected %v, vault %q, items %v", got, connected, vault, items)
		}
	}
	want(f.decode(f.do("GET", "/api/onepassword", ""), http.StatusOK), false, "", 0)

	got := f.decode(f.do("PUT", "/api/onepassword", `{"token":"wrong"}`), http.StatusBadRequest)
	if msg, _ := got["error"].(string); !strings.Contains(msg, "1Password rejected the token") {
		t.Errorf("error = %q, want the 1Password message", msg)
	}
	f.decode(f.do("POST", "/api/onepassword/vaults", `{"token":"wrong"}`), http.StatusBadRequest)
	rec := f.do("POST", "/api/onepassword/vaults", `{"token":"tok"}`)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `[{"id":"vault1","name":"Infra"}]` {
		t.Errorf("POST vaults = %d %s, want the token's vaults", rec.Code, rec.Body)
	}
	got = f.decode(f.do("PUT", "/api/onepassword", `{"token":"tok","vault":"other"}`), http.StatusBadRequest)
	if msg, _ := got["error"].(string); !strings.Contains(msg, "can't access vault other") {
		t.Errorf("error = %q, want the vault message", msg)
	}
	f.onePassword.err = fmt.Errorf("%w: rate limited", onepassword.ErrRequest)
	f.decode(f.do("PUT", "/api/onepassword", `{"token":"tok"}`), http.StatusBadGateway)
	f.onePassword.err = nil

	rec = f.do("PUT", "/api/onepassword", `{"token":"tok","vault":"vault1"}`)
	if strings.Contains(rec.Body.String(), "tok\"") {
		t.Errorf("response contains the token: %s", rec.Body)
	}
	want(f.decode(rec, http.StatusOK), true, "Infra", 1)
	f.decode(f.do("PUT", "/api/onepassword", `{"token":"tok"}`), http.StatusConflict)

	vars := "/api/services/" + svc.ID + "/variables"
	if got := f.decode(f.do("GET", vars, ""), http.StatusOK); got["API_KEY"] != "k" {
		t.Errorf("variables = %v, want the item's", got)
	}
	f.onePassword.err = fmt.Errorf("%w: rate limited", onepassword.ErrRequest)
	f.decode(f.do("GET", vars, ""), http.StatusBadGateway)
	f.decode(f.do("DELETE", "/api/onepassword", ""), http.StatusBadGateway)
	f.onePassword.err = nil
	clear(f.onePassword.items)
	got = f.decode(f.do("GET", vars, ""), http.StatusConflict)
	if msg, _ := got["error"].(string); msg != "the 1Password item of service web is missing" {
		t.Errorf("error = %q", msg)
	}
	f.decode(f.do("PUT", vars, `{"B":"2"}`), http.StatusOK)

	want(f.decode(f.do("DELETE", "/api/onepassword", ""), http.StatusOK), false, "", 0)
	if got := f.decode(f.do("GET", vars, ""), http.StatusOK); got["B"] != "2" || len(got) != 1 {
		t.Errorf("variables after disconnecting = %v", got)
	}
}

func TestServiceHasNoOnePasswordField(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	if got := f.decode(f.do("GET", "/api/services/"+svc.ID, ""), http.StatusOK); got["onePassword"] != nil {
		t.Errorf("service JSON has onePassword: %v", got["onePassword"])
	} else if _, ok := got["onePassword"]; ok {
		t.Error("service JSON has an onePassword key")
	}
	for _, path := range []string{"/api/services/" + svc.ID + "/onepassword", "/api/services/" + svc.ID + "/variables/onepassword"} {
		if rec := f.do("GET", path, ""); rec.Code == http.StatusOK {
			t.Errorf("GET %s = 200, want the per-service 1Password routes gone", path)
		}
	}
}
