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
	// ErrInvalidToken is wrapped by errors for a token 1Password rejects.
	ErrInvalidToken = errors.New("onepassword: 1Password rejected the token")
	// ErrRead is wrapped by errors for a failed read of an Environment.
	ErrRead = errors.New("onepassword: 1Password could not read Environment")
)

// envReader reads the variables of Environments. The SDK's EnvironmentsAPI
// implements it.
type envReader interface {
	GetVariables(ctx context.Context, environmentID string) (sdk.GetVariablesResponse, error)
}

// Client reads 1Password Environments, each with the service account token
// given for it. It is safe for concurrent use.
//
// The SDK runs its core as WebAssembly in-process. Loading it takes about a
// second and about 110 MB of memory, which the SDK keeps for the life of the
// process. Client signs in once per token and reuses that SDK client until a
// read with it fails.
type Client struct {
	version string
	log     *slog.Logger
	open    func(ctx context.Context, token string) (envReader, error)

	mu      sync.Mutex
	readers map[string]envReader // by token
}

// New returns a Client that reports version as shed's version to 1Password.
func New(version string, log *slog.Logger) *Client {
	c := &Client{version: version, log: log, readers: make(map[string]envReader)}
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
	return sdkReader{cl}, nil
}

// sdkReader holds the SDK client itself, not just its EnvironmentsAPI: the SDK
// ends the client's session in the core when the *sdk.Client is garbage
// collected, after which reads fail with "invalid client id".
type sdkReader struct{ client *sdk.Client }

func (r sdkReader) GetVariables(ctx context.Context, environmentID string) (sdk.GetVariablesResponse, error) {
	return r.client.Environments().GetVariables(ctx, environmentID)
}

// Preload signs in with token in the background, which loads the SDK core,
// so the first read does not wait for it. A failure is logged; the next read
// tries again.
func (c *Client) Preload(ctx context.Context, token string) {
	go func() {
		if _, err := c.reader(ctx, token); err != nil {
			c.log.Warn("load 1Password", "err", err)
		}
	}()
}

// Variables returns the variables of an Environment by name, read with token.
// It returns an error wrapping ErrInvalidToken if 1Password rejects the
// token, and one wrapping ErrRead if it fails the read.
func (c *Client) Variables(ctx context.Context, token, environmentID string) (map[string]string, error) {
	r, err := c.reader(ctx, token)
	if err != nil {
		return nil, err
	}
	resp, err := r.GetVariables(ctx, environmentID)
	if err != nil {
		c.mu.Lock()
		if c.readers[token] == r {
			delete(c.readers, token)
		}
		c.mu.Unlock()
		return nil, fmt.Errorf("%w %s: %w", ErrRead, environmentID, err)
	}
	vars := make(map[string]string, len(resp.Variables))
	for _, v := range resp.Variables {
		vars[v.Name] = v.Value
	}
	return vars, nil
}

// reader returns the SDK client for token, signing in if needed.
func (c *Client) reader(ctx context.Context, token string) (envReader, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := c.readers[token]; ok {
		return r, nil
	}
	r, err := c.open(ctx, token)
	var limited *sdk.RateLimitExceededError
	switch {
	case err == nil:
		c.readers[token] = r
		return r, nil
	case ctx.Err() != nil, errors.As(err, &limited):
		return nil, fmt.Errorf("onepassword: sign in to 1Password: %w", err)
	default:
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
}
