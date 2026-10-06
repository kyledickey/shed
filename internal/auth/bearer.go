package auth

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RequireBearer returns middleware that serves next only for requests with
// an OAuth access token, in the Authorization header, that was issued for
// resource and belongs to a permitted user. It makes the user available
// through [UserFrom] and the token's scopes through [ScopesFrom]. Cookies are
// ignored. Other requests get a 401 whose WWW-Authenticate header points
// clients at the resource's metadata.
func (a *Auth) RequireBearer(resource string) func(http.Handler) http.Handler {
	metadata := a.baseURL + "/.well-known/oauth-protected-resource"
	if u, err := url.Parse(resource); err == nil {
		metadata += strings.TrimRight(u.EscapedPath(), "/")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				challenge(w, metadata, "")
				return
			}
			grant, user, ok, err := a.store.AccessToken(r.Context(), hashToken(token))
			if err != nil {
				a.fail(w, "look up access token", err)
				return
			}
			if !ok || grant.Resource != resource || !a.permits(user.Login) {
				challenge(w, metadata, "invalid_token")
				return
			}
			if err := a.store.TouchGrant(r.Context(), grant.ID, time.Now()); err != nil {
				a.log.Warn("record oauth grant use", "grant", grant.ID, "err", err)
			}
			ctx := context.WithValue(r.Context(), userKey{}, user)
			ctx = context.WithValue(ctx, scopesKey{}, strings.Fields(grant.Scope))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type scopesKey struct{}

// ScopesFrom returns the scopes of the access token that
// [Auth.RequireBearer] accepted for ctx.
func ScopesFrom(ctx context.Context) []string {
	s, _ := ctx.Value(scopesKey{}).([]string)
	return s
}

// bearerToken returns the token of an Authorization: Bearer header.
func bearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	token = strings.TrimSpace(token)
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", false
	}
	return token, true
}

// challenge answers 401 with a Bearer challenge naming the resource
// metadata, and an error code when a token was presented.
func challenge(w http.ResponseWriter, metadata, code string) {
	value := `Bearer resource_metadata="` + metadata + `"`
	if code != "" {
		value += `, error="` + code + `"`
	}
	w.Header().Set("WWW-Authenticate", value)
	writeError(w, http.StatusUnauthorized, "unauthorized")
}
