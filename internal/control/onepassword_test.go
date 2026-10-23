package control

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/onepassword"
	"github.com/kyledickey/shed/internal/store"
)

// fakeOnePassword serves envs, each to the token "<id>-token" only.
type fakeOnePassword struct {
	envs    map[string]map[string]string
	reads   int
	readErr error
}

func (f *fakeOnePassword) Variables(_ context.Context, token, id string) (map[string]string, error) {
	f.reads++
	if f.readErr != nil {
		return nil, f.readErr
	}
	if token == "bad" {
		return nil, fmt.Errorf("%w: invalid service account token", onepassword.ErrInvalidToken)
	}
	if token != id+"-token" {
		return nil, fmt.Errorf("%w %s: 403 forbidden", onepassword.ErrRead, id)
	}
	return f.envs[id], nil
}

func newOnePasswordFixture(t *testing.T) (*fixture, *fakeOnePassword) {
	t.Helper()
	f := newFixture(t)
	op := &fakeOnePassword{envs: map[string]map[string]string{"env1": {"API_KEY": "k"}, "env2": {}}}
	f.plane.onePassword = op
	return f, op
}

func TestLinkOnePassword(t *testing.T) {
	f, op := newOnePasswordFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)
	unlinked := func() {
		t.Helper()
		if _, err := f.st.OnePasswordLink(ctx, svc.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("link saved: %v", err)
		}
	}

	for _, tt := range []struct{ env, token, want string }{
		{"ABC", "env1-token", "1Password Environment ID"},
		{"a-b", "env1-token", "1Password Environment ID"},
		{strings.Repeat("a", 65), "env1-token", "1Password Environment ID"},
		{"env1", "  ", "a service account token is required"},
		{"env1", "bad", "1Password rejected the token: invalid service account token"},
		{"env1", "env2-token", "1Password could not read Environment env1: 403 forbidden"},
	} {
		_, err := f.plane.LinkOnePassword(ctx, svc.ID, tt.env, tt.token)
		wantKind(t, err, ErrInvalid, tt.want)
		unlinked()
	}

	got, err := f.plane.LinkOnePassword(ctx, svc.ID, " env1 ", " env1-token ")
	if err != nil || got.OnePassword == nil || *got.OnePassword != (OnePasswordLink{Environment: "env1", HasToken: true}) {
		t.Fatalf("LinkOnePassword() = %+v, %v; want env1 with a token", got.OnePassword, err)
	}
	if l, _ := f.st.OnePasswordLink(ctx, svc.ID); l.Token != "env1-token" {
		t.Errorf("saved token = %q, want the trimmed token", l.Token)
	}

	// A blank token keeps the saved one, which is checked against the new
	// Environment.
	_, err = f.plane.LinkOnePassword(ctx, svc.ID, "env2", "")
	wantKind(t, err, ErrInvalid, "could not read Environment env2")
	if l, _ := f.st.OnePasswordLink(ctx, svc.ID); l.Environment != "env1" || l.Token != "env1-token" {
		t.Errorf("link after a failed relink = %+v, want env1 unchanged", l)
	}
	reads := op.reads
	if got, err := f.plane.LinkOnePassword(ctx, svc.ID, "env1", ""); err != nil || !got.OnePassword.HasToken {
		t.Fatalf("relink keeping the token = %+v, %v", got.OnePassword, err)
	}
	if op.reads != reads+1 {
		t.Errorf("relinking read 1Password %d times, want 1", op.reads-reads)
	}

	got, err = f.plane.UnlinkOnePassword(ctx, svc.ID)
	if err != nil || got.OnePassword != nil {
		t.Fatalf("UnlinkOnePassword() = %+v, %v; want no link", got.OnePassword, err)
	}
	unlinked()
	_, err = f.plane.LinkOnePassword(ctx, svc.ID, "env1", "")
	wantKind(t, err, ErrInvalid, "a service account token is required")

	_, err = f.plane.LinkOnePassword(ctx, "nope", "env1", "env1-token")
	wantKind(t, err, ErrNotFound, "not found")
	_, err = f.plane.UnlinkOnePassword(ctx, "nope")
	wantKind(t, err, ErrNotFound, "not found")
}

func TestLinkOnePasswordKeepsMigratedEnvironment(t *testing.T) {
	f, _ := newOnePasswordFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)
	if err := f.st.SetOnePasswordLink(ctx, store.OnePasswordLink{ServiceID: svc.ID, Environment: "env1"}); err != nil {
		t.Fatal(err)
	}
	if got, err := f.plane.Service(ctx, svc.ID); err != nil || got.OnePassword == nil || got.OnePassword.HasToken {
		t.Fatalf("Service().OnePassword = %+v, %v; want a link with no token", got.OnePassword, err)
	}
	_, err := f.plane.LinkOnePassword(ctx, svc.ID, "env1", "")
	wantKind(t, err, ErrInvalid, "a service account token is required")
	if got, err := f.plane.LinkOnePassword(ctx, svc.ID, "env1", "env1-token"); err != nil || !got.OnePassword.HasToken {
		t.Errorf("adding a token = %+v, %v", got.OnePassword, err)
	}
}

func TestOnePasswordVariables(t *testing.T) {
	f, op := newOnePasswordFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)

	if got, err := f.plane.OnePasswordVariables(ctx, svc.ID); err != nil || len(got) != 0 || got == nil {
		t.Errorf("unlinked = %v, %v; want {}", got, err)
	}
	if err := f.st.SetOnePasswordLink(ctx, store.OnePasswordLink{ServiceID: svc.ID, Environment: "env1"}); err != nil {
		t.Fatal(err)
	}
	_, err := f.plane.OnePasswordVariables(ctx, svc.ID)
	wantKind(t, err, ErrConflict, "add one on its Variables tab")

	if _, err := f.plane.LinkOnePassword(ctx, svc.ID, "env1", "env1-token"); err != nil {
		t.Fatal(err)
	}
	if got, err := f.plane.OnePasswordVariables(ctx, svc.ID); err != nil || !maps.Equal(got, op.envs["env1"]) {
		t.Errorf("linked = %v, %v; want %v", got, err, op.envs["env1"])
	}
	op.readErr = fmt.Errorf("%w env1: rate limited", onepassword.ErrRead)
	_, err = f.plane.OnePasswordVariables(ctx, svc.ID)
	wantKind(t, err, ErrUpstream, "rate limited")
	op.readErr = fmt.Errorf("%w: revoked", onepassword.ErrInvalidToken)
	_, err = f.plane.OnePasswordVariables(ctx, svc.ID)
	wantKind(t, err, ErrUpstream, "revoked")

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

func TestResolvedVariablesWithoutOnePasswordToken(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	f.deployer.resolveErr = fmt.Errorf("resolve variables: %w", deploy.ErrNoOnePasswordToken)
	_, err := f.plane.ResolvedVariables(context.Background(), svc.ID)
	wantKind(t, err, ErrConflict, "has no token")
}
