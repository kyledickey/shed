package onepassword

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"testing"

	sdk "github.com/1password/onepassword-sdk-go"
)

type memTokens struct{ token string }

func (m *memTokens) Token(context.Context) (string, error) { return m.token, nil }

func (m *memTokens) SetToken(_ context.Context, token string) error {
	m.token = token
	return nil
}

// fake1Password accepts the token "good" and serves envs.
type fake1Password struct {
	envs    map[string][]sdk.EnvironmentVariable
	opens   int
	readErr error
}

func (f *fake1Password) open(_ context.Context, token string) (envReader, error) {
	f.opens++
	if token != "good" {
		return nil, errors.New("invalid service account token")
	}
	return f, nil
}

func (f *fake1Password) GetVariables(_ context.Context, id string) (sdk.GetVariablesResponse, error) {
	if f.readErr != nil {
		return sdk.GetVariablesResponse{}, f.readErr
	}
	return sdk.GetVariablesResponse{Variables: f.envs[id]}, nil
}

func newClient(token string) (*Client, *memTokens, *fake1Password) {
	tokens := &memTokens{token: token}
	fake := &fake1Password{envs: map[string][]sdk.EnvironmentVariable{
		"env1": {{Name: "API_KEY", Value: "k"}, {Name: "DEBUG", Value: "1"}},
	}}
	c := New(tokens, "test", slog.New(slog.DiscardHandler))
	c.open = fake.open
	return c, tokens, fake
}

func TestConnectSavesOnlyAcceptedTokens(t *testing.T) {
	ctx := context.Background()
	c, tokens, _ := newClient("")

	if err := c.Connect(ctx, "bad"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Connect(bad) = %v, want ErrInvalidToken", err)
	}
	if ok, _ := c.Connected(ctx); ok || tokens.token != "" {
		t.Fatalf("rejected token saved: %q", tokens.token)
	}

	if err := c.Connect(ctx, "good"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := c.Connected(ctx); !ok || tokens.token != "good" {
		t.Fatalf("token = %q, want good", tokens.token)
	}

	if err := c.Connect(ctx, "bad"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Connect(bad) = %v, want ErrInvalidToken", err)
	}
	if tokens.token != "good" {
		t.Errorf("rejected token replaced the saved one: %q", tokens.token)
	}

	if err := c.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, _ := c.Connected(ctx); ok {
		t.Error("connected after Disconnect")
	}
	if _, err := c.Variables(ctx, "env1"); !errors.Is(err, ErrNotConnected) {
		t.Errorf("Variables after Disconnect = %v, want ErrNotConnected", err)
	}
}

func TestVariables(t *testing.T) {
	ctx := context.Background()
	c, _, fake := newClient("")
	if _, err := c.Variables(ctx, "env1"); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("Variables without token = %v, want ErrNotConnected", err)
	}
	if fake.opens != 0 {
		t.Errorf("signed in %d times without a token", fake.opens)
	}

	c, _, fake = newClient("good")
	for range 2 {
		got, err := c.Variables(ctx, "env1")
		if err != nil {
			t.Fatal(err)
		}
		if want := map[string]string{"API_KEY": "k", "DEBUG": "1"}; !maps.Equal(got, want) {
			t.Errorf("Variables = %v, want %v", got, want)
		}
	}
	if fake.opens != 1 {
		t.Errorf("signed in %d times for two reads, want 1", fake.opens)
	}

	fake.readErr = errors.New("network down")
	if _, err := c.Variables(ctx, "env1"); err == nil {
		t.Fatal("Variables succeeded on a failed read")
	}
	fake.readErr = nil
	if _, err := c.Variables(ctx, "env1"); err != nil {
		t.Fatal(err)
	}
	if fake.opens != 2 {
		t.Errorf("signed in %d times, want the client rebuilt once after a failed read", fake.opens)
	}
}

func TestVariablesWithRejectedSavedToken(t *testing.T) {
	c, _, _ := newClient("revoked")
	if _, err := c.Variables(context.Background(), "env1"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("Variables = %v, want ErrInvalidToken", err)
	}
}
