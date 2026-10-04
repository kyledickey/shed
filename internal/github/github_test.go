package github

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"a":1}`)
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write(body)
	good := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	tests := []struct {
		name, secret, header string
		want                 bool
	}{
		{"valid", "secret", good, true},
		{"wrong secret", "other", good, false},
		{"empty secret", "", good, false},
		{"missing prefix", "secret", strings.TrimPrefix(good, "sha256="), false},
		{"bad hex", "secret", "sha256=zz", false},
		{"empty header", "secret", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VerifySignature(tt.secret, body, tt.header); got != tt.want {
				t.Errorf("VerifySignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParsePush(t *testing.T) {
	body := `{
		"ref": "refs/heads/main",
		"after": "abc123",
		"deleted": false,
		"repository": {"full_name": "octo/app"},
		"head_commit": {
			"id": "abc123",
			"message": "Fix bug\n\nLong description",
			"author": {"name": "Octo Cat", "username": "octocat"}
		}
	}`
	got, err := ParsePush([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	want := PushEvent{Repo: "octo/app", Branch: "main", SHA: "abc123", Message: "Fix bug", Author: "Octo Cat"}
	if got != want {
		t.Errorf("ParsePush() = %+v, want %+v", got, want)
	}

	got, err = ParsePush([]byte(`{"ref":"refs/tags/v1","after":"0","repository":{"full_name":"octo/app"}}`))
	if err != nil || got.Branch != "" {
		t.Errorf("tag push = %+v, %v; want empty Branch", got, err)
	}

	got, err = ParsePush([]byte(`{"ref":"refs/heads/gone","deleted":true,"head_commit":null,"repository":{"full_name":"octo/app"}}`))
	if err != nil || !got.Deleted || got.Branch != "gone" {
		t.Errorf("delete push = %+v, %v", got, err)
	}

	if _, err := ParsePush([]byte("not json")); err == nil {
		t.Error("ParsePush(invalid) succeeded")
	}
}

func TestManifest(t *testing.T) {
	b, err := Manifest("https://shed.example.com/", "shed")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	checks := map[string]any{
		"name":         "shed",
		"url":          "https://shed.example.com",
		"redirect_url": "https://shed.example.com/api/setup/github/callback",
		"setup_url":    "https://shed.example.com/",
		"public":       true,
	}
	for k, want := range checks {
		if m[k] != want {
			t.Errorf("manifest[%q] = %v, want %v", k, m[k], want)
		}
	}
	if hook := m["hook_attributes"].(map[string]any)["url"]; hook != "https://shed.example.com/api/github/webhook" {
		t.Errorf("hook url = %v", hook)
	}
	if cb := m["callback_urls"]; !reflect.DeepEqual(cb, []any{"https://shed.example.com/api/auth/callback"}) {
		t.Errorf("callback_urls = %v", cb)
	}
	if ev := m["default_events"]; !reflect.DeepEqual(ev, []any{"push"}) {
		t.Errorf("default_events = %v", ev)
	}
	perms := m["default_permissions"].(map[string]any)
	for _, p := range []string{"contents", "metadata", "checks", "statuses"} {
		if perms[p] != "read" {
			t.Errorf("permission %s = %v, want read", p, perms[p])
		}
	}
}

// fakeGitHub serves the GitHub endpoints used by the tests.
type fakeGitHub struct {
	srv        *httptest.Server
	key        *rsa.PrivateKey
	tokenCalls atomic.Int32
	checkRuns  string // JSON array of check runs.
	status     string // JSON combined status.
}

func newFakeGitHub(t *testing.T) (*fakeGitHub, *Client) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeGitHub{key: key, checkRuns: `[]`, status: `{"state":"pending","total_count":0}`}

	mux := http.NewServeMux()
	jsonOut := func(w http.ResponseWriter, v string) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, v)
	}
	requireApp := func(w http.ResponseWriter, r *http.Request) bool {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		claims := &jwt.RegisteredClaims{}
		_, err := jwt.ParseWithClaims(tok, claims, func(*jwt.Token) (any, error) { return &key.PublicKey, nil },
			jwt.WithValidMethods([]string{"RS256"}))
		if err != nil || claims.Issuer != "42" {
			http.Error(w, "bad app jwt", http.StatusUnauthorized)
			return false
		}
		return true
	}
	requireInstall := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer inst-token" {
			http.Error(w, "bad installation token", http.StatusUnauthorized)
			return false
		}
		return true
	}

	mux.HandleFunc("POST /app/installations/7/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		if !requireApp(w, r) {
			return
		}
		f.tokenCalls.Add(1)
		w.WriteHeader(http.StatusCreated)
		jsonOut(w, fmt.Sprintf(`{"token":"inst-token","expires_at":%q}`, time.Now().Add(time.Hour).Format(time.RFC3339)))
	})
	mux.HandleFunc("GET /repos/octo/{repo}/installation", func(w http.ResponseWriter, r *http.Request) {
		if requireApp(w, r) {
			jsonOut(w, `{"id":7}`)
		}
	})
	mux.HandleFunc("GET /app/installations", func(w http.ResponseWriter, r *http.Request) {
		if requireApp(w, r) {
			jsonOut(w, `[{"id":7}]`)
		}
	})
	mux.HandleFunc("GET /installation/repositories", func(w http.ResponseWriter, r *http.Request) {
		if requireInstall(w, r) {
			jsonOut(w, `{"total_count":2,"repositories":[
				{"full_name":"octo/zeta","default_branch":"main","private":true},
				{"full_name":"octo/alpha","default_branch":"dev","private":false}]}`)
		}
	})
	mux.HandleFunc("GET /repos/octo/app/branches", func(w http.ResponseWriter, r *http.Request) {
		if requireInstall(w, r) {
			jsonOut(w, `[{"name":"main"},{"name":"dev"}]`)
		}
	})
	mux.HandleFunc("GET /repos/octo/app/commits/{ref}", func(w http.ResponseWriter, r *http.Request) {
		if requireInstall(w, r) {
			jsonOut(w, `{"sha":"abc123","commit":{"message":"Subject\n\nBody","author":{"name":"Octo Cat"}}}`)
		}
	})
	mux.HandleFunc("GET /repos/octo/app/commits/{ref}/check-runs", func(w http.ResponseWriter, r *http.Request) {
		if requireInstall(w, r) {
			jsonOut(w, `{"total_count":1,"check_runs":`+f.checkRuns+`}`)
		}
	})
	mux.HandleFunc("GET /repos/octo/app/commits/{ref}/status", func(w http.ResponseWriter, r *http.Request) {
		if requireInstall(w, r) {
			jsonOut(w, f.status)
		}
	})
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("code") != "good" || r.Form.Get("client_id") != "cid" ||
			r.Form.Get("client_secret") != "csecret" || r.Form.Get("redirect_uri") != "http://x/cb" {
			jsonOut(w, `{"error":"bad_verification_code","error_description":"nope"}`)
			return
		}
		jsonOut(w, `{"access_token":"user-token"}`)
	})
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer user-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		jsonOut(w, `{"id":99,"login":"octocat","name":"Octo Cat","avatar_url":"http://avatar"}`)
	})
	mux.HandleFunc("POST /app-manifests/{code}/conversions", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		jsonOut(w, `{"id":42,"slug":"shed-app","client_id":"cid","client_secret":"csecret","webhook_secret":"whs","pem":"PEM DATA"}`)
	})

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)

	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	c, err := New(App{ID: 42, Slug: "shed-app", ClientID: "cid", ClientSecret: "csecret", PrivateKey: pemKey}, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.apiURL = f.srv.URL + "/"
	c.webURL = f.srv.URL
	return f, c
}

func TestNewRejectsBadKey(t *testing.T) {
	if _, err := New(App{ID: 1, PrivateKey: []byte("junk")}, nil); err == nil {
		t.Error("New with invalid key succeeded")
	}
}

func TestConvertManifest(t *testing.T) {
	f, _ := newFakeGitHub(t)
	app, err := convertManifest(t.Context(), http.DefaultClient, f.srv.URL+"/", "somecode")
	if err != nil {
		t.Fatal(err)
	}
	want := App{ID: 42, Slug: "shed-app", ClientID: "cid", ClientSecret: "csecret", WebhookSecret: "whs", PrivateKey: []byte("PEM DATA")}
	if !reflect.DeepEqual(app, want) {
		t.Errorf("convertManifest() = %+v, want %+v", app, want)
	}
}

func TestReposSorted(t *testing.T) {
	_, c := newFakeGitHub(t)
	repos, err := c.Repos(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []Repo{
		{FullName: "octo/alpha", DefaultBranch: "dev"},
		{FullName: "octo/zeta", DefaultBranch: "main", Private: true},
	}
	if !reflect.DeepEqual(repos, want) {
		t.Errorf("Repos() = %+v, want %+v", repos, want)
	}
}

func TestBranchesAndCommit(t *testing.T) {
	_, c := newFakeGitHub(t)
	branches, err := c.Branches(t.Context(), "octo/app")
	if err != nil || !reflect.DeepEqual(branches, []string{"main", "dev"}) {
		t.Errorf("Branches() = %v, %v", branches, err)
	}
	commit, err := c.Commit(t.Context(), "octo/app", "main")
	if err != nil || commit != (Commit{SHA: "abc123", Message: "Subject", Author: "Octo Cat"}) {
		t.Errorf("Commit() = %+v, %v", commit, err)
	}
	if _, err := c.Branches(t.Context(), "badname"); err == nil {
		t.Error("Branches(invalid name) succeeded")
	}
}

func TestInstallationTokenCached(t *testing.T) {
	f, c := newFakeGitHub(t)
	for range 3 {
		if _, err := c.Branches(t.Context(), "octo/app"); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.tokenCalls.Load(); n != 1 {
		t.Errorf("token requests = %d, want 1", n)
	}

	// An expiring token is replaced.
	c.mu.Lock()
	c.tokens[7] = installationToken{value: "stale", expires: time.Now().Add(time.Minute)}
	c.mu.Unlock()
	if _, err := c.Branches(t.Context(), "octo/app"); err != nil {
		t.Fatal(err)
	}
	if n := f.tokenCalls.Load(); n != 2 {
		t.Errorf("token requests after expiry = %d, want 2", n)
	}
}

func TestCloneURL(t *testing.T) {
	_, c := newFakeGitHub(t)
	got, err := c.CloneURL(t.Context(), "octo/app")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	pw, _ := u.User.Password()
	if u.User.Username() != "x-access-token" || pw != "inst-token" || u.Path != "/octo/app.git" {
		t.Errorf("CloneURL() = %q", got)
	}
}

func TestCIStatus(t *testing.T) {
	tests := []struct {
		name, runs, status string
		want               CIState
	}{
		{"none", `[]`, `{"state":"pending","total_count":0}`, CINone},
		{"runs pass", `[{"status":"completed","conclusion":"success"},{"status":"completed","conclusion":"skipped"}]`,
			`{"state":"pending","total_count":0}`, CISuccess},
		{"run pending", `[{"status":"completed","conclusion":"success"},{"status":"in_progress"}]`,
			`{"state":"pending","total_count":0}`, CIPending},
		{"run failed", `[{"status":"completed","conclusion":"failure"},{"status":"queued"}]`,
			`{"state":"pending","total_count":0}`, CIFailure},
		{"status only success", `[]`, `{"state":"success","total_count":2}`, CISuccess},
		{"status pending", `[{"status":"completed","conclusion":"success"}]`, `{"state":"pending","total_count":1}`, CIPending},
		{"status error", `[]`, `{"state":"error","total_count":1}`, CIFailure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, c := newFakeGitHub(t)
			f.checkRuns, f.status = tt.runs, tt.status
			got, err := c.CIStatus(t.Context(), "octo/app", "abc123")
			if err != nil || got != tt.want {
				t.Errorf("CIStatus() = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestOAuth(t *testing.T) {
	_, c := newFakeGitHub(t)
	u, err := url.Parse(c.AuthorizeURL("http://x/cb", "st"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/login/oauth/authorize" || u.Query().Get("client_id") != "cid" ||
		u.Query().Get("redirect_uri") != "http://x/cb" || u.Query().Get("state") != "st" {
		t.Errorf("AuthorizeURL() = %v", u)
	}

	user, err := c.ExchangeUser(t.Context(), "good", "http://x/cb")
	if err != nil {
		t.Fatal(err)
	}
	if want := (User{ID: 99, Login: "octocat", Name: "Octo Cat", AvatarURL: "http://avatar"}); user != want {
		t.Errorf("ExchangeUser() = %+v, want %+v", user, want)
	}
	if _, err := c.ExchangeUser(t.Context(), "bad", "http://x/cb"); err == nil {
		t.Error("ExchangeUser(bad code) succeeded")
	}
}
