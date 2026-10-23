package control

import (
	"context"
	"fmt"
	"maps"
	"slices"
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
	ctx := context.Background()
	op := &fakeOnePassword{}
	f.plane.onePassword = op

	status := func() bool {
		t.Helper()
		s, err := f.plane.OnePassword(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return s.Connected
	}
	if status() {
		t.Fatal("connected before any token")
	}
	_, err := f.plane.ConnectOnePassword(ctx, "  ")
	wantKind(t, err, ErrInvalid, "token is required")
	_, err = f.plane.ConnectOnePassword(ctx, "bad")
	wantKind(t, err, ErrInvalid, "1Password rejected the token")
	if status() {
		t.Fatal("connected with a rejected token")
	}
	if s, err := f.plane.ConnectOnePassword(ctx, " good "); err != nil || !s.Connected || !status() {
		t.Fatalf("ConnectOnePassword(good) = %+v, %v", s, err)
	}
	if err := f.plane.DisconnectOnePassword(ctx); err != nil || status() {
		t.Fatalf("DisconnectOnePassword = %v, connected = %v", err, status())
	}
}

func TestOnePasswordVariables(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	op := &fakeOnePassword{envs: map[string]map[string]string{"env1": {"API_KEY": "k"}}}
	f.plane.onePassword = op
	svc := createApp(t, f.st)

	if got, err := f.plane.OnePasswordVariables(ctx, svc.ID); err != nil || len(got) != 0 || got == nil {
		t.Errorf("unlinked = %v, %v; want {}", got, err)
	}
	env := "env1"
	if _, err := f.plane.UpdateService(ctx, svc.ID, ServicePatch{OnePasswordEnvironment: &env}); err != nil {
		t.Fatal(err)
	}
	_, err := f.plane.OnePasswordVariables(ctx, svc.ID)
	wantKind(t, err, ErrConflict, "not connected")

	op.token = "good"
	if got, err := f.plane.OnePasswordVariables(ctx, svc.ID); err != nil || !maps.Equal(got, op.envs["env1"]) {
		t.Errorf("linked = %v, %v; want %v", got, err, op.envs["env1"])
	}
	op.readErr = fmt.Errorf("%w env1: rate limited", onepassword.ErrRead)
	_, err = f.plane.OnePasswordVariables(ctx, svc.ID)
	wantKind(t, err, ErrUpstream, "rate limited")

	_, err = f.plane.OnePasswordVariables(ctx, "nope")
	wantKind(t, err, ErrNotFound, "not found")
}

func TestVariableNamesOnePassword(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)
	if err := f.st.SetVariables(ctx, svc.ID, map[string]string{"OWN": "x", "BOTH": "own"}); err != nil {
		t.Fatal(err)
	}
	f.deployer.resolved.Env = map[string]string{"OWN": "x", "BOTH": "own", "FROM_OP": "op", "PORT": "3000"}
	f.deployer.resolved.OnePassword = []string{"FROM_OP"}
	got, err := f.plane.VariableNames(ctx, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []VariableName{{Name: "BOTH"}, {Name: "FROM_OP", OnePassword: true}, {Name: "OWN"}, {Name: "PORT", Injected: true}}
	if !slices.Equal(got.Variables, want) {
		t.Errorf("VariableNames() = %+v, want %+v", got.Variables, want)
	}
}
