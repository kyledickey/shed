package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"mime"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// OAuth 2.1 authorization server for agents. GitHub sign-in identifies the
// user; shed issues its own opaque tokens, stored hashed.
const (
	resourcePath = "/mcp" // The protected resource: the MCP endpoint.

	requestTTL      = 10 * time.Minute // pending authorization request
	codeTTL         = time.Minute
	accessTTL       = time.Hour
	refreshTTL      = 30 * 24 * time.Hour
	staleClientTTL  = 24 * time.Hour // unused registrations
	cleanupInterval = time.Hour

	maxClients      = 1000
	registerBurst   = 10        // registrations per client address...
	registerPeriod  = time.Hour // ...regained over this period
	maxRegistrants  = 4096      // client addresses tracked for registration
	maxPending      = 256
	maxRedirectURIs = 10
	maxNameLen      = 100
	maxURILen       = 2000
	maxBodyBytes    = 64 << 10

	accessPrefix  = "shed_at_"
	refreshPrefix = "shed_rt_"
)

// ScopeRead is the scope that grants read-only access to the MCP server.
const ScopeRead = "read"

var supportedScopes = []string{ScopeRead}

// ErrReplayed is returned by an [OAuthStore] when an authorization code or a
// rotated refresh token is presented again. The store has revoked the grant
// by then.
var ErrReplayed = errors.New("auth: oauth credential replayed")

// Client is an OAuth client registered through dynamic client registration.
// Every client is public: it has no secret.
type Client struct {
	ID           string
	Name         string
	URI          string
	RedirectURIs []string
	CreatedAt    time.Time
}

// Code is an authorization code, keyed by the hash of its value.
type Code struct {
	Hash        string
	ClientID    string
	GitHubID    int64
	RedirectURI string
	Challenge   string // PKCE S256 code challenge
	Scope       string
	Resource    string
	ExpiresAt   time.Time
}

// Grant is an approved authorization: a user let a client act for them.
type Grant struct {
	ID          string
	ClientID    string
	ClientName  string
	GitHubID    int64
	RedirectURI string
	Scope       string // space-separated
	Resource    string // audience of its access tokens
	CreatedAt   time.Time
	LastUsedAt  *time.Time
}

// Token is an access or refresh token of a grant, keyed by the hash of its
// value.
type Token struct {
	Hash      string
	Refresh   bool
	ExpiresAt time.Time
}

// OAuthStore persists OAuth clients, codes, grants, and tokens.
type OAuthStore interface {
	// CreateClient stores a client and returns it with its ID and creation
	// time.
	CreateClient(ctx context.Context, c Client) (Client, error)
	// Client returns a registered client, and whether it exists.
	Client(ctx context.Context, id string) (Client, bool, error)
	// CountClients returns the number of registered clients.
	CountClients(ctx context.Context) (int, error)
	// CreateCode stores an authorization code.
	CreateCode(ctx context.Context, c Code) error
	// RedeemCode spends the unexpired code with the given hash. issue
	// validates it and returns the grant and tokens to create; its error is
	// returned and the code is spent anyway. It reports false if the code is
	// unknown or expired. Redeeming a code twice revokes the grant it
	// produced and returns ErrReplayed.
	RedeemCode(ctx context.Context, hash string, issue func(Code) (Grant, []Token, error)) (Grant, bool, error)
	// RotateRefreshToken spends the unexpired refresh token with the given
	// hash. issue validates its grant and user and returns the tokens that
	// replace it; if it fails, nothing changes. It reports false if the
	// token is unknown or expired. Presenting a rotated token revokes its
	// grant and returns ErrReplayed.
	RotateRefreshToken(ctx context.Context, hash string, issue func(Grant, User) ([]Token, error)) (Grant, bool, error)
	// AccessToken returns the grant and user of an unexpired access token,
	// and whether it exists.
	AccessToken(ctx context.Context, hash string) (Grant, User, bool, error)
	// TouchGrant records that a grant was used at the given time.
	TouchGrant(ctx context.Context, id string, at time.Time) error
	// Grants returns a user's live grants, newest first.
	Grants(ctx context.Context, githubID int64) ([]Grant, error)
	// DeleteGrant revokes a user's grant, reporting whether it existed.
	DeleteGrant(ctx context.Context, id string, githubID int64) (bool, error)
	// RevokeToken revokes the grant of a token, if the token was issued to
	// clientID or clientID is empty.
	RevokeToken(ctx context.Context, hash, clientID string) error
	// DeleteExpiredOAuth removes expired codes and tokens, dead grants, and
	// clients created before staleBefore that were never used.
	DeleteExpiredOAuth(ctx context.Context, staleBefore time.Time) error
	// DeleteDisallowedGrants revokes grants and codes of logins outside
	// allowed.
	DeleteDisallowedGrants(ctx context.Context, allowed []string) error
}

