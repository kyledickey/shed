// Package onepassword reads 1Password Environments with a service account
// token, through the official 1Password Go SDK.
package onepassword

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	sdk "github.com/1password/onepassword-sdk-go"
)

var (
	// ErrNotConnected is returned while no service account token is saved.
	ErrNotConnected = errors.New("onepassword: 1Password is not connected")
	// ErrInvalidToken is wrapped by errors for a token 1Password rejects.
	ErrInvalidToken = errors.New("onepassword: 1Password rejected the token")
)

// Tokens persists the service account token. Token returns "" when none is
// saved; SetToken("") removes it.
type Tokens interface {
	Token(ctx context.Context) (string, error)
	SetToken(ctx context.Context, token string) error
}

// envReader reads the variables of Environments. The SDK's EnvironmentsAPI
// implements it.
type envReader interface {
	GetVariables(ctx context.Context, environmentID string) (sdk.GetVariablesResponse, error)
}

// Client reads 1Password Environments with the saved service account token.
// It is safe for concurrent use.
//
// The SDK runs its core as WebAssembly in-process. Loading it takes about a
// second and about 110 MB of memory, which the SDK keeps for the life of the
// process, so Client builds an SDK client only once a token is saved and
// reuses it.
type Client struct {
	tokens  Tokens
	version string
	log     *slog.Logger
	open    func(ctx context.Context, token string) (envReader, error)

	mu     sync.Mutex
	reader envReader // for the saved token; nil until built or after a failed read
}

// New returns a Client that keeps its token in tokens and reports version as
// shed's version to 1Password.
func New(tokens Tokens, version string, log *slog.Logger) *Client {
	c := &Client{tokens: tokens, version: version, log: log}
	c.open = c.openSDK
	return c
}

func (c *Client) openSDK(ctx context.Context, token string) (envReader, error) {
	cl, err := sdk.NewClient(ctx,
		sdk.WithServiceAccountToken(token),
		sdk.WithIntegrationInfo("shed", c.version))
	if err != nil {
		return nil, err
	}
	return cl.Environments(), nil
}

// Preload builds the SDK client in the background if a token is saved, so
// the first read does not wait for it. A failure is logged; the next read
// tries again.
func (c *Client) Preload(ctx context.Context) {
	go func() {
		if _, err := c.client(ctx); err != nil && !errors.Is(err, ErrNotConnected) {
			c.log.Warn("load 1Password", "err", err)
		}
	}()
}

// Connected reports whether a token is saved.
func (c *Client) Connected(ctx context.Context) (bool, error) {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return false, fmt.Errorf("onepassword: read token: %w", err)
	}
	return token != "", nil
}

// Connect signs in to 1Password with token and, only if that succeeds, saves
// it in place of the current one. It returns an error wrapping
// ErrInvalidToken if 1Password rejects the token.
func (c *Client) Connect(ctx context.Context, token string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, err := c.signIn(ctx, token)
	if err != nil {
		return err
	}
	if err := c.tokens.SetToken(ctx, token); err != nil {
		return fmt.Errorf("onepassword: save token: %w", err)
	}
	c.reader = r
	return nil
}

// Disconnect forgets the saved token.
func (c *Client) Disconnect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.tokens.SetToken(ctx, ""); err != nil {
		return fmt.Errorf("onepassword: remove token: %w", err)
	}
	c.reader = nil
	return nil
}

// Variables returns the variables of an Environment by name. It returns
// ErrNotConnected if no token is saved.
func (c *Client) Variables(ctx context.Context, environmentID string) (map[string]string, error) {
	r, err := c.client(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := r.GetVariables(ctx, environmentID)
	if err != nil {
		c.mu.Lock()
		if c.reader == r {
			c.reader = nil
		}
		c.mu.Unlock()
		return nil, fmt.Errorf("onepassword: read Environment %s: %w", environmentID, err)
	}
	vars := make(map[string]string, len(resp.Variables))
	for _, v := range resp.Variables {
		vars[v.Name] = v.Value
	}
	return vars, nil
}

// client returns the SDK client for the saved token, building it if needed.
func (c *Client) client(ctx context.Context) (envReader, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.reader != nil {
		return c.reader, nil
	}
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("onepassword: read token: %w", err)
	}
	if token == "" {
		return nil, ErrNotConnected
	}
	if c.reader, err = c.signIn(ctx, token); err != nil {
		return nil, err
	}
	return c.reader, nil
}

// signIn builds an SDK client for token, which authenticates it.
func (c *Client) signIn(ctx context.Context, token string) (envReader, error) {
	r, err := c.open(ctx, token)
	var limited *sdk.RateLimitExceededError
	switch {
	case err == nil:
		return r, nil
	case ctx.Err() != nil, errors.As(err, &limited):
		return nil, fmt.Errorf("onepassword: sign in: %w", err)
	default:
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
}
