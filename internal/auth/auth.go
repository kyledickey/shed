// Package auth implements dashboard authentication: GitHub sign-in handlers,
// cookie sessions, and middleware that guards API routes.
//
// Auth stores sessions through the small [Store] interface and reaches GitHub
// through [OAuth], so neither storage nor the GitHub client is imported here
// beyond the [github.User] type.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kyledickey/shed/internal/github"
)

const (
	sessionCookie = "shed_session"
	stateCookie   = "shed_oauth_state"

	sessionTTL = 30 * 24 * time.Hour
	stateTTL   = 10 * time.Minute

	callbackPath = "/api/auth/callback"
)

// User is a signed-in person.
type User struct {
	GitHubID  int64
	Login     string
	Name      string
	AvatarURL string
}

// Store persists users and sessions. Sessions are keyed by the hash of their
// token, never the token itself.
type Store interface {
	// User returns the user with the given GitHub ID, and whether it exists.
	User(ctx context.Context, githubID int64) (User, bool, error)
	// UpsertUser creates the user or updates its profile.
	UpsertUser(ctx context.Context, u User) error
	// CreateSession stores a session that expires at the given time.
	CreateSession(ctx context.Context, tokenHash string, githubID int64, expires time.Time) error
	// SessionUser returns the user of an unexpired session, and whether the
	// session exists.
	SessionUser(ctx context.Context, tokenHash string) (User, bool, error)
	// DeleteSession removes a session.
	DeleteSession(ctx context.Context, tokenHash string) error
}

// OAuth performs the GitHub sign-in handshake. *github.Client implements it.
type OAuth interface {
	// AuthorizeURL returns the GitHub page that asks the user to sign in.
	AuthorizeURL(redirectURL, state string) string
	// ExchangeUser trades an authorization code for the signed-in user.
	ExchangeUser(ctx context.Context, code, redirectURL string) (github.User, error)
}

var _ OAuth = (*github.Client)(nil)

// Auth serves the sign-in endpoints and guards routes behind a session.
type Auth struct {
	store   Store
	oauth   func() (OAuth, bool)
	baseURL string
	allowed map[string]bool // Lowercase logins.
	secure  bool            // Whether cookies are marked Secure.
	log     *slog.Logger
}

// New returns an Auth that keeps sessions in store. oauth reports the GitHub
// client, or false until the GitHub App has been configured. baseURL is the
// public dashboard URL; cookies are marked Secure when it is https.
// allowedUsers are the GitHub logins permitted to sign in, compared without
// regard to case.
func New(store Store, oauth func() (OAuth, bool), baseURL string, allowedUsers []string, log *slog.Logger) *Auth {
	allowed := make(map[string]bool, len(allowedUsers))
	for _, login := range allowedUsers {
		allowed[strings.ToLower(login)] = true
	}
	return &Auth{
		store:   store,
		oauth:   oauth,
		baseURL: strings.TrimRight(baseURL, "/"),
		allowed: allowed,
		secure:  strings.HasPrefix(baseURL, "https://"),
		log:     log,
	}
}

// Login starts GitHub sign-in: it sets a short-lived state cookie and
// redirects to GitHub.
func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	oauth, ok := a.oauth()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "github app is not configured")
		return
	}
	state, err := randomToken(16)
	if err != nil {
		a.fail(w, "generate oauth state", err)
		return
	}
	a.setCookie(w, stateCookie, state, "/api/auth", stateTTL)
	http.Redirect(w, r, oauth.AuthorizeURL(a.baseURL+callbackPath, state), http.StatusFound)
}

// Callback completes GitHub sign-in. If the user is permitted it creates a
// session and redirects to the dashboard; otherwise it redirects to the login
// page with an error.
func (a *Auth) Callback(w http.ResponseWriter, r *http.Request) {
	oauth, ok := a.oauth()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "github app is not configured")
		return
	}
	cookie, err := r.Cookie(stateCookie)
	state := r.URL.Query().Get("state")
	a.clearCookie(w, stateCookie, "/api/auth")
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		writeError(w, http.StatusBadRequest, "invalid oauth state")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" { // The user declined, or GitHub reported an error.
		http.Redirect(w, r, "/login?error=denied", http.StatusFound)
		return
	}
	ghUser, err := oauth.ExchangeUser(r.Context(), code, a.baseURL+callbackPath)
	if err != nil {
		a.log.Warn("github sign-in failed", "err", err)
		http.Redirect(w, r, "/login?error=failed", http.StatusFound)
		return
	}
	user := User{GitHubID: ghUser.ID, Login: ghUser.Login, Name: ghUser.Name, AvatarURL: ghUser.AvatarURL}

	token, err := a.admit(r.Context(), user)
	switch {
	case errors.Is(err, errDenied):
		a.log.Info("sign-in denied", "login", user.Login)
		http.Redirect(w, r, "/login?error=denied", http.StatusFound)
	case err != nil:
		a.fail(w, "sign in", err)
	default:
		a.setCookie(w, sessionCookie, token, "/", sessionTTL)
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

// Logout ends the current session.
func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := a.store.DeleteSession(r.Context(), hashToken(c.Value)); err != nil {
			a.log.Error("delete session", "err", err)
		}
	}
	a.clearCookie(w, sessionCookie, "/")
	w.WriteHeader(http.StatusNoContent)
}

// Require returns a handler that serves next only for requests with a valid
// session, making the user available through [UserFrom]. Other requests get a
// 401 JSON error.
func (a *Auth) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, ok, err := a.store.SessionUser(r.Context(), hashToken(c.Value))
		if err != nil {
			a.fail(w, "look up session", err)
			return
		}
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, user)))
	})
}

type userKey struct{}

// UserFrom returns the user that [Auth.Require] attached to ctx.
func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userKey{}).(User)
	return u, ok
}

var errDenied = errors.New("auth: user not permitted")

// admit applies the access rule to user and, if it passes, records the user
// and a new session, returning the session token. A login is permitted if it
// is in the allowed list or its user already exists. New users must be
// explicitly allowed, including the first administrator.
func (a *Auth) admit(ctx context.Context, user User) (token string, err error) {
	if !a.allowed[strings.ToLower(user.Login)] {
		_, exists, err := a.store.User(ctx, user.GitHubID)
		if err != nil {
			return "", fmt.Errorf("auth: look up user %q: %w", user.Login, err)
		}
		if !exists {
			return "", errDenied
		}
	}
	if err := a.store.UpsertUser(ctx, user); err != nil {
		return "", fmt.Errorf("auth: save user %q: %w", user.Login, err)
	}
	token, err = randomToken(32)
	if err != nil {
		return "", fmt.Errorf("auth: generate session token: %w", err)
	}
	if err := a.store.CreateSession(ctx, hashToken(token), user.GitHubID, time.Now().Add(sessionTTL)); err != nil {
		return "", fmt.Errorf("auth: create session: %w", err)
	}
	return token, nil
}

func (a *Auth) setCookie(w http.ResponseWriter, name, value, path string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		MaxAge:   int(ttl / time.Second),
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *Auth) clearCookie(w http.ResponseWriter, name, path string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Path:     path,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// fail logs err and sends a generic 500.
func (a *Auth) fail(w http.ResponseWriter, what string, err error) {
	a.log.Error(what, "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// randomToken returns n random bytes, base64url-encoded.
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken returns the hex SHA-256 of a session token, which is what the
// store keeps.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