// Resource returns the URL of the resource that bearer tokens are issued for:
// the MCP endpoint.
func (a *Auth) Resource() string {
	return a.baseURL + resourcePath
}

// ProtectedResourceMetadata serves the RFC 9728 metadata of the MCP endpoint.
func (a *Auth) ProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if !allowCORS(w, r, http.MethodGet) {
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"resource":                 a.Resource(),
		"authorization_servers":    []string{a.baseURL},
		"scopes_supported":         supportedScopes,
		"bearer_methods_supported": []string{"header"},
	})
}

// AuthorizationServerMetadata serves the RFC 8414 authorization server
// metadata.
func (a *Auth) AuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	if !allowCORS(w, r, http.MethodGet) {
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"issuer":                                     a.baseURL,
		"authorization_endpoint":                     a.baseURL + "/oauth/authorize",
		"token_endpoint":                             a.baseURL + "/oauth/token",
		"registration_endpoint":                      a.baseURL + "/oauth/register",
		"revocation_endpoint":                        a.baseURL + "/oauth/revoke",
		"response_types_supported":                   []string{"code"},
		"grant_types_supported":                      []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":           []string{"S256"},
		"token_endpoint_auth_methods_supported":      []string{"none"},
		"revocation_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                           supportedScopes,
	})
}

