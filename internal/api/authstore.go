package api

import (
	"context"
	"errors"
	"time"

	"github.com/kyledickey/shed/internal/auth"
	"github.com/kyledickey/shed/internal/store"
)

// AuthStore adapts the store to [auth.Store].
func AuthStore(st *store.Store) auth.Store {
	return authStore{st}
}

type authStore struct {
	st *store.Store
}

func (a authStore) DeleteDisallowedSessions(ctx context.Context, allowed []string) error {
	return a.st.DeleteDisallowedSessions(ctx, allowed)
}

func (a authStore) UpsertUser(ctx context.Context, u auth.User) error {
	return a.st.UpsertUser(ctx, store.User{GitHubID: u.GitHubID, Login: u.Login, Name: u.Name, AvatarURL: u.AvatarURL})
}

func (a authStore) CreateSession(ctx context.Context, tokenHash string, githubID int64, expires time.Time) error {
	return a.st.CreateSession(ctx, tokenHash, githubID, expires)
}

func (a authStore) SessionUser(ctx context.Context, tokenHash string) (auth.User, bool, error) {
	return found(a.st.SessionUser(ctx, tokenHash))
}

func (a authStore) DeleteSession(ctx context.Context, tokenHash string) error {
	return a.st.DeleteSession(ctx, tokenHash)
}

// found converts a store lookup into the found-flag form auth uses.
func found(u store.User, err error) (auth.User, bool, error) {
	if errors.Is(err, store.ErrNotFound) {
		return auth.User{}, false, nil
	}
	if err != nil {
		return auth.User{}, false, err
	}
	return authUser(u), true, nil
}

func (a authStore) CreateClient(ctx context.Context, c auth.Client) (auth.Client, error) {
	sc, err := a.st.CreateOAuthClient(ctx, store.OAuthClient{Name: c.Name, URI: c.URI, RedirectURIs: c.RedirectURIs})
	if err != nil {
		return auth.Client{}, err
	}
	return authClient(sc), nil
}

func (a authStore) Client(ctx context.Context, id string) (auth.Client, bool, error) {
	c, err := a.st.OAuthClient(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return auth.Client{}, false, nil
	}
	if err != nil {
		return auth.Client{}, false, err
	}
	return authClient(c), true, nil
}

func (a authStore) CountClients(ctx context.Context) (int, error) {
	return a.st.CountOAuthClients(ctx)
}

func (a authStore) CreateCode(ctx context.Context, c auth.Code) error {
	return a.st.CreateOAuthCode(ctx, store.OAuthCode{
		Hash: c.Hash, ClientID: c.ClientID, GitHubID: c.GitHubID, RedirectURI: c.RedirectURI,
		Challenge: c.Challenge, Scope: c.Scope, Resource: c.Resource, ExpiresAt: c.ExpiresAt,
	})
}

func (a authStore) RedeemCode(ctx context.Context, hash string,
	issue func(auth.Code) (auth.Grant, []auth.Token, error)) (auth.Grant, bool, error) {
	g, err := a.st.RedeemOAuthCode(ctx, hash, func(c store.OAuthCode) (store.OAuthGrant, []store.OAuthToken, error) {
		g, tokens, err := issue(auth.Code{
			Hash: c.Hash, ClientID: c.ClientID, GitHubID: c.GitHubID, RedirectURI: c.RedirectURI,
			Challenge: c.Challenge, Scope: c.Scope, Resource: c.Resource, ExpiresAt: c.ExpiresAt,
		})
		return storeGrant(g), storeTokens(tokens), err
	})
	return grantFound(g, err)
}

func (a authStore) RotateRefreshToken(ctx context.Context, hash string,
	issue func(auth.Grant, auth.User) ([]auth.Token, error)) (auth.Grant, bool, error) {
	g, err := a.st.RotateOAuthRefreshToken(ctx, hash, func(g store.OAuthGrant, u store.User) ([]store.OAuthToken, error) {
		tokens, err := issue(authGrant(g), authUser(u))
		return storeTokens(tokens), err
	})
	return grantFound(g, err)
}

func (a authStore) AccessToken(ctx context.Context, hash string) (auth.Grant, auth.User, bool, error) {
	g, u, err := a.st.OAuthAccessToken(ctx, hash)
	if errors.Is(err, store.ErrNotFound) {
		return auth.Grant{}, auth.User{}, false, nil
	}
	if err != nil {
		return auth.Grant{}, auth.User{}, false, err
	}
	return authGrant(g), authUser(u), true, nil
}

func (a authStore) TouchGrant(ctx context.Context, id string, at time.Time) error {
	return a.st.TouchOAuthGrant(ctx, id, at)
}

func (a authStore) Grants(ctx context.Context, githubID int64) ([]auth.Grant, error) {
	grants, err := a.st.OAuthGrants(ctx, githubID)
	if err != nil {
		return nil, err
	}
	out := make([]auth.Grant, len(grants))
	for i, g := range grants {
		out[i] = authGrant(g)
	}
	return out, nil
}

func (a authStore) DeleteGrant(ctx context.Context, id string, githubID int64) (bool, error) {
	err := a.st.DeleteOAuthGrant(ctx, id, githubID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (a authStore) RevokeToken(ctx context.Context, hash, clientID string) error {
	return a.st.RevokeOAuthToken(ctx, hash, clientID)
}

func (a authStore) DeleteExpiredOAuth(ctx context.Context, staleBefore time.Time) error {
	return a.st.DeleteExpiredOAuth(ctx, staleBefore)
}

func (a authStore) DeleteDisallowedGrants(ctx context.Context, allowed []string) error {
	return a.st.DeleteDisallowedOAuthGrants(ctx, allowed)
}

func authUser(u store.User) auth.User {
	return auth.User{GitHubID: u.GitHubID, Login: u.Login, Name: u.Name, AvatarURL: u.AvatarURL}
}

func authClient(c store.OAuthClient) auth.Client {
	return auth.Client{ID: c.ID, Name: c.Name, URI: c.URI, RedirectURIs: c.RedirectURIs, CreatedAt: c.CreatedAt}
}

func authGrant(g store.OAuthGrant) auth.Grant {
	return auth.Grant{
		ID: g.ID, ClientID: g.ClientID, ClientName: g.ClientName, GitHubID: g.GitHubID,
		RedirectURI: g.RedirectURI, Scope: g.Scope, Resource: g.Resource,
		CreatedAt: g.CreatedAt, LastUsedAt: g.LastUsedAt,
	}
}

func storeGrant(g auth.Grant) store.OAuthGrant {
	return store.OAuthGrant{
		ClientID: g.ClientID, GitHubID: g.GitHubID, RedirectURI: g.RedirectURI,
		Scope: g.Scope, Resource: g.Resource,
	}
}

func storeTokens(tokens []auth.Token) []store.OAuthToken {
	out := make([]store.OAuthToken, len(tokens))
	for i, t := range tokens {
		kind := store.OAuthAccess
		if t.Refresh {
			kind = store.OAuthRefresh
		}
		out[i] = store.OAuthToken{Hash: t.Hash, Kind: kind, ExpiresAt: t.ExpiresAt}
	}
	return out
}

// grantFound converts a redemption into the found-flag form auth uses.
func grantFound(g store.OAuthGrant, err error) (auth.Grant, bool, error) {
	if errors.Is(err, store.ErrNotFound) {
		return auth.Grant{}, false, nil
	}
	if errors.Is(err, store.ErrReplayed) {
		return auth.Grant{}, false, auth.ErrReplayed
	}
	if err != nil {
		return auth.Grant{}, false, err
	}
	return authGrant(g), true, nil
}
