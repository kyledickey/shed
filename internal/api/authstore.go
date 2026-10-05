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
	return auth.User{GitHubID: u.GitHubID, Login: u.Login, Name: u.Name, AvatarURL: u.AvatarURL}, true, nil
}