// Register implements RFC 7591 dynamic client registration for public
// clients.
func (a *Auth) Register(w http.ResponseWriter, r *http.Request) {
	if !allowCORS(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "Content-Type must be application/json")
		return
	}
	// Other metadata, such as the requested token endpoint authentication
	// method, grant types, or scope, is ignored: every client is registered
	// as a public authorization code client with the read scope, and the
	// response says so (RFC 7591 section 3.2.1).
	var in struct {
		RedirectURIs []string `json:"redirect_uris"`
		ClientName   string   `json:"client_name"`
		ClientURI    string   `json:"client_uri"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&in); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "invalid JSON body")
		return
	}
	if len(in.RedirectURIs) == 0 || len(in.RedirectURIs) > maxRedirectURIs {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri",
			fmt.Sprintf("between 1 and %d redirect_uris are required", maxRedirectURIs))
		return
	}
	for _, u := range in.RedirectURIs {
		if err := checkRedirectURI(u); err != nil {
			oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
			return
		}
	}
	var problem string
	switch {
	case len(in.ClientName) > maxNameLen || strings.ContainsFunc(in.ClientName, isControl):
		problem = fmt.Sprintf("client_name must be at most %d printable characters", maxNameLen)
	case in.ClientURI != "" && !httpsURL(in.ClientURI):
		problem = "client_uri must be an https URL"
	}
	if problem != "" {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", problem)
		return
	}

	if !a.registrations.allow(clientAddr(r), time.Now()) {
		w.Header().Set("Retry-After", fmt.Sprint(int(registerPeriod.Seconds())/registerBurst))
		oauthError(w, http.StatusTooManyRequests, "temporarily_unavailable", "too many registrations; try again later")
		return
	}

	ctx := r.Context()
	a.registerMu.Lock()
	defer a.registerMu.Unlock()
	if err := a.cleanUp(ctx); err != nil {
		a.log.Warn("remove expired oauth rows", "err", err)
	}
	n, err := a.store.CountClients(ctx)
	if err == nil && n >= maxClients {
		// Make room by dropping registrations that never led to an
		// authorization.
		if err = a.store.DeleteExpiredOAuth(ctx, time.Now().Add(-requestTTL)); err == nil {
			n, err = a.store.CountClients(ctx)
		}
	}
	if err != nil {
		a.log.Error("register oauth client", "err", err)
		oauthError(w, http.StatusInternalServerError, "server_error", "")
		return
	}
	if n >= maxClients {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "too many registered clients")
		return
	}
	c, err := a.store.CreateClient(ctx, Client{
		Name:         strings.TrimSpace(in.ClientName),
		URI:          in.ClientURI,
		RedirectURIs: in.RedirectURIs,
	})
	if err != nil {
		a.log.Error("register oauth client", "err", err)
		oauthError(w, http.StatusInternalServerError, "server_error", "")
		return
	}
	a.log.Info("oauth client registered", "client", c.ID, "name", c.Name)
	out := map[string]any{
		"client_id":                  c.ID,
		"client_id_issued_at":        c.CreatedAt.Unix(),
		"client_name":                c.Name,
		"redirect_uris":              c.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"scope":                      ScopeRead,
	}
	if c.URI != "" {
		out["client_uri"] = c.URI
	}
	writeJSONStatus(w, http.StatusCreated, out)
}

// Authorize starts an authorization: it validates the request, sends a user
// without a session to sign in first, and otherwise hands the request to the
// dashboard's consent page. Invalid requests get an error page, never a
// redirect to the client.
func (a *Auth) Authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Has("request") {
		a.resumeAuthorize(w, r, q.Get("request"))
		return
	}
	for k, v := range q {
		if len(v) > 1 {
			errorPage(w, http.StatusBadRequest, fmt.Sprintf("The %s parameter is repeated.", k))
			return
		}
	}
	client, ok, err := a.store.Client(r.Context(), q.Get("client_id"))
	if err != nil {
		a.log.Error("look up oauth client", "err", err)
		errorPage(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		return
	}
	if !ok {
		errorPage(w, http.StatusBadRequest, "This app is not registered with shed. Reconnect it to register again.")
		return
	}
	redirect := q.Get("redirect_uri")
	if !client.allowsRedirect(redirect) {
		errorPage(w, http.StatusBadRequest, "The app asked to return to an address it did not register.")
		return
	}

	// Anyone can register a client with any https redirect URI, so errors
	// are shown here rather than redirected: redirecting them would let
	// shed bounce visitors to an arbitrary site without their consent
	// (RFC 9700 section 4.11.2).
	scope, scopeOK := parseScope(q.Get("scope"))
	resource := q.Get("resource")
	var problem string
	switch {
	case q.Get("response_type") != "code":
		problem = `The app asked for an unsupported response type; it must be "code".`
	case q.Get("code_challenge_method") != "S256" || !validChallenge(q.Get("code_challenge")):
		problem = "The app did not use PKCE with code_challenge_method S256."
	case !scopeOK:
		problem = `The app asked for an unknown scope; the only scope is "read".`
	case resource != "" && resource != a.Resource():
		problem = "The app asked for access to another resource than " + a.Resource() + "."
	}
	if problem != "" {
		errorPage(w, http.StatusBadRequest, problem)
		return
	}

	req := &request{
		client:      client,
		redirectURI: redirect,
		state:       q.Get("state"),
		challenge:   q.Get("code_challenge"),
		scope:       scope,
		resource:    a.Resource(),
	}
	user, ok, err := a.sessionUser(r)
	if err != nil {
		a.log.Error("look up session", "err", err)
		errorPage(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		return
	}
	if !ok {
		// The request waits here rather than riding along in the sign-in
		// return path: a full authorize URL can outgrow that limit.
		id, err := a.signins.add(req)
		if err != nil {
			a.log.Error("store authorization request", "err", err)
			errorPage(w, http.StatusInternalServerError, "Something went wrong. Try again.")
			return
		}
		a.redirectToSignIn(w, r, id)
		return
	}
	a.awaitConsent(w, r, req, user)
}

// resumeAuthorize continues an authorization request that waited for the
// user to sign in.
func (a *Auth) resumeAuthorize(w http.ResponseWriter, r *http.Request, id string) {
	user, ok, err := a.sessionUser(r)
	if err != nil {
		a.log.Error("look up session", "err", err)
		errorPage(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		return
	}
	if !ok {
		a.redirectToSignIn(w, r, id)
		return
	}
	req, ok := a.signins.take(id, 0)
	if !ok {
		errorPage(w, http.StatusBadRequest, "This connection request expired. Start connecting again from the app.")
		return
	}
	a.awaitConsent(w, r, req, user)
}

func (a *Auth) redirectToSignIn(w http.ResponseWriter, r *http.Request, id string) {
	next := "/oauth/authorize?request=" + url.QueryEscape(id)
	http.Redirect(w, r, "/api/auth/login?next="+url.QueryEscape(next), http.StatusFound)
}

// awaitConsent binds req to user and sends the browser to the consent page.
func (a *Auth) awaitConsent(w http.ResponseWriter, r *http.Request, req *request, user User) {
	req.githubID = user.GitHubID
	id, err := a.requests.add(req)
	if err != nil {
		a.log.Error("store authorization request", "err", err)
		errorPage(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		return
	}
	http.Redirect(w, r, "/authorize?request="+id, http.StatusFound)
}

// AuthorizationRequest returns a pending authorization request of the
// signed-in user, for the consent page.
func (a *Auth) AuthorizationRequest(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	req, ok := a.requests.get(r.PathValue("id"), user.GitHubID)
	if !ok {
		writeError(w, http.StatusNotFound, "authorization request not found or expired")
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"id":           r.PathValue("id"),
		"clientName":   req.client.Name,
		"clientUri":    req.client.URI,
		"redirectHost": redirectHost(req.redirectURI),
		"scopes":       strings.Fields(req.scope),
	})
}

// DecideAuthorization approves or denies a pending authorization request of
// the signed-in user and returns where to send the browser: the client's
// redirect URI with an authorization code or an access_denied error. A
// request can be decided once.
func (a *Auth) DecideAuthorization(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Approve bool `json:"approve"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	user, _ := UserFrom(r.Context())
	req, ok := a.requests.take(r.PathValue("id"), user.GitHubID)
	if !ok {
		writeError(w, http.StatusNotFound, "authorization request not found or expired")
		return
	}
	if !in.Approve {
		a.log.Info("oauth authorization denied", "client", req.client.ID, "login", user.Login)
		writeJSONStatus(w, http.StatusOK, map[string]string{
			"redirect": withQuery(req.redirectURI, "error", "access_denied", "state", req.state),
		})
		return
	}
	code, err := randomToken(32)
	if err != nil {
		a.fail(w, "generate authorization code", err)
		return
	}
	err = a.store.CreateCode(r.Context(), Code{
		Hash:        hashToken(code),
		ClientID:    req.client.ID,
		GitHubID:    user.GitHubID,
		RedirectURI: req.redirectURI,
		Challenge:   req.challenge,
		Scope:       req.scope,
		Resource:    req.resource,
		ExpiresAt:   time.Now().Add(codeTTL),
	})
	if err != nil {
		a.fail(w, "store authorization code", err)
		return
	}
	a.log.Info("oauth authorization approved", "client", req.client.ID, "name", req.client.Name, "login", user.Login)
	writeJSONStatus(w, http.StatusOK, map[string]string{
		"redirect": withQuery(req.redirectURI, "code", code, "state", req.state),
	})
}

