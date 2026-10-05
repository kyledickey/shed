package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Setting returns the value of a setting, or ErrNotFound.
func (s *Store) Setting(ctx context.Context, key string) (string, error) {
	v, err := queryOne(ctx, s, func(r scanner) (string, error) {
		var v string
		err := r.Scan(&v)
		return v, err
	}, `SELECT value FROM settings WHERE key = ?`, key)
	if err != nil {
		return "", fmt.Errorf("store: setting %q: %w", key, err)
	}
	return v, nil
}

// SetSetting creates or replaces a setting.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	err := s.exec(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("store: set setting %q: %w", key, err)
	}
	return nil
}

// AddSetting stores a setting unless the key already has a value, atomically.
// It reports whether it stored value.
func (s *Store) AddSetting(ctx context.Context, key, value string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO NOTHING`, key, value)
	if err != nil {
		return false, fmt.Errorf("store: add setting %q: %w", key, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: add setting %q: %w", key, err)
	}
	return n == 1, nil
}

const userCols = `github_id, login, name, avatar_url, created_at`

func scanUser(r scanner) (User, error) {
	var u User
	err := r.Scan(&u.GitHubID, &u.Login, &u.Name, &u.AvatarURL, (*timestamp)(&u.CreatedAt))
	return u, err
}

// UpsertUser creates the user or updates its login, name, and avatar. A zero
// CreatedAt defaults to the current time and is kept for existing users.
func (s *Store) UpsertUser(ctx context.Context, u User) error {
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now()
	}
	err := s.exec(ctx, `INSERT INTO users (`+userCols+`) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (github_id) DO UPDATE SET
			login = excluded.login, name = excluded.name, avatar_url = excluded.avatar_url`,
		u.GitHubID, u.Login, u.Name, u.AvatarURL, formatTime(u.CreatedAt))
	if err != nil {
		return fmt.Errorf("store: upsert user %q: %w", u.Login, err)
	}
	return nil
}

// User returns the user with the given GitHub ID, or ErrNotFound.
func (s *Store) User(ctx context.Context, githubID int64) (User, error) {
	u, err := queryOne(ctx, s, scanUser, `SELECT `+userCols+` FROM users WHERE github_id = ?`, githubID)
	if err != nil {
		return User{}, fmt.Errorf("store: user %d: %w", githubID, err)
	}
	return u, nil
}

// CountUsers returns the number of users.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count users: %w", err)
	}
	return n, nil
}

// CreateSession stores a session for a user, identified by the hash of its
// token.
func (s *Store) CreateSession(ctx context.Context, tokenHash string, githubID int64, expires time.Time) error {
	err := s.exec(ctx, `INSERT INTO sessions (token_hash, github_id, expires_at) VALUES (?, ?, ?)`,
		tokenHash, githubID, formatTime(expires))
	if err != nil {
		return fmt.Errorf("store: create session: %w", err)
	}
	return nil
}

// SessionUser returns the user owning an unexpired session, or ErrNotFound.
func (s *Store) SessionUser(ctx context.Context, tokenHash string) (User, error) {
	u, err := queryOne(ctx, s, scanUser, `SELECT u.github_id, u.login, u.name, u.avatar_url, u.created_at
		FROM sessions s JOIN users u USING (github_id)
		WHERE s.token_hash = ? AND s.expires_at > ?`, tokenHash, formatTime(time.Now()))
	if err != nil {
		return User{}, fmt.Errorf("store: session user: %w", err)
	}
	return u, nil
}

// DeleteSession removes a session. Deleting an unknown session is not an
// error.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	if err := s.exec(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("store: delete session: %w", err)
	}
	return nil
}

// DeleteExpiredSessions removes all sessions that have expired.
func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	if err := s.exec(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, formatTime(time.Now())); err != nil {
		return fmt.Errorf("store: delete expired sessions: %w", err)
	}
	return nil
}

// DeleteDisallowedSessions revokes sessions for users outside the login allowlist.
// An empty allowlist revokes every session.
func (s *Store) DeleteDisallowedSessions(ctx context.Context, allowed []string) error {
	args := make([]any, len(allowed))
	for i, login := range allowed {
		args[i] = strings.ToLower(login)
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
	query := `DELETE FROM sessions WHERE github_id IN (SELECT github_id FROM users WHERE lower(login) NOT IN (` + marks + `))`
	if err := s.exec(ctx, query, args...); err != nil {
		return fmt.Errorf("store: revoke disallowed sessions: %w", err)
	}
	return nil
}
