package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/github"
	"github.com/kyledickey/shed/internal/store"
)

// githubAppKey is the settings key holding the GitHub App credentials.
const githubAppKey = "github_app"

// maxWebhookBody bounds the push payload accepted by shed.
const maxWebhookBody = 1 << 20

// GitHubHolder holds the GitHub client, which exists only once the GitHub App
// is configured. It is safe for concurrent use; the zero value holds nothing.
type GitHubHolder struct {
	mu     sync.RWMutex
	client *github.Client
}

// Get returns the client, or nil if GitHub is not configured.
func (h *GitHubHolder) Get() *github.Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.client
}

// Set installs the client.
func (h *GitHubHolder) Set(c *github.Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.client = c
}

// storedApp is the JSON form of the App credentials in the settings table.
type storedApp struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	ClientID      string `json:"clientId"`
	ClientSecret  string `json:"clientSecret"`
	WebhookSecret string `json:"webhookSecret"`
	PrivateKey    string `json:"privateKey"`
}

// loadGitHub creates the GitHub client from the stored App, or generates and
// logs the setup token if there is none.
func (s *Server) loadGitHub(ctx context.Context) error {
	raw, err := s.store.Setting(ctx, githubAppKey)
	if errors.Is(err, store.ErrNotFound) {
		s.setupToken = rand.Text()
		s.log.Warn("GitHub App is not configured; open the setup page and enter the setup token",
			"url", s.baseURL+"/setup", "token", s.setupToken)
		return nil
	}
	if err != nil {
		return fmt.Errorf("api: load GitHub App: %w", err)
	}
	var sa storedApp
	if err := json.Unmarshal([]byte(raw), &sa); err != nil {
		return fmt.Errorf("api: decode GitHub App: %w", err)
	}
	client, err := github.New(github.App{
		ID: sa.ID, Slug: sa.Slug, ClientID: sa.ClientID, ClientSecret: sa.ClientSecret,
		WebhookSecret: sa.WebhookSecret, PrivateKey: []byte(sa.PrivateKey),
	}, s.httpClient)
	if err != nil {
		return fmt.Errorf("api: load GitHub App: %w", err)
	}
	s.github.Set(client)
	return nil
}

// requireGitHub returns the GitHub client, or a 409 error until setup.
func (s *Server) requireGitHub() (*github.Client, error) {
	if c := s.github.Get(); c != nil {
		return c, nil
	}
	return nil, errorf(http.StatusConflict, "GitHub App is not configured")
}

// branchHead returns the commit at the head of a repo service's branch.
func (s *Server) branchHead(ctx context.Context, svc store.Service) (deploy.Commit, error) {
	gh, err := s.requireGitHub()
	if err != nil {
		return deploy.Commit{}, err
	}
	c, err := gh.Commit(ctx, svc.Repo, svc.Branch)
	if err != nil {
		s.log.Warn("resolve branch head", "repo", svc.Repo, "branch", svc.Branch, "err", err)
		return deploy.Commit{}, errorf(http.StatusBadRequest,
			"cannot read branch %s of %s; is the GitHub App installed on it?", svc.Branch, svc.Repo)
	}
	return deploy.Commit{SHA: c.SHA, Message: c.Message, Author: c.Author}, nil
}

func installURL(slug string) string {
	return "https://github.com/apps/" + url.PathEscape(slug) + "/installations/new"
}

func (s *Server) getSetup(w http.ResponseWriter, r *http.Request) error {
	var out setupJSON
	if c := s.github.Get(); c != nil {
		slug := c.App().Slug
		out = setupJSON{GitHubConfigured: true, AppSlug: slug, InstallURL: installURL(slug)}
	}
	return writeJSON(w, http.StatusOK, out)
}

// validSetupToken reports whether token is the current setup token.
func (s *Server) validSetupToken(token string) bool {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	return s.setupToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.setupToken)) == 1
}

// setupRedirect sends the browser back to the dashboard's setup page with an
// error code.
func setupRedirect(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/setup?error="+code, http.StatusFound)
}