// Token implements the token endpoint for the authorization_code and
// refresh_token grants.
func (a *Auth) Token(w http.ResponseWriter, r *http.Request) {
	if !allowCORS(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	form, ok := parseOAuthForm(w, r)
	if !ok {
		return
	}
	if err := a.cleanUp(r.Context()); err != nil {
		a.log.Warn("remove expired oauth rows", "err", err)
	}
	switch form.Get("grant_type") {
	case "authorization_code":
		a.exchangeCode(w, r.Context(), form)
	case "refresh_token":
		a.refresh(w, r.Context(), form)
	case "":
		oauthError(w, http.StatusBadRequest, "invalid_request", "grant_type is required")
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "")
	}
}

// tokenError is an OAuth error response from the token endpoint.
type tokenError struct {
	code, desc string
}

func (e *tokenError) Error() string { return e.code + ": " + e.desc }

func (a *Auth) exchangeCode(w http.ResponseWriter, ctx context.Context, form url.Values) {
	code, clientID, verifier := form.Get("code"), form.Get("client_id"), form.Get("code_verifier")
	if code == "" || clientID == "" || verifier == "" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "code, client_id, and code_verifier are required")
		return
	}
	pair, err := newTokenPair()
	if err != nil {
		a.log.Error("generate oauth tokens", "err", err)
		oauthError(w, http.StatusInternalServerError, "server_error", "")
		return
	}
	grant, ok, err := a.store.RedeemCode(ctx, hashToken(code), func(c Code) (Grant, []Token, error) {
		switch {
		case c.ClientID != clientID:
			return Grant{}, nil, &tokenError{"invalid_grant", "the code was issued to another client"}
		case c.RedirectURI != form.Get("redirect_uri"):
			return Grant{}, nil, &tokenError{"invalid_grant", "redirect_uri does not match the authorization request"}
		case !verifyPKCE(verifier, c.Challenge):
			return Grant{}, nil, &tokenError{"invalid_grant", "code_verifier does not match the code challenge"}
		case form.Has("resource") && form.Get("resource") != c.Resource:
			return Grant{}, nil, &tokenError{"invalid_target", "resource does not match the authorization"}
		}
		return Grant{
			ClientID:    c.ClientID,
			GitHubID:    c.GitHubID,
			RedirectURI: c.RedirectURI,
			Scope:       c.Scope,
			Resource:    c.Resource,
		}, pair.tokens(), nil
	})
	a.writeTokens(w, grant, pair, ok, err)
}

