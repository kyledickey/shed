//go:build integration

// This test needs Docker and network access, and that no shed-telemetry-*
// containers or volumes exist. It runs the real Manager: enables the stack,
// signs in to Grafana through the /grafana/ proxy, disables and enables it
// again with data kept, and deletes the data:
//
//	go test -tags integration -run Integration -v -timeout 20m ./internal/observability/
package observability_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/kyledickey/shed/internal/api"
	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/auth"
	"github.com/kyledickey/shed/internal/control"
	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/grafana"
	"github.com/kyledickey/shed/internal/observability"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/victoria"
)

func freeAddr(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func TestIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	for _, c := range []string{"metrics", "logs", "traces", "grafana"} {
		name := "shed-telemetry-" + c
		if out, _ := exec.Command("docker", "ps", "-aq", "--filter", "name=^"+name+"$").Output(); len(out) > 0 {
			t.Fatalf("container %s exists; this test would replace it", name)
		}
		if exec.Command("docker", "volume", "inspect", name).Run() == nil {
			t.Fatalf("volume %s exists; this test would delete it", name)
		}
		t.Cleanup(func() {
			if t.Failed() {
				logs, _ := exec.Command("docker", "logs", "--tail", "40", name).CombinedOutput()
				t.Logf("%s logs:\n%s", name, logs)
			}
			exec.Command("docker", "rm", "-f", name).Run()
			exec.Command("docker", "volume", "rm", "-f", name).Run()
		})
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	st, err := store.Open(filepath.Join(t.TempDir(), "shed.db"), make([]byte, store.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dc, err := docker.New()
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()

	// shed's handler is served before it exists, so Grafana's root URL can
	// name its address.
	var handler atomic.Value
	shed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.Load().(http.Handler).ServeHTTP(w, r)
	}))
	defer shed.Close()

	addrs := observability.Addrs{Metrics: freeAddr(t), Logs: freeAddr(t), Traces: freeAddr(t), Grafana: freeAddr(t)}
	dir := filepath.Join(t.TempDir(), "telemetry")
	metrics := victoria.NewMetrics("http://"+addrs.Metrics, nil)
	stack := observability.New(observability.Config{
		Store:   st,
		Docker:  dc,
		Metrics: metrics,
		Logs:    victoria.NewLogs("http://"+addrs.Logs, nil),
		Traces:  victoria.NewTraces("http://"+addrs.Traces, nil),
		Grafana: func(pw string) observability.GrafanaAPI {
			return grafana.New("http://"+addrs.Grafana+"/grafana", pw, &http.Client{Timeout: 30 * time.Second})
		},
		Addrs:        addrs,
		Dir:          dir,
		MinFreeBytes: 0,
		GrafanaURL:   shed.URL + "/grafana/",
		OTLPEndpoint: "http://localhost:4318",
		Log:          log,
	})
	auditLog := audit.New(st, log)
	plane := control.New(control.Config{Store: st, Audit: auditLog, Telemetry: stack, BaseURL: shed.URL, Log: log})
	authn := auth.New(api.AuthStore(st), auditLog, func() (auth.OAuth, bool) { return nil, false }, shed.URL, []string{"octocat"}, log)
	srv, err := api.New(ctx, api.Config{
		Control: plane, Settings: st, Auth: authn, GitHub: &api.GitHubHolder{}, BaseURL: shed.URL,
		Web: fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}, GrafanaAddr: addrs.Grafana, Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler.Store(srv.Handler())
	if err := st.UpsertUser(ctx, store.User{GitHubID: 1, Login: "octocat", Name: "The Octocat"}); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("token"))
	if err := st.CreateSession(ctx, hex.EncodeToString(sum[:]), 1, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	session := &http.Cookie{Name: "shed_session", Value: "token"}

	set := func(on bool) {
		t.Helper()
		s := observability.DefaultSettings()
		s.Enabled = on
		if _, err := plane.SetTelemetry(ctx, s); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		if err := stack.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
		t.Logf("reconciled enabled=%v in %s", on, time.Since(start).Round(time.Millisecond))
	}
	expectStates := func(want observability.State) {
		t.Helper()
		status, err := plane.Telemetry(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(status.Components) != 4 {
			t.Fatalf("components %+v", status.Components)
		}
		for _, c := range status.Components {
			if c.State != want {
				t.Errorf("%s is %s (%s), want %s", c.Name, c.State, c.Error, want)
			}
		}
	}
	get := func(path string, v any) *http.Response {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, "GET", shed.URL+path, nil)
		req.AddCookie(session)
		req.Header.Set("X-WEBAUTH-USER", "admin")
		resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if v != nil {
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s: %s", path, resp.Status)
			}
			if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
		}
		return resp
	}

	set(true)
	expectStates(observability.Running)

	var user struct{ Login, Name string }
	get("/grafana/api/user", &user)
	var orgs []struct{ Role string }
	get("/grafana/api/user/orgs", &orgs)
	if user.Login != "octocat" || user.Name != "The Octocat" || len(orgs) != 1 || orgs[0].Role != "Admin" {
		t.Errorf("signed in to Grafana as %+v with %+v, want octocat as Admin", user, orgs)
	}
	var folder struct{ UID, Title string }
	get("/grafana/api/folders/shed", &folder)
	var rules []struct{ FolderUID string }
	get("/grafana/api/v1/provisioning/alert-rules", &rules)
	if folder.Title != "shed" || len(rules) == 0 || rules[0].FolderUID != "shed" {
		t.Errorf("folder %+v, %d alert rules", folder, len(rules))
	}
	var sources []struct{ UID string }
	get("/grafana/api/datasources", &sources)
	if len(sources) != 3 {
		t.Errorf("data sources %+v", sources)
	}

	t.Run("live websocket", func(t *testing.T) {
		conn, err := net.Dial("tcp", strings.TrimPrefix(shed.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		req, _ := http.NewRequest("GET", shed.URL+"/grafana/api/live/ws", nil)
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Sec-WebSocket-Version", "13")
		req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		req.Header.Set("Origin", shed.URL)
		req.AddCookie(session)
		if err := req.Write(conn); err != nil {
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(bufio.NewReader(conn), req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusSwitchingProtocols {
			t.Errorf("Grafana Live handshake: %s", resp.Status)
		}
	})

	if err := metrics.ImportPrometheus(ctx, []string{"shed_it_marker 42"}); err != nil {
		t.Fatal(err)
	}

	set(false)
	expectStates(observability.Stopped)
	if resp := get("/grafana/", nil); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/server/telemetry" {
		t.Errorf("/grafana/ while off: %s to %q", resp.Status, resp.Header.Get("Location"))
	}

	set(true)
	expectStates(observability.Running)
	deadline := time.Now().Add(time.Minute)
	for {
		res, err := metrics.Query(ctx, `last_over_time(shed_it_marker[1h])`, time.Time{})
		if err == nil && len(res.Series) == 1 && res.Series[0].Values[0].Value == 42 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("marker lost across disable and enable: %+v, %v", res, err)
		}
		time.Sleep(2 * time.Second)
	}
	get("/grafana/api/user", &user)

	if err := plane.DeleteTelemetryData(ctx); err == nil {
		t.Fatal("deleted data while enabled")
	}
	set(false)
	if err := plane.DeleteTelemetryData(ctx); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"metrics", "logs", "traces", "grafana"} {
		if ok, err := dc.VolumeExists(ctx, "shed-telemetry-"+c); err != nil || ok {
			t.Errorf("volume of %s after delete: %v, %v", c, ok, err)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("telemetry directory after delete: %v", err)
	}
	status, err := plane.Telemetry(ctx)
	if err != nil || len(status.Components) != 0 {
		t.Errorf("after delete: %+v, %v", status.Components, err)
	}
}
