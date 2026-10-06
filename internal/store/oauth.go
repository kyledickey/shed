package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrReplayed is returned when an authorization code is redeemed twice or a
// rotated refresh token is presented again. The grant they belong to has been
// revoked by the time it is returned.
var ErrReplayed = errors.New("store: oauth credential replayed")

// OAuthClient is an OAuth client registered through dynamic client
// registration. Every client is public: it has no secret.
type OAuthClient struct {
	ID           string
	Name         string
	URI          string
	RedirectURIs []string
	CreatedAt    time.Time
}

// OAuthCode is an unredeemed authorization code, keyed by the hash of its
// value.
type OAuthCode struct {
	Hash        string
	ClientID    string
	GitHubID    int64
	RedirectURI string
	Challenge   string // PKCE S256 code challenge
	Scope       string
	Resource    string
	ExpiresAt   time.Time
}

// OAuthGrant is an approved authorization: a user let a client act for them.
// Its tokens die with it.
type OAuthGrant struct {
	ID          string
	ClientID    string
	GitHubID    int64
	RedirectURI string
	Scope       string
	Resource    string
	CreatedAt   time.Time
	LastUsedAt  *time.Time
	// ClientName is the client's registered name. Reads fill it; writes
	// ignore it.
	ClientName string
}

// OAuthTokenKind distinguishes access tokens from refresh tokens.
type OAuthTokenKind string

// OAuth token kinds.
const (
	OAuthAccess  OAuthTokenKind = "access"
	OAuthRefresh OAuthTokenKind = "refresh"
)

// OAuthToken is an access or refresh token of a grant, keyed by the hash of
// its value.
type OAuthToken struct {
	Hash      string
	Kind      OAuthTokenKind
	ExpiresAt time.Time
}

const clientCols = `id, name, uri, redirect_uris, created_at`

func scanClient(r scanner) (OAuthClient, error) {
	var c OAuthClient
	var uris string
	if err := r.Scan(&c.ID, &c.Name, &c.URI, &uris, (*timestamp)(&c.CreatedAt)); err != nil {
		return c, err
	}
	if err := json.Unmarshal([]byte(uris), &c.RedirectURIs); err != nil {
		return c, fmt.Errorf("decode redirect uris: %w", err)
	}
	return c, nil
}

// CreateOAuthClient stores a client and returns it with its new ID and
// creation time.
func (s *Store) CreateOAuthClient(ctx context.Context, c OAuthClient) (OAuthClient, error) {
	c.ID, c.CreatedAt = NewID(), now()
	uris, err := json.Marshal(c.RedirectURIs)
	if err != nil {
		return OAuthClient{}, fmt.Errorf("store: create oauth client: %w", err)
	}
	err = s.exec(ctx, `INSERT INTO oauth_clients (`+clientCols+`) VALUES (?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.URI, string(uris), formatTime(c.CreatedAt))
	if err != nil {
		return OAuthClient{}, fmt.Errorf("store: create oauth client: %w", err)
	}
	return c, nil
}

// OAuthClient returns a registered client, or ErrNotFound.
func (s *Store) OAuthClient(ctx context.Context, id string) (OAuthClient, error) {
	c, err := queryOne(ctx, s, scanClient, `SELECT `+clientCols+` FROM oauth_clients WHERE id = ?`, id)
	if err != nil {
		return OAuthClient{}, fmt.Errorf("store: oauth client %s: %w", id, err)
	}
	return c, nil
}

// CountOAuthClients returns the number of registered clients.
func (s *Store) CountOAuthClients(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM oauth_clients`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count oauth clients: %w", err)
	}
	return n, nil
}