func (a *Auth) refresh(w http.ResponseWriter, ctx context.Context, form url.Values) {
	token, clientID := form.Get("refresh_token"), form.Get("client_id")
	if token == "" || clientID == "" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "refresh_token and client_id are required")
		return
	}
	pair, err := newTokenPair()
	if err != nil {
		a.log.Error("generate oauth tokens", "err", err)
		oauthError(w, http.StatusInternalServerError, "server_error", "")
		return
	}
	grant, ok, err := a.store.RotateRefreshToken(ctx, hashToken(token), func(g Grant, u User) ([]Token, error) {
		switch {
		case g.ClientID != clientID:
			return nil, &tokenError{"invalid_grant", "the refresh token was issued to another client"}
		case !a.permits(u.Login):
			return nil, &tokenError{"invalid_grant", "the user is no longer allowed"}
		case form.Has("scope") && !subset(strings.Fields(form.Get("scope")), strings.Fields(g.Scope)...):
			return nil, &tokenError{"invalid_scope", "scope exceeds the authorization"}
		case form.Has("resource") && form.Get("resource") != g.Resource:
			return nil, &tokenError{"invalid_target", "resource does not match the authorization"}
		}
		return pair.tokens(), nil
	})
	a.writeTokens(w, grant, pair, ok, err)
}

