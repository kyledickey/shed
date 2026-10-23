package onepassword

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"testing"

	sdk "github.com/1password/onepassword-sdk-go"
)

// fake1Password accepts the tokens in envs, each of which reads its own
// Environments.
type fake1Password struct {
	envs    map[string]map[string][]sdk.EnvironmentVariable // by token, then Environment ID
	opens   map[string]int
	readErr error
}

type fakeReader struct {
	f     *fake1Password
	token string
}

func (f *fake1Password) open(_ context.Context, token string) (envReader, error) {
	f.opens[token]++
	if _, ok := f.envs[token]; !ok {
		return nil, errors.New("invalid service account token")
	}
	return fakeReader{f, token}, nil
}

func (r fakeReader) GetVariables(_ context.Context, id string) (sdk.GetVariablesResponse, error) {
	if r.f.readErr != nil {
		return sdk.GetVariablesResponse{}, r.f.readErr
	}
	vars, ok := r.f.envs[r.token][id]
	if !ok {
		return sdk.GetVariablesResponse{}, errors.New("403 forbidden")
	}
	return sdk.GetVariablesResponse{Variables: vars}, nil
}

func newClient() (*Client, *fake1Password) {
	fake := &fake1Password{
		envs: map[string]map[string][]sdk.EnvironmentVariable{
			"web": {"env1": {{Name: "API_KEY", Value: "k"}, {Name: "DEBUG", Value: "1"}}},
			"db":  {"env2": {{Name: "PASSWORD", Value: "p"}}},
		},
		opens: make(map[string]int),
	}
	c := New("test", slog.New(slog.DiscardHandler))
	c.open = fake.open
	return c, fake
}

func TestVariables(t *testing.T) {
	ctx := context.Background()
	c, fake := newClient()
	for range 2 {
		got, err := c.Variables(ctx, "web", "env1")
		if err != nil {
			t.Fatal(err)
		}
		if want := map[string]string{"API_KEY": "k", "DEBUG": "1"}; !maps.Equal(got, want) {
			t.Errorf("Variables(web, env1) = %v, want %v", got, want)
		}
	}
	if got, err := c.Variables(ctx, "db", "env2"); err != nil || got["PASSWORD"] != "p" {
		t.Errorf("Variables(db, env2) = %v, %v; want PASSWORD=p", got, err)
	}
	if fake.opens["web"] != 1 || fake.opens["db"] != 1 {
		t.Errorf("sign-ins = %v, want one per token", fake.opens)
	}

	if _, err := c.Variables(ctx, "web", "env2"); !errors.Is(err, ErrRead) {
		t.Errorf("Variables(web, env2) = %v, want ErrRead for an Environment the token cannot read", err)
	}
	if _, err := c.Variables(ctx, "db", "env2"); err != nil {
		t.Fatal(err)
	}
	if fake.opens["web"] != 1 || fake.opens["db"] != 1 {
		t.Errorf("sign-ins = %v; a failed read of one token rebuilt another's client", fake.opens)
	}
}

func TestVariablesRebuildsAfterFailedRead(t *testing.T) {
	ctx := context.Background()
	c, fake := newClient()
	if _, err := c.Variables(ctx, "web", "env1"); err != nil {
		t.Fatal(err)
	}
	fake.readErr = errors.New("network down")
	if _, err := c.Variables(ctx, "web", "env1"); !errors.Is(err, ErrRead) {
		t.Fatalf("Variables on a failed read = %v, want ErrRead", err)
	}
	fake.readErr = nil
	if _, err := c.Variables(ctx, "web", "env1"); err != nil {
		t.Fatal(err)
	}
	if fake.opens["web"] != 2 {
		t.Errorf("signed in %d times, want the client rebuilt once after a failed read", fake.opens["web"])
	}
}

func TestVariablesWithRejectedToken(t *testing.T) {
	c, _ := newClient()
	if _, err := c.Variables(context.Background(), "revoked", "env1"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("Variables = %v, want ErrInvalidToken", err)
	}
}