// CreateOAuthCode stores an authorization code.
func (s *Store) CreateOAuthCode(ctx context.Context, c OAuthCode) error {
	err := s.exec(ctx, `INSERT INTO oauth_codes (code_hash, client_id, github_id, redirect_uri,
		code_challenge, scope, resource, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Hash, c.ClientID, c.GitHubID, c.RedirectURI, c.Challenge, c.Scope, c.Resource, formatTime(c.ExpiresAt))
	if err != nil {
		return fmt.Errorf("store: create oauth code: %w", err)
	}
	return nil
}

// RedeemOAuthCode redeems the unexpired code with the given hash. issue
// validates the code and returns the grant and tokens to create for it; if it
// fails, the code is spent and its error returned. A code can be redeemed
// once: redeeming it again revokes the grant it produced and returns
// ErrReplayed. An unknown or expired code is ErrNotFound.
func (s *Store) RedeemOAuthCode(ctx context.Context, hash string,
	issue func(OAuthCode) (OAuthGrant, []OAuthToken, error)) (OAuthGrant, error) {
	g, err := s.redeemOAuthCode(ctx, hash, issue)
	if err != nil {
		return OAuthGrant{}, fmt.Errorf("store: redeem oauth code: %w", err)
	}
	return g, nil
}

func (s *Store) redeemOAuthCode(ctx context.Context, hash string,
	issue func(OAuthCode) (OAuthGrant, []OAuthToken, error)) (OAuthGrant, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return OAuthGrant{}, err
	}
	defer tx.Rollback()

	var c OAuthCode
	var grantID string
	err = tx.QueryRowContext(ctx, `SELECT code_hash, client_id, github_id, redirect_uri, code_challenge,
		scope, resource, expires_at, grant_id FROM oauth_codes WHERE code_hash = ? AND expires_at > ?`,
		hash, formatTime(time.Now())).
		Scan(&c.Hash, &c.ClientID, &c.GitHubID, &c.RedirectURI, &c.Challenge,
			&c.Scope, &c.Resource, (*timestamp)(&c.ExpiresAt), &grantID)
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthGrant{}, ErrNotFound
	}
	if err != nil {
		return OAuthGrant{}, err
	}
	if grantID != "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_grants WHERE id = ?`, grantID); err != nil {
			return OAuthGrant{}, err
		}
		if err := tx.Commit(); err != nil {
			return OAuthGrant{}, err
		}
		return OAuthGrant{}, ErrReplayed
	}

	g, tokens, issueErr := issue(c)
	if issueErr != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_codes WHERE code_hash = ?`, hash); err != nil {
			return OAuthGrant{}, err
		}
		if err := tx.Commit(); err != nil {
			return OAuthGrant{}, err
		}
		return OAuthGrant{}, issueErr
	}
	g.ID, g.CreatedAt, g.LastUsedAt = NewID(), now(), nil
	if _, err := tx.ExecContext(ctx, `INSERT INTO oauth_grants (id, client_id, github_id, redirect_uri,
		scope, resource, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		g.ID, g.ClientID, g.GitHubID, g.RedirectURI, g.Scope, g.Resource, formatTime(g.CreatedAt)); err != nil {
		return OAuthGrant{}, err
	}
	if err := insertTokens(ctx, tx, g.ID, tokens); err != nil {
		return OAuthGrant{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oauth_codes SET grant_id = ? WHERE code_hash = ?`, g.ID, hash); err != nil {
		return OAuthGrant{}, err
	}
	return g, tx.Commit()
}

func insertTokens(ctx context.Context, tx *sql.Tx, grantID string, tokens []OAuthToken) error {
	for _, t := range tokens {
		if _, err := tx.ExecContext(ctx, `INSERT INTO oauth_tokens (token_hash, grant_id, kind, expires_at)
			VALUES (?, ?, ?, ?)`, t.Hash, grantID, string(t.Kind), formatTime(t.ExpiresAt)); err != nil {
			return mapError(err)
		}
	}
	return nil
}

// grantUserCols selects a grant (g) with its client's name (c) and its user (u).
const grantUserCols = `g.id, g.client_id, g.github_id, g.redirect_uri, g.scope, g.resource,
	g.created_at, g.last_used_at, c.name, u.github_id, u.login, u.name, u.avatar_url, u.created_at`

const grantUserFrom = ` FROM oauth_grants g JOIN oauth_clients c ON c.id = g.client_id
	JOIN users u ON u.github_id = g.github_id `

func scanGrantUser(r scanner) (OAuthGrant, User, error) {
	var g OAuthGrant
	var u User
	err := r.Scan(&g.ID, &g.ClientID, &g.GitHubID, &g.RedirectURI, &g.Scope, &g.Resource,
		(*timestamp)(&g.CreatedAt), nullTimestamp{&g.LastUsedAt}, &g.ClientName,
		&u.GitHubID, &u.Login, &u.Name, &u.AvatarURL, (*timestamp)(&u.CreatedAt))
	return g, u, err
}

// RotateOAuthRefreshToken spends the unexpired refresh token with the given
// hash. issue validates its grant and user and returns the tokens that
// replace it; if it fails, nothing changes and its error is returned.
// Presenting a token that was already rotated revokes its grant and returns
// ErrReplayed. An unknown or expired token is ErrNotFound.
func (s *Store) RotateOAuthRefreshToken(ctx context.Context, hash string,
	issue func(OAuthGrant, User) ([]OAuthToken, error)) (OAuthGrant, error) {
	g, err := s.rotateOAuthRefreshToken(ctx, hash, issue)
	if err != nil {
		return OAuthGrant{}, fmt.Errorf("store: rotate oauth refresh token: %w", err)
	}
	return g, nil
}

func (s *Store) rotateOAuthRefreshToken(ctx context.Context, hash string,
	issue func(OAuthGrant, User) ([]OAuthToken, error)) (OAuthGrant, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return OAuthGrant{}, err
	}
	defer tx.Rollback()

	at := now()
	var rotated bool
	var g OAuthGrant
	var u User
	err = tx.QueryRowContext(ctx, `SELECT t.rotated, `+grantUserCols+grantUserFrom+
		`JOIN oauth_tokens t ON t.grant_id = g.id
		WHERE t.token_hash = ? AND t.kind = ? AND t.expires_at > ?`,
		hash, string(OAuthRefresh), formatTime(at)).
		Scan(&rotated, &g.ID, &g.ClientID, &g.GitHubID, &g.RedirectURI, &g.Scope, &g.Resource,
			(*timestamp)(&g.CreatedAt), nullTimestamp{&g.LastUsedAt}, &g.ClientName,
			&u.GitHubID, &u.Login, &u.Name, &u.AvatarURL, (*timestamp)(&u.CreatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthGrant{}, ErrNotFound
	}
	if err != nil {
		return OAuthGrant{}, err
	}
	if rotated {
		if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_grants WHERE id = ?`, g.ID); err != nil {
			return OAuthGrant{}, err
		}
		if err := tx.Commit(); err != nil {
			return OAuthGrant{}, err
		}
		return OAuthGrant{}, ErrReplayed
	}
	tokens, err := issue(g, u)
	if err != nil {
		return OAuthGrant{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oauth_tokens SET rotated = 1 WHERE token_hash = ?`, hash); err != nil {
		return OAuthGrant{}, err
	}
	if err := insertTokens(ctx, tx, g.ID, tokens); err != nil {
		return OAuthGrant{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oauth_grants SET last_used_at = ? WHERE id = ?`,
		formatTime(at), g.ID); err != nil {
		return OAuthGrant{}, err
	}
	g.LastUsedAt = &at
	return g, tx.Commit()
}