// writeTokens answers a token request with the result of redeeming a code or
// a refresh token.
func (a *Auth) writeTokens(w http.ResponseWriter, g Grant, pair tokenPair, ok bool, err error) {
	var te *tokenError
	switch {
	case errors.As(err, &te):
		oauthError(w, http.StatusBadRequest, te.code, te.desc)
	case errors.Is(err, ErrReplayed):
		a.log.Warn("oauth code or refresh token replayed; grant revoked")
		oauthError(w, http.StatusBadRequest, "invalid_grant", "the grant is invalid, expired, or revoked")
	case err != nil:
		a.log.Error("issue oauth tokens", "err", err)
		oauthError(w, http.StatusInternalServerError, "server_error", "")
	case !ok:
		oauthError(w, http.StatusBadRequest, "invalid_grant", "the grant is invalid, expired, or revoked")
	default:
		writeJSONStatus(w, http.StatusOK, map[string]any{
			"access_token":  pair.access,
			"token_type":    "Bearer",
			"expires_in":    int(accessTTL / time.Second),
			"refresh_token": pair.refresh,
			"scope":         g.Scope,
		})
	}
}

// Revoke implements RFC 7009 token revocation. Revoking an access or refresh
// token revokes its whole grant.
func (a *Auth) Revoke(w http.ResponseWriter, r *http.Request) {
	if !allowCORS(w, r, http.MethodPost) {
		return
	}
	form, ok := parseOAuthForm(w, r)
	if !ok {
		return
	}
	token := form.Get("token")
	if token == "" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "token is required")
		return
	}
	if err := a.store.RevokeToken(r.Context(), hashToken(token), form.Get("client_id")); err != nil {
		a.log.Error("revoke oauth token", "err", err)
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Grants lists the signed-in user's connected clients.
func (a *Auth) Grants(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	grants, err := a.store.Grants(r.Context(), user.GitHubID)
	if err != nil {
		a.fail(w, "list oauth grants", err)
		return
	}
	type grantJSON struct {
		ID           string     `json:"id"`
		ClientName   string     `json:"clientName"`
		RedirectHost string     `json:"redirectHost"`
		Scopes       []string   `json:"scopes"`
		CreatedAt    time.Time  `json:"createdAt"`
		LastUsedAt   *time.Time `json:"lastUsedAt"`
	}
	out := make([]grantJSON, len(grants))
	for i, g := range grants {
		out[i] = grantJSON{
			ID:           g.ID,
			ClientName:   g.ClientName,
			RedirectHost: redirectHost(g.RedirectURI),
			Scopes:       strings.Fields(g.Scope),
			CreatedAt:    g.CreatedAt.UTC(),
			LastUsedAt:   g.LastUsedAt,
		}
	}
	writeJSONStatus(w, http.StatusOK, out)
}

// RevokeGrant revokes one of the signed-in user's grants and all its tokens.
func (a *Auth) RevokeGrant(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	ok, err := a.store.DeleteGrant(r.Context(), r.PathValue("id"), user.GitHubID)
	if err != nil {
		a.fail(w, "revoke oauth grant", err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "grant not found")
		return
	}
	a.log.Info("oauth grant revoked", "grant", r.PathValue("id"), "login", user.Login)
	w.WriteHeader(http.StatusNoContent)
}

// cleanUp removes expired OAuth rows, at most once per cleanupInterval.
func (a *Auth) cleanUp(ctx context.Context) error {
	a.cleanMu.Lock()
	defer a.cleanMu.Unlock()
	if time.Since(a.cleanedAt) < cleanupInterval {
		return nil
	}
	if err := a.store.DeleteExpiredOAuth(ctx, time.Now().Add(-staleClientTTL)); err != nil {
		return err
	}
	a.cleanedAt = time.Now()
	return nil
}

// request is a pending authorization request awaiting the user's consent.
type request struct {
	client      Client
	githubID    int64 // The user who started it; only they can decide it. 0 until sign-in.
	redirectURI string
	state       string
	challenge   string
	scope       string
	resource    string
	expires     time.Time
}

// requests holds pending authorization requests in memory. They live for
// requestTTL; a restart forgets them and the user starts again.
type requests struct {
	mu sync.Mutex
	m  map[string]*request
}

func newRequests() *requests {
	return &requests{m: make(map[string]*request)}
}

// add stores req and returns its ID. When full, it evicts the request that
// expires first.
func (rs *requests) add(req *request) (string, error) {
	id, err := randomToken(16)
	if err != nil {
		return "", err
	}
	now := time.Now()
	req.expires = now.Add(requestTTL)
	rs.mu.Lock()
	defer rs.mu.Unlock()
	var oldest string
	for k, v := range rs.m {
		if now.After(v.expires) {
			delete(rs.m, k)
		} else if oldest == "" || v.expires.Before(rs.m[oldest].expires) {
			oldest = k
		}
	}
	if len(rs.m) >= maxPending {
		delete(rs.m, oldest)
	}
	rs.m[id] = req
	return id, nil
}

// get returns the unexpired request with the given ID if githubID started it.
func (rs *requests) get(id string, githubID int64) (*request, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.lookup(id, githubID)
}

// take is get, also removing the request.
func (rs *requests) take(id string, githubID int64) (*request, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	req, ok := rs.lookup(id, githubID)
	if ok {
		delete(rs.m, id)
	}
	return req, ok
}

func (rs *requests) lookup(id string, githubID int64) (*request, bool) {
	req, ok := rs.m[id]
	if !ok || req.githubID != githubID {
		return nil, false
	}
	if time.Now().After(req.expires) {
		delete(rs.m, id)
		return nil, false
	}
	return req, true
}

// tokenPair is a new access token and refresh token.
type tokenPair struct {
	access, refresh string
}

func newTokenPair() (tokenPair, error) {
	access, err := randomToken(32)
	if err != nil {
		return tokenPair{}, err
	}
	refresh, err := randomToken(32)
	if err != nil {
		return tokenPair{}, err
	}
	return tokenPair{access: accessPrefix + access, refresh: refreshPrefix + refresh}, nil
}

// tokens returns the pair in its stored form, expiring from now.
func (p tokenPair) tokens() []Token {
	now := time.Now()
	return []Token{
		{Hash: hashToken(p.access), ExpiresAt: now.Add(accessTTL)},
		{Hash: hashToken(p.refresh), Refresh: true, ExpiresAt: now.Add(refreshTTL)},
	}
}

// allowsRedirect reports whether uri is one of the client's redirect URIs.
// Loopback http URIs match on any port, as RFC 8252 requires for native
// apps.
func (c Client) allowsRedirect(uri string) bool {
	if uri == "" {
		return false
	}
	if slices.Contains(c.RedirectURIs, uri) {
		return true
	}
	got, err := url.Parse(uri)
	if err != nil || got.Scheme != "http" || !loopback(got.Hostname()) {
		return false
	}
	for _, reg := range c.RedirectURIs {
		want, err := url.Parse(reg)
		if err != nil || want.Scheme != "http" || !loopback(want.Hostname()) {
			continue
		}
		if want.Hostname() == got.Hostname() && want.EscapedPath() == got.EscapedPath() &&
			want.RawQuery == got.RawQuery && got.User == nil && !strings.Contains(uri, "#") {
			return true
		}
	}
	return false
}

// checkRedirectURI reports why uri cannot be registered: it must be an
// absolute https URL, or an http URL on a loopback address, with no fragment
// or user info.
func checkRedirectURI(uri string) error {
	u, err := url.Parse(uri)
	switch {
	case len(uri) > maxURILen:
		return fmt.Errorf("redirect URI is longer than %d characters", maxURILen)
	case err != nil || u.Host == "" || strings.ContainsFunc(uri, isControl):
		return fmt.Errorf("redirect URI %q is not an absolute URL", uri)
	case u.Fragment != "" || strings.Contains(uri, "#"):
		return fmt.Errorf("redirect URI %q has a fragment", uri)
	case u.User != nil:
		return fmt.Errorf("redirect URI %q has user info", uri)
	case u.Scheme == "https":
		return nil
	case u.Scheme == "http" && loopback(u.Hostname()):
		return nil
	}
	return fmt.Errorf("redirect URI %q must use https, or http on a loopback address", uri)
}

// loopback reports whether host names the local machine.
func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func httpsURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && len(s) <= maxURILen && u.Scheme == "https" && u.Host != "" &&
		u.User == nil && !strings.ContainsFunc(s, isControl)
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

// redirectHost returns the host (and port) of a redirect URI, which the
// dashboard shows so the user can tell where approval sends them.
func redirectHost(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	return u.Host
}

// withQuery returns uri with the non-empty key-value pairs added to its
// query.
func withQuery(uri string, kv ...string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	q := u.Query()
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			q.Set(kv[i], kv[i+1])
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// parseScope parses a requested scope, defaulting to read. It reports false
// for unsupported scopes.
func parseScope(s string) (string, bool) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ScopeRead, true
	}
	if !subset(fields, supportedScopes...) {
		return "", false
	}
	slices.Sort(fields)
	return strings.Join(slices.Compact(fields), " "), true
}

