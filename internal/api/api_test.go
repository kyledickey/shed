package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/kyledickey/shed/internal/auth"
	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/github"
	"github.com/kyledickey/shed/internal/metrics"
	"github.com/kyledickey/shed/internal/store"
)

// fakeDeployer records deployments and service controls without running
// them.
type fakeDeployer struct {
	mu         sync.Mutex
	deploy     []store.Trigger
	controls   []string // "stop <id>", "start <id>", "restart <id>"
	controlErr error    // returned by the service controls
}

func (f *fakeDeployer) control(op, serviceID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.controls = append(f.controls, op+" "+serviceID)
	return f.controlErr
}

func (f *fakeDeployer) StopService(_ context.Context, id string) error { return f.control("stop", id) }
func (f *fakeDeployer) StartService(_ context.Context, id string) error {
	return f.control("start", id)
}
func (f *fakeDeployer) RestartService(_ context.Context, id string) error {
	return f.control("restart", id)
}

func (f *fakeDeployer) Deploy(_ context.Context, serviceID string, trigger store.Trigger, c deploy.Commit) (store.Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deploy = append(f.deploy, trigger)
	return store.Deployment{ServiceID: serviceID, Trigger: trigger, CommitSHA: c.SHA, Status: store.StatusQueued}, nil
}

func (f *fakeDeployer) triggers() []store.Trigger {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.Trigger(nil), f.deploy...)
}

func (f *fakeDeployer) Redeploy(context.Context, string) (store.Deployment, error) {
	return store.Deployment{}, deploy.ErrNoImage
}
func (f *fakeDeployer) Cancel(context.Context, string) (store.Deployment, error) {
	return store.Deployment{}, deploy.ErrNotInProgress
}
func (f *fakeDeployer) ServiceStatuses(context.Context, string) (map[string]deploy.ServiceStatus, error) {
	return map[string]deploy.ServiceStatus{}, nil
}
func (f *fakeDeployer) ApplyRoutes(context.Context) error           { return nil }
func (f *fakeDeployer) DeleteService(context.Context, string) error { return nil }
func (f *fakeDeployer) DeleteProject(context.Context, string) error { return nil }
func (f *fakeDeployer) DeleteVolume(context.Context, string) error  { return nil }
func (f *fakeDeployer) RuntimeLogs(context.Context, string, int, io.Writer) error {
	return deploy.ErrNoContainer
}
func (f *fakeDeployer) FollowLog(_ context.Context, _ string, line func(string), status func(store.DeploymentStatus)) error {
	line("hello")
	status(store.StatusActive)
	return nil
}

// fakeMetrics returns a fixed two-point series and records the range asked
// for.
type fakeMetrics struct {
	mu    sync.Mutex
	asked []metrics.Range
}

func (f *fakeMetrics) Query(_ context.Context, _ string, r metrics.Range) (metrics.Series, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, r)
	one := 1.5
	series := []*float64{nil, &one}
	return metrics.Series{
		Range: r, Start: time.Date(2026, 5, 1, 11, 0, 20, 0, time.UTC), Step: 20 * time.Second,
		CPU: series, Memory: series, NetRx: series, NetTx: series, DiskRead: series, DiskWrite: series,
	}, nil
}