// OAuthAccessToken returns the grant and user of the unexpired access token
// with the given hash, or ErrNotFound.
func (s *Store) OAuthAccessToken(ctx context.Context, hash string) (OAuthGrant, User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+grantUserCols+grantUserFrom+
		`JOIN oauth_tokens t ON t.grant_id = g.id
		WHERE t.token_hash = ? AND t.kind = ? AND t.expires_at > ?`,
		hash, string(OAuthAccess), formatTime(time.Now()))
	g, u, err := scanGrantUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if err != nil {
		return OAuthGrant{}, User{}, fmt.Errorf("store: oauth access token: %w", err)
	}
	return g, u, nil
}

// TouchOAuthGrant records that a grant was used at the given time. To spare
// writes, it records nothing when the last recorded use is under a minute
// older.
func (s *Store) TouchOAuthGrant(ctx context.Context, id string, at time.Time) error {
	err := s.exec(ctx, `UPDATE oauth_grants SET last_used_at = ?
		WHERE id = ? AND (last_used_at IS NULL OR last_used_at < ?)`,
		formatTime(at), id, formatTime(at.Add(-time.Minute)))
	if err != nil {
		return fmt.Errorf("store: touch oauth grant %s: %w", id, err)
	}
	return nil
}

// OAuthGrants returns a user's live grants, those with an unexpired refresh
// token, newest first.
func (s *Store) OAuthGrants(ctx context.Context, githubID int64) ([]OAuthGrant, error) {
	grants, err := queryAll(ctx, s, func(r scanner) (OAuthGrant, error) {
		g, _, err := scanGrantUser(r)
		return g, err
	}, `SELECT `+grantUserCols+grantUserFrom+`
		WHERE g.github_id = ? AND EXISTS (SELECT 1 FROM oauth_tokens t
			WHERE t.grant_id = g.id AND t.kind = ? AND t.rotated = 0 AND t.expires_at > ?)
		ORDER BY g.created_at DESC, g.id`, githubID, string(OAuthRefresh), formatTime(time.Now()))
	if err != nil {
		return nil, fmt.Errorf("store: oauth grants of %d: %w", githubID, err)
	}
	return grants, nil
}

