package store

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

func oauthFixture(t *testing.T) (*Store, OAuthClient) {
	t.Helper()
	s := newStore(t)
	ctx := context.Background()
	for _, u := range []User{{GitHubID: 1, Login: "Alice"}, {GitHubID: 2, Login: "bob"}} {
		if err := s.UpsertUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	c, err := s.CreateOAuthClient(ctx, OAuthClient{Name: "agent", RedirectURIs: []string{"http://127.0.0.1/cb"}})
	if err != nil {
		t.Fatal(err)
	}
	return s, c
}

func mustCode(t *testing.T, s *Store, c OAuthCode) {
	t.Helper()
	if c.ExpiresAt.IsZero() {
		c.ExpiresAt = time.Now().Add(time.Minute)
	}
	if err := s.CreateOAuthCode(context.Background(), c); err != nil {
		t.Fatal(err)
	}
}

// grantFor redeems a fresh code for user and returns the grant, whose tokens
// are access-<n> and refresh-<n>.
func grantFor(t *testing.T, s *Store, clientID string, user int64, n string) OAuthGrant {
	t.Helper()
	mustCode(t, s, OAuthCode{Hash: "code-" + n, ClientID: clientID, GitHubID: user, RedirectURI: "r", Scope: "read", Resource: "res"})
	g, err := s.RedeemOAuthCode(context.Background(), "code-"+n, func(c OAuthCode) (OAuthGrant, []OAuthToken, error) {
		return OAuthGrant{ClientID: c.ClientID, GitHubID: c.GitHubID, RedirectURI: c.RedirectURI, Scope: c.Scope, Resource: c.Resource},
			tokens(n), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func tokens(n string) []OAuthToken {
	exp := time.Now().Add(time.Hour)
	return []OAuthToken{{Hash: "access-" + n, Kind: OAuthAccess, ExpiresAt: exp}, {Hash: "refresh-" + n, Kind: OAuthRefresh, ExpiresAt: exp}}
}

func TestOAuthClient(t *testing.T) {
	s, c := oauthFixture(t)
	ctx := context.Background()
	got, err := s.OAuthClient(ctx, c.ID)
	if err != nil || got.Name != "agent" || !slices.Equal(got.RedirectURIs, c.RedirectURIs) {
		t.Fatalf("OAuthClient = %+v, %v", got, err)
	}
	if _, err := s.OAuthClient(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown client err = %v", err)
	}
	if n, err := s.CountOAuthClients(ctx); n != 1 || err != nil {
		t.Errorf("CountOAuthClients = %d, %v", n, err)
	}
}

func TestRedeemOAuthCode(t *testing.T) {
	s, c := oauthFixture(t)
	ctx := context.Background()
	g := grantFor(t, s, c.ID, 1, "1")

	got, u, err := s.OAuthAccessToken(ctx, "access-1")
	if err != nil || got.ID != g.ID || got.ClientName != "agent" || u.Login != "Alice" {
		t.Fatalf("OAuthAccessToken = %+v %+v, %v", got, u, err)
	}
	if _, _, err := s.OAuthAccessToken(ctx, "refresh-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("refresh token used as access token: %v", err)
	}

	// Redeeming again revokes the grant.
	_, err = s.RedeemOAuthCode(ctx, "code-1", func(OAuthCode) (OAuthGrant, []OAuthToken, error) {
		t.Fatal("issue called for a redeemed code")
		return OAuthGrant{}, nil, nil
	})
	if !errors.Is(err, ErrReplayed) {
		t.Fatalf("second redemption err = %v", err)
	}
	if _, _, err := s.OAuthAccessToken(ctx, "access-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("token survived code replay: %v", err)
	}

	// A failed issue spends the code.
	mustCode(t, s, OAuthCode{Hash: "code-2", ClientID: c.ID, GitHubID: 1})
	boom := errors.New("boom")
	if _, err := s.RedeemOAuthCode(ctx, "code-2", func(OAuthCode) (OAuthGrant, []OAuthToken, error) {
		return OAuthGrant{}, nil, boom
	}); !errors.Is(err, boom) {
		t.Fatalf("issue error = %v", err)
	}
	if _, err := s.RedeemOAuthCode(ctx, "code-2", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("spent code err = %v", err)
	}

	// Expired codes are not found.
	mustCode(t, s, OAuthCode{Hash: "code-3", ClientID: c.ID, GitHubID: 1, ExpiresAt: time.Now().Add(-time.Second)})
	if _, err := s.RedeemOAuthCode(ctx, "code-3", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired code err = %v", err)
	}
}

func TestRotateOAuthRefreshToken(t *testing.T) {
	s, c := oauthFixture(t)
	ctx := context.Background()
	g := grantFor(t, s, c.ID, 1, "1")

	got, err := s.RotateOAuthRefreshToken(ctx, "refresh-1", func(g OAuthGrant, u User) ([]OAuthToken, error) {
		if u.Login != "Alice" || g.ClientID != c.ID {
			t.Errorf("issue got %+v %+v", g, u)
		}
		return tokens("2"), nil
	})
	if err != nil || got.ID != g.ID || got.LastUsedAt == nil {
		t.Fatalf("rotate = %+v, %v", got, err)
	}
	if _, _, err := s.OAuthAccessToken(ctx, "access-2"); err != nil {
		t.Fatalf("new access token: %v", err)
	}

	// A failing issue leaves the token usable.
	boom := errors.New("boom")
	if _, err := s.RotateOAuthRefreshToken(ctx, "refresh-2", func(OAuthGrant, User) ([]OAuthToken, error) {
		return nil, boom
	}); !errors.Is(err, boom) {
		t.Fatalf("issue error = %v", err)
	}

	// Reusing the rotated token revokes the grant.
	if _, err := s.RotateOAuthRefreshToken(ctx, "refresh-1", nil); !errors.Is(err, ErrReplayed) {
		t.Fatalf("reuse err = %v", err)
	}
	for _, h := range []string{"access-1", "access-2"} {
		if _, _, err := s.OAuthAccessToken(ctx, h); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s survived reuse: %v", h, err)
		}
	}
	if _, err := s.RotateOAuthRefreshToken(ctx, "refresh-2", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("refresh after revoke err = %v", err)
	}
}

func TestOAuthGrantsAndRevocation(t *testing.T) {
	s, c := oauthFixture(t)
	ctx := context.Background()
	g1 := grantFor(t, s, c.ID, 1, "1")
	g2 := grantFor(t, s, c.ID, 1, "2")
	grantFor(t, s, c.ID, 2, "3")

	grants, err := s.OAuthGrants(ctx, 1)
	if err != nil || len(grants) != 2 {
		t.Fatalf("OAuthGrants = %+v, %v", grants, err)
	}
	if err := s.TouchOAuthGrant(ctx, g1.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteOAuthGrant(ctx, g1.ID, 2); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting another user's grant: %v", err)
	}
	if err := s.DeleteOAuthGrant(ctx, g1.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeOAuthToken(ctx, "refresh-2", "other-client"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.OAuthAccessToken(ctx, "access-2"); err != nil {
		t.Errorf("revoked by the wrong client: %v", err)
	}
	if err := s.RevokeOAuthToken(ctx, "access-2", c.ID); err != nil {
		t.Fatal(err)
	}
	if grants, _ := s.OAuthGrants(ctx, 1); len(grants) != 0 {
		t.Errorf("grants after revoke = %+v (g2 %s)", grants, g2.ID)
	}

	if err := s.DeleteDisallowedOAuthGrants(ctx, []string{"alice"}); err != nil {
		t.Fatal(err)
	}
	if grants, _ := s.OAuthGrants(ctx, 2); len(grants) != 0 {
		t.Errorf("disallowed user's grants kept: %+v", grants)
	}
}

func TestDeleteExpiredOAuth(t *testing.T) {
	s, c := oauthFixture(t)
	ctx := context.Background()
	live := grantFor(t, s, c.ID, 1, "1")
	mustCode(t, s, OAuthCode{Hash: "dead-code", ClientID: c.ID, GitHubID: 1, RedirectURI: "r", Scope: "read", Resource: "res"})
	dead, err := s.RedeemOAuthCode(ctx, "dead-code", func(c OAuthCode) (OAuthGrant, []OAuthToken, error) {
		return OAuthGrant{ClientID: c.ClientID, GitHubID: 1, RedirectURI: "r", Scope: "read", Resource: "res"},
			[]OAuthToken{{Hash: "old", Kind: OAuthRefresh, ExpiresAt: time.Now().Add(-time.Second)}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	unused, err := s.CreateOAuthClient(ctx, OAuthClient{RedirectURIs: []string{"https://x/cb"}})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteExpiredOAuth(ctx, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM oauth_grants WHERE id = ?`, dead.ID).Scan(&n)
	if n != 0 {
		t.Error("grant without refresh tokens kept")
	}
	if _, _, err := s.OAuthAccessToken(ctx, "access-1"); err != nil {
		t.Errorf("live grant %s removed: %v", live.ID, err)
	}
	if _, err := s.OAuthClient(ctx, unused.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("stale unused client kept: %v", err)
	}
	if _, err := s.OAuthClient(ctx, c.ID); err != nil {
		t.Errorf("client with grants removed: %v", err)
	}
}