// subset reports whether every element of items is in allowed.
func subset(items []string, allowed ...string) bool {
	for _, it := range items {
		if !slices.Contains(allowed, it) {
			return false
		}
	}
	return true
}

// validChallenge reports whether c is a base64url-encoded SHA-256 digest.
func validChallenge(c string) bool {
	b, err := base64.RawURLEncoding.DecodeString(c)
	return err == nil && len(b) == sha256.Size
}

// verifyPKCE reports whether verifier matches an S256 code challenge.
func verifyPKCE(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	for _, c := range verifier {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~", c)) {
			return false
		}
	}
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

// parseOAuthForm parses a form-encoded OAuth request body, rejecting other
// methods, other content types, and repeated parameters. On failure it has
// answered the request.
func parseOAuthForm(w http.ResponseWriter, r *http.Request) (url.Values, bool) {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/x-www-form-urlencoded" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "Content-Type must be application/x-www-form-urlencoded")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "invalid form body")
		return nil, false
	}
	for k, v := range r.PostForm {
		if len(v) > 1 {
			oauthError(w, http.StatusBadRequest, "invalid_request", k+" is repeated")
			return nil, false
		}
	}
	return r.PostForm, true
}

// allowCORS lets any origin call an OAuth endpoint, which clients reach from
// their own origin without cookies. It answers preflight requests and
// methods other than method, and reports whether the caller should go on.
func allowCORS(w http.ResponseWriter, r *http.Request, method string) bool {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	switch {
	case r.Method == method, r.Method == http.MethodHead && method == http.MethodGet:
		return true
	case r.Method == http.MethodOptions:
		h.Set("Access-Control-Allow-Methods", method+", OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, MCP-Protocol-Version")
		h.Set("Access-Control-Max-Age", "86400")
		w.WriteHeader(http.StatusNoContent)
		return false
	}
	h.Set("Allow", method+", OPTIONS")
	oauthError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
	return false
}

func oauthError(w http.ResponseWriter, status int, code, desc string) {
	body := map[string]string{"error": code}
	if desc != "" {
		body["error_description"] = desc
	}
	writeJSONStatus(w, status, body)
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

var errorPageTmpl = template.Must(template.New("error").Parse(`<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Authorization failed · shed</title>
<style>
body { font: 16px/1.5 system-ui, sans-serif; max-width: 32rem; margin: 4rem auto; padding: 0 1rem; color: #1a1a1a; }
h1 { font-size: 1.25rem; }
</style>
<h1>Can't connect this app</h1>
<p>{{.}}</p>
</html>
`))

// errorPage renders an authorization error for the user, for requests that
// must not be redirected back to the client.
func errorPage(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	errorPageTmpl.Execute(w, msg)
}