// DeleteOAuthGrant revokes a user's grant and its tokens. It returns
// ErrNotFound if the user has no such grant.
func (s *Store) DeleteOAuthGrant(ctx context.Context, id string, githubID int64) error {
	if err := s.execOne(ctx, `DELETE FROM oauth_grants WHERE id = ? AND github_id = ?`, id, githubID); err != nil {
		return fmt.Errorf("store: delete oauth grant %s: %w", id, err)
	}
	return nil
}

// RevokeOAuthToken revokes the grant of the access or refresh token with the
// given hash, if that token was issued to clientID or clientID is empty.
// Revoking an unknown token is not an error.
func (s *Store) RevokeOAuthToken(ctx context.Context, hash, clientID string) error {
	err := s.exec(ctx, `DELETE FROM oauth_grants WHERE (? = '' OR client_id = ?) AND id IN
		(SELECT grant_id FROM oauth_tokens WHERE token_hash = ?)`, clientID, clientID, hash)
	if err != nil {
		return fmt.Errorf("store: revoke oauth token: %w", err)
	}
	return nil
}

// DeleteExpiredOAuth removes expired codes and tokens, grants left without a
// usable refresh token, and clients created before staleBefore that have
// neither grants nor codes.
func (s *Store) DeleteExpiredOAuth(ctx context.Context, staleBefore time.Time) error {
	at := formatTime(time.Now())
	for _, q := range []struct {
		query string
		args  []any
	}{
		{`DELETE FROM oauth_codes WHERE expires_at <= ?`, []any{at}},
		{`DELETE FROM oauth_tokens WHERE expires_at <= ?`, []any{at}},
		{`DELETE FROM oauth_grants WHERE NOT EXISTS (SELECT 1 FROM oauth_tokens t
			WHERE t.grant_id = oauth_grants.id AND t.kind = ? AND t.rotated = 0)`, []any{string(OAuthRefresh)}},
		{`DELETE FROM oauth_clients WHERE created_at < ?
			AND NOT EXISTS (SELECT 1 FROM oauth_grants g WHERE g.client_id = oauth_clients.id)
			AND NOT EXISTS (SELECT 1 FROM oauth_codes c WHERE c.client_id = oauth_clients.id)`,
			[]any{formatTime(staleBefore)}},
	} {
		if err := s.exec(ctx, q.query, q.args...); err != nil {
			return fmt.Errorf("store: delete expired oauth: %w", err)
		}
	}
	return nil
}

// DeleteDisallowedOAuthGrants revokes the grants and codes of users outside
// the login allowlist. An empty allowlist revokes every grant.
func (s *Store) DeleteDisallowedOAuthGrants(ctx context.Context, allowed []string) error {
	args := make([]any, len(allowed))
	for i, login := range allowed {
		args[i] = strings.ToLower(login)
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
	disallowed := `github_id IN (SELECT github_id FROM users WHERE lower(login) NOT IN (` + marks + `))`
	for _, table := range []string{"oauth_grants", "oauth_codes"} {
		if err := s.exec(ctx, `DELETE FROM `+table+` WHERE `+disallowed, args...); err != nil {
			return fmt.Errorf("store: revoke disallowed oauth grants: %w", err)
		}
	}
	return nil
}