type fixture struct {
	t        *testing.T
	st       *store.Store
	deployer *fakeDeployer
	metrics  *fakeMetrics
	github   *GitHubHolder
	handler  http.Handler
	cookie   *http.Cookie
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "shed.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	log := slog.New(slog.DiscardHandler)
	gh := &GitHubHolder{}
	authn := auth.New(AuthStore(st), func() (auth.OAuth, bool) { return nil, false }, "http://localhost", nil, log)
	f := &fixture{t: t, st: st, deployer: &fakeDeployer{}, metrics: &fakeMetrics{}, github: gh}
	srv, err := New(context.Background(), Config{
		Store:      st,
		Deployer:   f.deployer,
		Metrics:    f.metrics,
		Auth:       authn,
		GitHub:     gh,
		BaseURL:    "http://localhost",
		BaseDomain: "apps.example.com",
		Web: fstest.MapFS{
			"index.html":      {Data: []byte("<html>shed</html>")},
			"assets/app-1.js": {Data: []byte("console.log(1)")},
		},
		Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.handler = srv.Handler()

	// Sign in by creating a session directly, as auth would.
	ctx := context.Background()
	if err := st.UpsertUser(ctx, store.User{GitHubID: 1, Login: "octocat"}); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("token"))
	if err := st.CreateSession(ctx, hex.EncodeToString(sum[:]), 1, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	f.cookie = &http.Cookie{Name: "shed_session", Value: "token"}
	return f
}

// do sends a request with the session cookie; a non-empty body is sent as
// JSON.
func (f *fixture) do(method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(f.cookie)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func (f *fixture) decode(rec *httptest.ResponseRecorder, wantStatus int) map[string]any {
	f.t.Helper()
	if rec.Code != wantStatus {
		f.t.Fatalf("status = %d, want %d; body: %s", rec.Code, wantStatus, rec.Body)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		f.t.Fatalf("decode %s: %v", rec.Body, err)
	}
	return m
}

func TestRequiresSession(t *testing.T) {
	f := newFixture(t)
	req := httptest.NewRequest("GET", "/api/projects", nil)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestCreateProjectAndDatabase(t *testing.T) {
	f := newFixture(t)
	project := f.decode(f.do("POST", "/api/projects", `{"name":"My Shop"}`), http.StatusCreated)
	if project["name"] != "My Shop" || project["id"] == "" || project["createdAt"] == nil {
		t.Errorf("project = %v", project)
	}
	if services, ok := project["services"].([]any); !ok || len(services) != 0 {
		t.Errorf("services = %#v, want []", project["services"])
	}
	id := project["id"].(string)

	svc := f.decode(f.do("POST", "/api/projects/"+id+"/services", `{"name":"db","kind":"postgres"}`), http.StatusCreated)
	if svc["kind"] != "postgres" || svc["port"] != 5432.0 || svc["privateHost"] != "db" || svc["projectId"] != id {
		t.Errorf("service = %v", svc)
	}
	if svc["latestDeployment"] != nil || svc["status"] != "offline" {
		t.Errorf("latestDeployment = %v, status = %v; want null, offline", svc["latestDeployment"], svc["status"])
	}
	vols := svc["volumes"].([]any)
	if len(vols) != 1 || vols[0].(map[string]any)["mountPath"] != "/var/lib/postgresql" {
		t.Errorf("volumes = %v", vols)
	}
	if domains, ok := svc["domains"].([]any); !ok || len(domains) != 0 {
		t.Errorf("domains = %#v, want []", svc["domains"])
	}
	if got := f.deployer.triggers(); len(got) != 1 || got[0] != store.TriggerCreate {
		t.Errorf("deploys = %v, want [create]", got)
	}

	vars := f.decode(f.do("GET", "/api/services/"+svc["id"].(string)+"/variables", ""), http.StatusOK)
	if vars["POSTGRES_PASSWORD"] == "" || !strings.Contains(vars["DATABASE_URL"].(string), "${{SHED_PRIVATE_DOMAIN}}") {
		t.Errorf("variables = %v", vars)
	}

	dom := f.decode(f.do("POST", "/api/services/"+svc["id"].(string)+"/domains", `{}`), http.StatusCreated)
	if dom["host"] != "db-my-shop.apps.example.com" || dom["generated"] != true {
		t.Errorf("domain = %v", dom)
	}

	full := f.decode(f.do("GET", "/api/projects/"+id, ""), http.StatusOK)
	if services := full["services"].([]any); len(services) != 1 || services[0].(map[string]any)["volumes"] == nil {
		t.Errorf("project services = %v, want one full service", services)
	}
}

func TestValidation(t *testing.T) {
	f := newFixture(t)
	project := f.decode(f.do("POST", "/api/projects", `{"name":"p"}`), http.StatusCreated)
	base := "/api/projects/" + project["id"].(string) + "/services"

	tests := []struct {
		name, body string
		want       int
	}{
		{"bad name", `{"name":"Bad_Name","kind":"redis"}`, http.StatusBadRequest},
		{"bad kind", `{"name":"x","kind":"oracle"}`, http.StatusBadRequest},
		{"app without source", `{"name":"x","kind":"app"}`, http.StatusBadRequest},
		{"repo app without GitHub", `{"name":"x","kind":"app","repo":"o/r","branch":"main"}`, http.StatusConflict},
	}
	for _, tt := range tests {
		if rec := f.do("POST", base, tt.body); rec.Code != tt.want {
			t.Errorf("%s: status = %d, want %d; body: %s", tt.name, rec.Code, tt.want, rec.Body)
		}
	}

	req := httptest.NewRequest("POST", "/api/projects", strings.NewReader(`{"name":"q"}`))
	req.Header.Set("Content-Type", "text/plain")
	req.AddCookie(f.cookie)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain body: status = %d, want 415", rec.Code)
	}
}

func TestDeploymentLogStream(t *testing.T) {
	f := newFixture(t)
	dep := createApp(t, f.st)
	d, err := f.st.CreateDeployment(context.Background(), store.Deployment{ServiceID: dep.ID, Status: store.StatusActive, Trigger: store.TriggerManual})
	if err != nil {
		t.Fatal(err)
	}
	rec := f.do("GET", "/api/deployments/"+d.ID+"/logs", "")
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	want := "event: log\ndata: hello\n\nevent: status\ndata: {\"status\":\"active\"}\n\nevent: end\ndata: \n\n"
	if rec.Body.String() != want {
		t.Errorf("body = %q, want %q", rec.Body, want)
	}
}

func TestServiceMetrics(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	base := "/api/services/" + svc.ID + "/metrics"

	rec := f.do("GET", base, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body)
	}
	want := `{"range":"1h","start":"2026-05-01T11:00:20Z","step":20,"cpuLimit":0,"memoryLimit":0,` +
		`"cpu":[null,1.5],"memory":[null,1.5],"netRx":[null,1.5],"netTx":[null,1.5],"diskRead":[null,1.5],"diskWrite":[null,1.5]}` + "\n"
	if rec.Body.String() != want {
		t.Errorf("body = %s, want %s", rec.Body, want)
	}

	f.decode(f.do("GET", base+"?range=7d", ""), http.StatusOK)
	if got := f.metrics.asked; len(got) != 2 || got[0] != metrics.Range1h || got[1] != metrics.Range7d {
		t.Errorf("ranges asked = %v, want [1h 7d]", got)
	}
	f.decode(f.do("GET", base+"?range=2h", ""), http.StatusBadRequest)
	f.decode(f.do("GET", "/api/services/nope/metrics", ""), http.StatusNotFound)
}

func TestServiceControls(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	tests := []struct {
		op, service string
		err         error
		wantStatus  int
		wantError   string
	}{
		{"stop", svc.ID, nil, http.StatusOK, ""},
		{"start", svc.ID, nil, http.StatusOK, ""},
		{"restart", svc.ID, nil, http.StatusOK, ""},
		{"start", svc.ID, deploy.ErrNoContainer, http.StatusConflict, "nothing to run; deploy the service first"},
		{"restart", svc.ID, deploy.ErrNoContainer, http.StatusConflict, "nothing to run; deploy the service first"},
		{"restart", svc.ID, deploy.ErrServiceStopped, http.StatusConflict, "service is stopped; start it instead"},
		{"stop", "nope", store.ErrNotFound, http.StatusNotFound, "not found"},
		{"stop", "nope", nil, http.StatusNotFound, "not found"},
	}
	for _, tt := range tests {
		t.Run(tt.op+" "+tt.service, func(t *testing.T) {
			f.deployer.mu.Lock()
			f.deployer.controls, f.deployer.controlErr = nil, tt.err
			f.deployer.mu.Unlock()

			body := f.decode(f.do("POST", "/api/services/"+tt.service+"/"+tt.op, ""), tt.wantStatus)
			if tt.wantError != "" && body["error"] != tt.wantError {
				t.Errorf("error = %v, want %q", body["error"], tt.wantError)
			}
			if tt.wantStatus == http.StatusOK && (body["id"] != svc.ID || body["status"] == nil) {
				t.Errorf("body = %v, want the service", body)
			}
			if want := tt.op + " " + tt.service; len(f.deployer.controls) != 1 || f.deployer.controls[0] != want {
				t.Errorf("controls = %v, want [%s]", f.deployer.controls, want)
			}
		})
	}
}

func createApp(t *testing.T, st *store.Store) store.Service {
	t.Helper()
	ctx := context.Background()
	p, err := st.CreateProject(ctx, "p-"+store.NewID())
	if err != nil {
		t.Fatal(err)
	}
	svc, err := st.CreateService(ctx, store.Service{
		ProjectID: p.ID, Name: "web", Kind: "app", Repo: "Octo/App", Branch: "main", AutoDeploy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestWebhook(t *testing.T) {
	f := newFixture(t)
	createApp(t, f.st)
	body := `{"ref":"refs/heads/main","after":"abc123","repository":{"full_name":"octo/app"},
		"head_commit":{"id":"abc123","message":"Fix it\n\nDetails","author":{"name":"Mona"}}}`

	send := func(signature string) int {
		req := httptest.NewRequest("POST", "/api/github/webhook", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("X-Hub-Signature-256", signature)
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := send("sha256=00"); code != http.StatusConflict {
		t.Errorf("unconfigured: status = %d, want 409", code)
	}

	f.github.Set(newGitHubClient(t, "s3cret"))
	if code := send("sha256=00"); code != http.StatusUnauthorized {
		t.Errorf("bad signature: status = %d, want 401", code)
	}
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write([]byte(body))
	if code := send("sha256=" + hex.EncodeToString(mac.Sum(nil))); code != http.StatusAccepted {
		t.Errorf("good signature: status = %d, want 202", code)
	}
	if got := f.deployer.triggers(); len(got) != 1 || got[0] != store.TriggerPush {
		t.Errorf("deploys = %v, want [push]", got)
	}
}

func newGitHubClient(t *testing.T, webhookSecret string) *github.Client {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	c, err := github.New(github.App{ID: 1, Slug: "shed-test", WebhookSecret: webhookSecret, PrivateKey: pemKey}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSPA(t *testing.T) {
	f := newFixture(t)
	tests := []struct {
		path     string
		want     int
		contains string
	}{
		{"/", http.StatusOK, "shed"},
		{"/projects/abc", http.StatusOK, "shed"},
		{"/assets/app-1.js", http.StatusOK, "console"},
		{"/assets/missing.js", http.StatusNotFound, ""},
		{"/api/nope", http.StatusNotFound, `"error"`},
	}
	for _, tt := range tests {
		rec := f.do("GET", tt.path, "")
		if rec.Code != tt.want || !strings.Contains(rec.Body.String(), tt.contains) {
			t.Errorf("GET %s = %d %q, want %d containing %q", tt.path, rec.Code, rec.Body, tt.want, tt.contains)
		}
	}
	if cc := f.do("GET", "/assets/app-1.js", "").Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("asset Cache-Control = %q", cc)
	}
}

func TestSetupRequiresToken(t *testing.T) {
	f := newFixture(t)
	rec := f.do("GET", "/api/setup/github?token=wrong", "")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/setup?error=invalid_token" {
		t.Errorf("wrong token: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	setup := f.decode(f.do("GET", "/api/setup", ""), http.StatusOK)
	if setup["githubConfigured"] != false {
		t.Errorf("setup = %v", setup)
	}
}
