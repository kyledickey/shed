package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/store"
)

// guestDB returns a database with shed's real schema.
func guestDB(t *testing.T) *sql.DB {
	t.Helper()
	file := filepath.Join(t.TempDir(), "shed.db")
	st, err := store.Open(file, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	db, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func execSQL(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func TestSessionSQLRestoresDeletedSession(t *testing.T) {
	db := guestDB(t)
	execSQL(t, db, `INSERT INTO users (github_id, login, created_at) VALUES (7, 'lab', '2026-01-01T00:00:00.000Z')`)
	stmt := sessionSQL(lab{Session: "token", Login: "lab"}, time.Now())
	count := func() (n int) {
		t.Helper()
		if err := db.QueryRow(`SELECT count(*) FROM sessions WHERE github_id = 7`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	execSQL(t, db, stmt)
	execSQL(t, db, stmt)
	if got := count(); got != 1 {
		t.Errorf("after two runs, %d sessions", got)
	}
	execSQL(t, db, `DELETE FROM sessions`)
	execSQL(t, db, stmt)
	if got := count(); got != 1 {
		t.Errorf("after signing out, %d sessions", got)
	}
}

func TestMoveDomainsSQL(t *testing.T) {
	db := guestDB(t)
	execSQL(t, db, `INSERT INTO projects (id, name, created_at) VALUES ('p', 'p', '')`)
	execSQL(t, db, `INSERT INTO services (id, project_id, name, kind, created_at) VALUES ('s', 'p', 's', 'app', '')`)
	execSQL(t, db, `INSERT INTO domains (id, service_id, host, generated, created_at) VALUES
		('a', 's', 'web-p.lab1.localhost', 1, ''),
		('b', 's', 'custom.lab1.localhost', 0, ''),
		('c', 's', 'web.example.com', 1, '')`)

	execSQL(t, db, moveDomainsSQL(slot(3)))

	for id, want := range map[string]string{"a": "web-p.lab3.localhost", "b": "custom.lab1.localhost", "c": "web.example.com"} {
		var got string
		if err := db.QueryRow(`SELECT host FROM domains WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("domain %s = %q, want %q", id, got, want)
		}
	}
}

func TestParseSlot(t *testing.T) {
	tests := []struct {
		in   string
		want slot
		ok   bool
	}{
		{"lab1", 1, true},
		{"3", 3, true},
		{"lab5", 5, true},
		{"lab6", 6, false},
		{"lab0", 0, false},
		{"labx", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		got, ok := parseSlot(tt.in)
		if ok != tt.ok || ok && got != tt.want {
			t.Errorf("parseSlot(%q) = %d, %v; want %d, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSlotPortsDoNotCollide(t *testing.T) {
	seen := map[int]string{}
	for s := slot(1); s <= slotCount; s++ {
		for _, p := range []int{s.dashPort(), s.sshPort(), s.httpPort(), s.httpsPort()} {
			if other, dup := seen[p]; dup {
				t.Errorf("port %d used by %s and %s", p, other, s.name())
			}
			seen[p] = s.name()
		}
	}
}

func TestScenariosParse(t *testing.T) {
	all, err := loadScenarios()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) == 0 {
		t.Fatal("no scenarios")
	}
	for _, s := range all {
		if s.Description == "" || s.CPUs == 0 || s.MemoryGB == 0 || s.DiskGB == 0 {
			t.Errorf("scenario %s is missing its description or size", s.Name)
		}
		for _, p := range s.Projects {
			for _, svc := range p.expand() {
				if svc.Kind == "app" && svc.Image == "" && svc.Repo == "" {
					t.Errorf("%s/%s has no source", s.Name, svc.Name)
				}
			}
		}
	}
}

func TestParseScenarioRejectsUnknownFields(t *testing.T) {
	if _, err := parseScenario([]byte("cpu = 4\n"), "typo"); err == nil {
		t.Error("a misspelled field parsed without an error")
	}
}

func TestExpandCount(t *testing.T) {
	p := project{Services: []service{
		{Name: "db", Kind: "postgres"},
		{Name: "gen", Generate: "traces", Rate: 10, Count: 2},
	}}
	got := p.expand()
	if len(got) != 3 || got[1].Name != "gen-1" || got[2].Name != "gen-2" {
		t.Fatalf("expand() = %+v", got)
	}
	if got[0].Kind != "postgres" || got[1].Kind != "app" {
		t.Errorf("kinds = %q, %q", got[0].Kind, got[1].Kind)
	}
	if got[1].Image != generatorImage || got[1].Vars["GEN_RATE"] != "10" || got[1].Vars["GEN_SCRIPT"] == "" {
		t.Errorf("generator not configured: %+v", got[1])
	}
}

func TestNeedsGitHub(t *testing.T) {
	image := scenario{Projects: []project{{Services: []service{{Image: "x"}}}}}
	repo := scenario{Projects: []project{{Services: []service{{Image: "x"}, {Repo: "o/r"}}}}}
	if image.needsGitHub() || !repo.needsGitHub() {
		t.Errorf("needsGitHub: image %v, repo %v", image.needsGitHub(), repo.needsGitHub())
	}
}

func TestParseSetupToken(t *testing.T) {
	log := `time=2026-10-31T01:07:30.220Z level=WARN msg="generated the key"
time=2026-10-31T01:07:30.237Z level=WARN msg="GitHub App is not configured; open the setup page and enter the setup token" url=http://lab1.localhost:18100/setup token=OLDOLDOLDOLDOLDOLDOLD2
time=2026-10-31T01:08:30.237Z level=WARN msg="GitHub App is not configured; open the setup page and enter the setup token" url=http://lab1.localhost:18100/setup token=YIIPUOB22FFAUJKNG7TDZB6YAJ
`
	got, ok := parseSetupToken(log)
	if !ok || got != "YIIPUOB22FFAUJKNG7TDZB6YAJ" {
		t.Errorf("parseSetupToken = %q, %v; want the latest token", got, ok)
	}
	if _, ok := parseSetupToken("level=INFO msg=listening"); ok {
		t.Error("found a token in a log without one")
	}
}

func TestHandoffSetsCookieAndRedirects(t *testing.T) {
	l := lab{Session: "secret", URL: "http://lab1.localhost:18100"}
	done := make(chan struct{}, 1)
	rec := httptest.NewRecorder()
	handoff(l, done).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	res := rec.Result()
	if got := res.Header.Get("Location"); got != l.URL+"/" {
		t.Errorf("Location = %q", got)
	}
	cookies := res.Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookie || cookies[0].Value != "secret" || !cookies[0].HttpOnly {
		t.Errorf("cookies = %+v", cookies)
	}
	select {
	case <-done:
	default:
		t.Error("the handoff didn't report that it was used")
	}
}

func TestLabManifestCoversEverySlot(t *testing.T) {
	raw, err := labManifest("shed-lab-me", "http://127.0.0.1:1234/callback")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		RedirectURL  string   `json:"redirect_url"`
		CallbackURLs []string `json:"callback_urls"`
		Public       bool     `json:"public"`
		Hook         struct {
			URL string `json:"url"`
		} `json:"hook_attributes"`
		Permissions map[string]string `json:"default_permissions"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.RedirectURL != "http://127.0.0.1:1234/callback" || m.Public {
		t.Errorf("redirect %q, public %v", m.RedirectURL, m.Public)
	}
	// GitHub allows at most 10 callback URLs.
	if len(m.CallbackURLs) > 10 {
		t.Errorf("%d callback URLs", len(m.CallbackURLs))
	}
	joined := strings.Join(m.CallbackURLs, " ")
	for s := slot(1); s <= slotCount; s++ {
		for _, want := range []string{s.localURL(), s.publicURL()} {
			if !strings.Contains(joined, want+"/api/auth/callback") {
				t.Errorf("no callback for %s", want)
			}
		}
	}
	if !strings.HasPrefix(m.Hook.URL, "https://") || m.Permissions["contents"] != "read" {
		t.Errorf("shed's manifest fields were lost: %+v", m)
	}
}

func TestGuestConfigUsesSlotPorts(t *testing.T) {
	s := slot(2)
	conf := guestConfig(lab{Slot: s, URL: s.localURL(), Login: "me"})
	for _, want := range []string{`listen = "0.0.0.0:18200"`, `url = "http://lab2.localhost:18200"`, "http_port = 18280", "https_port = 18243", `base_domain = "lab2.localhost"`, `allowed_users = ["me"]`} {
		if !strings.Contains(conf, want) {
			t.Errorf("config is missing %q:\n%s", want, conf)
		}
	}
}
