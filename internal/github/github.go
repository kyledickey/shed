// Package github integrates shed with a single GitHub App.
//
// The App provides everything shed needs from GitHub: repository access
// through installation tokens, sign-in through OAuth, push webhooks, and CI
// status. REST calls go through go-github.
package github

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	gh "github.com/google/go-github/v92/github"
)

const (
	defaultAPIURL = "https://api.github.com/"
	defaultWebURL = "https://github.com"

	// tokenRefreshMargin is how long before expiry an installation token is
	// replaced.
	tokenRefreshMargin = 5 * time.Minute
)

// App holds the credentials of a registered GitHub App.
type App struct {
	ID            int64
	Slug          string
	ClientID      string
	ClientSecret  string
	WebhookSecret string
	// PrivateKey is the PEM-encoded RSA key used to sign app JWTs.
	PrivateKey []byte
}

// Client calls the GitHub API as an App. It is safe for concurrent use.
type Client struct {
	app    App
	key    *rsa.PrivateKey
	http   *http.Client
	apiURL string // REST API base URL, with a trailing slash.
	webURL string // Web base URL, without a trailing slash.

	mu     sync.Mutex
	tokens map[int64]installationToken // Guarded by mu; keyed by installation ID.
}

// installationToken is a cached installation access token.
type installationToken struct {
	value   string
	expires time.Time
}

// New returns a Client for app. A nil httpClient means a client with a
// 30-second timeout. The client is never modified.
func New(app App, httpClient *http.Client) (*Client, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM(app.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("github: parse app private key: %w", err)
	}
	return &Client{
		app:    app,
		key:    key,
		http:   orDefault(httpClient),
		apiURL: defaultAPIURL,
		webURL: defaultWebURL,
		tokens: make(map[int64]installationToken),
	}, nil
}

// App returns the credentials the Client was created with.
func (c *Client) App() App {
	return c.app
}

// Lookup fetches the App's own record from GitHub, which confirms that the App
// ID and private key belong together, and returns the App's slug and client
// ID.
func (c *Client) Lookup(ctx context.Context) (slug, clientID string, err error) {
	client, err := c.appClient()
	if err != nil {
		return "", "", err
	}
	app, _, err := client.Apps.Get(ctx, "")
	if err != nil {
		return "", "", fmt.Errorf("github: get app %d: %w", c.app.ID, err)
	}
	return app.GetSlug(), app.GetClientID(), nil
}

func orDefault(hc *http.Client) *http.Client {
	if hc == nil {
		return &http.Client{Timeout: 30 * time.Second}
	}
	return hc
}

// api returns a go-github client that authenticates with token, or sends
// unauthenticated requests if token is empty.
func api(hc *http.Client, apiURL, token string) (*gh.Client, error) {
	clone := *hc // go-github installs its auth transport on the client it is given.
	opts := []gh.ClientOptionsFunc{gh.WithHTTPClient(&clone), gh.WithURLs(&apiURL, nil)}
	if token != "" {
		opts = append(opts, gh.WithAuthToken(token))
	}
	return gh.NewClient(opts...)
}

// appClient returns a client authenticated as the App itself.
func (c *Client) appClient() (*gh.Client, error) {
	now := time.Now()
	token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer:    strconv.FormatInt(c.app.ID, 10),
		IssuedAt:  jwt.NewNumericDate(now.Add(-time.Minute)), // Allow for clock drift.
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
	}).SignedString(c.key)
	if err != nil {
		return nil, fmt.Errorf("github: sign app JWT: %w", err)
	}
	return api(c.http, c.apiURL, token)
}

// installationClient returns a client authenticated as an installation.
func (c *Client) installationClient(ctx context.Context, id int64) (*gh.Client, error) {
	token, err := c.installationToken(ctx, id)
	if err != nil {
		return nil, err
	}
	return api(c.http, c.apiURL, token)
}

// installationToken returns a valid access token for an installation, reusing
// a cached one until shortly before it expires.
func (c *Client) installationToken(ctx context.Context, id int64) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.tokens[id]; ok && time.Until(t.expires) > tokenRefreshMargin {
		return t.value, nil
	}
	app, err := c.appClient()
	if err != nil {
		return "", err
	}
	tok, _, err := app.Apps.CreateInstallationToken(ctx, id, nil)
	if err != nil {
		return "", fmt.Errorf("github: create token for installation %d: %w", id, err)
	}
	t := installationToken{value: tok.GetToken(), expires: tok.GetExpiresAt().Time}
	c.tokens[id] = t
	return t.value, nil
}

// repoClient returns a client for the installation that covers the repository
// fullName (owner/name), along with the split owner and name.
func (c *Client) repoClient(ctx context.Context, fullName string) (client *gh.Client, owner, repo string, err error) {
	id, owner, repo, err := c.repoInstallation(ctx, fullName)
	if err != nil {
		return nil, "", "", err
	}
	client, err = c.installationClient(ctx, id)
	return client, owner, repo, err
}

// repoInstallation finds the installation covering the repository fullName.
func (c *Client) repoInstallation(ctx context.Context, fullName string) (id int64, owner, repo string, err error) {
	owner, repo, ok := strings.Cut(fullName, "/")
	if !ok || owner == "" || repo == "" {
		return 0, "", "", fmt.Errorf("github: invalid repository %q, want owner/name", fullName)
	}
	app, err := c.appClient()
	if err != nil {
		return 0, "", "", err
	}
	inst, _, err := app.Apps.GetRepositoryInstallation(ctx, owner, repo)
	if err != nil {
		return 0, "", "", fmt.Errorf("github: find installation for %s: %w", fullName, err)
	}
	if inst.GetID() == 0 {
		return 0, "", "", errors.New("github: installation response has no ID")
	}
	return inst.GetID(), owner, repo, nil
}