var manifestForm = template.Must(template.New("manifest").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Create GitHub App</title></head>
<body>
<form id="manifest" method="post" action="https://github.com/settings/apps/new?state={{.State}}">
<input type="hidden" name="manifest" value="{{.Manifest}}">
<noscript><button type="submit">Create GitHub App</button></noscript>
</form>
<script>document.getElementById("manifest").submit()</script>
</body>
</html>
`))

// setupGitHub renders a form that posts the App manifest to GitHub. The setup
// token travels as the manifest state and comes back to setupCallback.
func (s *Server) setupGitHub(w http.ResponseWriter, r *http.Request) error {
	if s.github.Get() != nil {
		setupRedirect(w, r, "already_configured")
		return nil
	}
	token := r.URL.Query().Get("token")
	if !s.validSetupToken(token) {
		setupRedirect(w, r, "invalid_token")
		return nil
	}
	manifest, err := github.Manifest(s.baseURL, appName(s.baseURL))
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	return manifestForm.Execute(w, struct{ State, Manifest string }{token, string(manifest)})
}

// appName derives a GitHub App name, which must be unique on GitHub and at
// most 34 characters, from the dashboard host.
func appName(baseURL string) string {
	name := "shed"
	if u, err := url.Parse(baseURL); err == nil && u.Hostname() != "" {
		name += "-" + strings.ReplaceAll(u.Hostname(), ".", "-")
	}
	if len(name) > 34 {
		name = strings.TrimRight(name[:34], "-")
	}
	return name
}

// setupCallback receives the App created from the manifest, stores its
// credentials, and sends the browser on to install it.
func (s *Server) setupCallback(w http.ResponseWriter, r *http.Request) error {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if s.github.Get() != nil {
		setupRedirect(w, r, "already_configured")
		return nil
	}
	q := r.URL.Query()
	if !s.validSetupToken(q.Get("state")) {
		setupRedirect(w, r, "invalid_token")
		return nil
	}
	app, err := s.createApp(r.Context(), q.Get("code"))
	if err != nil {
		s.log.Error("GitHub App setup failed", "err", err)
		setupRedirect(w, r, "failed")
		return nil
	}
	http.Redirect(w, r, installURL(app.Slug), http.StatusFound)
	return nil
}

// createApp converts the manifest code into App credentials and saves them.
func (s *Server) createApp(ctx context.Context, code string) (github.App, error) {
	if code == "" {
		return github.App{}, errors.New("missing code")
	}
	app, err := github.ConvertManifest(ctx, s.httpClient, code)
	if err != nil {
		return github.App{}, err
	}
	client, err := github.New(app, s.httpClient)
	if err != nil {
		return github.App{}, err
	}
	return app, s.saveApp(ctx, client)
}

// importRequest is the body of POST /api/setup/github/import.
type importRequest struct {
	Token         string `json:"token"`
	AppID         int64  `json:"appId"`
	ClientID      string `json:"clientId"`
	ClientSecret  string `json:"clientSecret"`
	WebhookSecret string `json:"webhookSecret"`
	PrivateKey    string `json:"privateKey"`
}

// importApp configures an App that already exists on GitHub from credentials
// the user generated in its settings, after checking the ID and key with
// GitHub.
func (s *Server) importApp(w http.ResponseWriter, r *http.Request) error {
	var in importRequest
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if s.github.Get() != nil {
		return errorf(http.StatusConflict, "GitHub App is already configured")
	}
	if !s.validSetupToken(in.Token) {
		return errorf(http.StatusForbidden, "invalid setup token")
	}
	in.ClientID = strings.TrimSpace(in.ClientID)
	in.ClientSecret = strings.TrimSpace(in.ClientSecret)
	in.WebhookSecret = strings.TrimSpace(in.WebhookSecret)
	if in.AppID <= 0 || in.ClientID == "" || in.ClientSecret == "" || in.WebhookSecret == "" {
		return errorf(http.StatusBadRequest, "app ID, client ID, client secret, and webhook secret are required")
	}
	app := github.App{
		ID: in.AppID, ClientID: in.ClientID, ClientSecret: in.ClientSecret,
		WebhookSecret: in.WebhookSecret, PrivateKey: []byte(strings.TrimSpace(in.PrivateKey) + "\n"),
	}
	client, err := github.New(app, s.httpClient)
	if err != nil {
		return errorf(http.StatusBadRequest, "private key is not a valid PEM-encoded RSA key")
	}
	slug, clientID, err := client.Lookup(r.Context())
	if err != nil {
		s.log.Warn("GitHub App import: lookup failed", "app", in.AppID, "err", err)
		return errorf(http.StatusBadRequest, "GitHub rejected App ID %d with this private key", in.AppID)
	}
	if clientID != in.ClientID {
		return errorf(http.StatusBadRequest, "client ID does not belong to App %d", in.AppID)
	}
	app.Slug = slug
	if client, err = github.New(app, s.httpClient); err != nil {
		return err
	}
	if err := s.saveApp(r.Context(), client); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, setupJSON{GitHubConfigured: true, AppSlug: slug, InstallURL: installURL(slug)})
}

// saveApp stores the client's App credentials, installs the client, and ends
// setup. The caller holds setupMu.
func (s *Server) saveApp(ctx context.Context, client *github.Client) error {
	app := client.App()
	raw, err := json.Marshal(storedApp{
		ID: app.ID, Slug: app.Slug, ClientID: app.ClientID, ClientSecret: app.ClientSecret,
		WebhookSecret: app.WebhookSecret, PrivateKey: string(app.PrivateKey),
	})
	if err != nil {
		return err
	}
	if err := s.store.SetSetting(ctx, githubAppKey, string(raw)); err != nil {
		return err
	}
	s.github.Set(client)
	s.tokenMu.Lock()
	s.setupToken = ""
	s.tokenMu.Unlock()
	s.log.Info("GitHub App configured", "slug", app.Slug)
	return nil
}

// webhook handles GitHub deliveries: a push to a branch deploys every app
// service that tracks it.
func (s *Server) webhook(w http.ResponseWriter, r *http.Request) error {
	// Keep the deadline through net/http's post-handler body drain. The server
	// resets deadlines before reading the next request on this connection.
	controller := http.NewResponseController(w)
	if err := controller.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	gh, err := s.requireGitHub()
	if err != nil {
		return err
	}
	signature := r.Header.Get("X-Hub-Signature-256")
	if !strings.HasPrefix(signature, "sha256=") || len(signature) != 71 {
		return errorf(http.StatusUnauthorized, "invalid signature")
	}
	if _, err := hex.DecodeString(signature[7:]); err != nil {
		return errorf(http.StatusUnauthorized, "invalid signature")
	}
	if !s.webhooks.acquire(time.Now()) {
		return errorf(http.StatusTooManyRequests, "webhook capacity exceeded")
	}
	defer s.webhooks.release()
	if r.ContentLength > maxWebhookBody {
		return errorf(http.StatusRequestEntityTooLarge, "webhook body too large")
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return errorf(http.StatusRequestEntityTooLarge, "webhook body too large")
		}
		return errorf(http.StatusBadRequest, "read body: %v", err)
	}
	if !github.VerifySignature(gh.App().WebhookSecret, body, r.Header.Get("X-Hub-Signature-256")) {
		return errorf(http.StatusUnauthorized, "invalid signature")
	}
	if r.Header.Get("X-GitHub-Event") != "push" {
		w.WriteHeader(http.StatusAccepted)
		return nil
	}
	ev, err := github.ParsePush(body)
	if err != nil {
		return errorf(http.StatusBadRequest, "invalid push event")
	}
	if ev.Deleted || ev.Branch == "" || ev.SHA == "" {
		w.WriteHeader(http.StatusAccepted)
		return nil
	}
	services, err := s.store.ServicesForPush(r.Context(), ev.Repo, ev.Branch)
	if err != nil {
		return err
	}
	commit := deploy.Commit{SHA: ev.SHA, Message: ev.Message, Author: ev.Author}
	digest := sha256.Sum256(body)
	for _, svc := range services {
		key := deliveryKey{body: digest, service: svc.ID}
		duplicate, busy := s.deliveries.begin(key, time.Now())
		if duplicate {
			continue
		}
		if busy {
			return errorf(http.StatusServiceUnavailable, "delivery is already being scheduled; retry later")
		}
		_, err := s.deployer.Deploy(r.Context(), svc.ID, store.TriggerPush, commit)
		s.deliveries.finish(key, err == nil)
		if err != nil {
			s.log.Error("deploy on push", "service", svc.ID, "err", err)
			return errorf(http.StatusServiceUnavailable, "could not schedule delivery; retry later")
		}
	}
	s.log.Info("push received", "repo", ev.Repo, "branch", ev.Branch, "services", len(services))
	w.WriteHeader(http.StatusAccepted)
	return nil
}

func (s *Server) repos(w http.ResponseWriter, r *http.Request) error {
	gh, err := s.requireGitHub()
	if err != nil {
		return err
	}
	repos, err := gh.Repos(r.Context())
	if err != nil {
		return errorf(http.StatusBadGateway, "%v", err)
	}
	out := make([]repoJSON, 0, len(repos))
	for _, repo := range repos {
		out = append(out, repoJSON{FullName: repo.FullName, DefaultBranch: repo.DefaultBranch, Private: repo.Private})
	}
	return writeJSON(w, http.StatusOK, out)
}

func (s *Server) branches(w http.ResponseWriter, r *http.Request) error {
	gh, err := s.requireGitHub()
	if err != nil {
		return err
	}
	branches, err := gh.Branches(r.Context(), r.PathValue("owner")+"/"+r.PathValue("repo"))
	if err != nil {
		return errorf(http.StatusBadGateway, "%v", err)
	}
	if branches == nil {
		branches = []string{}
	}
	return writeJSON(w, http.StatusOK, branches)
}
