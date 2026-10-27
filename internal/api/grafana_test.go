package api

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/kyledickey/shed/internal/observability"
)

// fakeTelemetry is the telemetry stack as far as the API sees it.
type fakeTelemetry struct {
	mu sync.Mutex
	s  observability.Settings
}

func (f *fakeTelemetry) Status(context.Context) (observability.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := observability.Status{Settings: f.s, GrafanaURL: "http://localhost/grafana/", OTLPEndpoint: "http://shed.internal:4318"}
	if f.s.Enabled {
		st.Components = []observability.ComponentStatus{{Name: observability.Grafana, State: observability.Running, Version: "13.2.4", MemoryLimit: 256 << 20}}
	}
	return st, nil
}

func (f *fakeTelemetry) Settings(context.Context) (observability.Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.s, nil
}

func (f *fakeTelemetry) SetSettings(_ context.Context, s observability.Settings) (observability.Settings, error) {
	if err := s.Validate(); err != nil {
		return observability.Settings{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.s = s
	return s, nil
}

func (f *fakeTelemetry) Restart(_ context.Context, c observability.Component) error {
	if !f.s.Enabled {
		return observability.ErrDisabled
	}
	return nil
}

func (f *fakeTelemetry) Dropping(_ context.Context, serviceID string) (bool, error) {
	return serviceID == "dropping", nil
}

func (f *fakeTelemetry) DeleteData(context.Context) error {
	if f.s.Enabled {
		return observability.ErrEnabled
	}
	return nil
}

func (f *fakeTelemetry) enable(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.s.Enabled = on
}

// fakeGrafana records the requests that reach it, answers them with the
// headers it received, and echoes WebSocket frames back.
type fakeGrafana struct {
	addr string
	mu   sync.Mutex
	last *http.Request
}

func newFakeGrafana(t *testing.T) *fakeGrafana {
	g := &fakeGrafana{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.last = r.Clone(context.Background())
		g.mu.Unlock()
		if r.Header.Get("Upgrade") == "websocket" {
			conn, buf, err := http.NewResponseController(w).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			buf.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
			buf.Flush()
			io.Copy(conn, buf)
			return
		}
		w.Header().Set("X-Frame-Options", "deny")
		w.Header().Set("X-Grafana", "yes")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	g.addr = strings.TrimPrefix(srv.URL, "http://")
	return g
}

func (g *fakeGrafana) take() *http.Request {
	g.mu.Lock()
	defer g.mu.Unlock()
	r := g.last
	g.last = nil
	return r
}

func TestTelemetryAPI(t *testing.T) {
	f := newFixture(t)
	got := f.decode(f.do("GET", "/api/telemetry", ""), http.StatusOK)
	settings := got["settings"].(map[string]any)
	if settings["enabled"] != false || settings["metricsRetentionDays"] != float64(30) || len(got["components"].([]any)) != 0 ||
		got["otlpEndpoint"] != "http://shed.internal:4318" {
		t.Errorf("GET /api/telemetry = %v", got)
	}
	f.decode(f.do("POST", "/api/telemetry/components/grafana/restart", ""), http.StatusConflict)

	body := `{"enabled":true,"metricsRetentionDays":30,"logsRetentionDays":7,"tracesRetentionDays":14,"diskLimitGB":20,
		"defaultCaps":{"spansPerSecond":5000,"logsPerSecond":2000,"series":50000}}`
	got = f.decode(f.do("PUT", "/api/telemetry", body), http.StatusOK)
	if got["settings"].(map[string]any)["logsRetentionDays"] != float64(7) || len(got["components"].([]any)) != 1 {
		t.Errorf("PUT /api/telemetry = %v", got)
	}
	f.decode(f.do("DELETE", "/api/telemetry/data", ""), http.StatusConflict)
	f.decode(f.do("POST", "/api/telemetry/components/grafana/restart", ""), http.StatusOK)

	bad := strings.Replace(body, `"diskLimitGB":20`, `"diskLimitGB":0`, 1)
	if e := f.decode(f.do("PUT", "/api/telemetry", bad), http.StatusBadRequest); !strings.Contains(e["error"].(string), "disk limit") {
		t.Errorf("invalid settings: %v", e)
	}
	f.telemetry.enable(false)
	if rec := f.do("DELETE", "/api/telemetry/data", ""); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE /api/telemetry/data while off = %d", rec.Code)
	}
}

func TestGrafanaProxy(t *testing.T) {
	f := newFixture(t)
	f.telemetry.enable(true)

	req := httptest.NewRequest("GET", "/grafana/api/user?x=1", nil)
	req.AddCookie(f.cookie)
	req.AddCookie(&http.Cookie{Name: "grafana_session", Value: "g"})
	req.Header.Set("X-WEBAUTH-USER", "admin")
	req.Header.Set("X-Webauth-Name", "Mallory")
	req.Header.Set("X-WEBAUTH-ROLE", "Admin")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("X-Grafana") != "yes" {
		t.Fatalf("status %d, headers %v", rec.Code, rec.Header())
	}
	if xfo := rec.Header().Values("X-Frame-Options"); len(xfo) != 1 || xfo[0] != "DENY" {
		t.Errorf("X-Frame-Options %v, want shed's alone", xfo)
	}
	up := f.grafana.take()
	if up.URL.Path != "/grafana/api/user" || up.URL.RawQuery != "x=1" || up.Host != req.Host {
		t.Errorf("Grafana got %s %s for host %q", up.Method, up.URL, up.Host)
	}
	if up.Header.Get("X-WEBAUTH-USER") != "octocat" || up.Header.Values("X-Webauth-Name")[0] != "" ||
		len(up.Header.Values("X-Webauth-Name")) != 1 || up.Header.Get("X-Webauth-Role") != "" {
		t.Errorf("Grafana got auth headers %v", up.Header)
	}
	if _, err := up.Cookie("shed_session"); err == nil {
		t.Error("the session cookie reached Grafana")
	}
	if c, err := up.Cookie("grafana_session"); err != nil || c.Value != "g" {
		t.Errorf("Grafana's own cookie: %v, %v", c, err)
	}

	t.Run("signed out", func(t *testing.T) {
		nav := httptest.NewRequest("GET", "/grafana/d/abc", nil)
		nav.Header.Set("Sec-Fetch-Mode", "navigate")
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, nav)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/api/auth/login?next="+url.QueryEscape("/grafana/d/abc") {
			t.Errorf("navigation: %d to %q", rec.Code, rec.Header().Get("Location"))
		}
		api := httptest.NewRequest("GET", "/grafana/api/search", nil)
		api.Header.Set("Accept", "application/json")
		rec = httptest.NewRecorder()
		f.handler.ServeHTTP(rec, api)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("API request: %d, want 401", rec.Code)
		}
		if f.grafana.take() != nil {
			t.Error("a signed-out request reached Grafana")
		}
	})

	t.Run("origin", func(t *testing.T) {
		for _, tt := range []struct {
			origin string
			status int
		}{
			{"http://localhost", http.StatusOK},
			{"https://app.apps.example.com", http.StatusForbidden},
		} {
			req := httptest.NewRequest("POST", "/grafana/api/ds/query", strings.NewReader("{}"))
			req.AddCookie(f.cookie)
			req.Header.Set("Origin", tt.origin)
			rec := httptest.NewRecorder()
			f.handler.ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Errorf("POST from %s: %d, want %d", tt.origin, rec.Code, tt.status)
			}
			if reached := f.grafana.take() != nil; reached != (tt.status == http.StatusOK) {
				t.Errorf("POST from %s reached Grafana: %v", tt.origin, reached)
			}
		}
	})

	t.Run("websocket", func(t *testing.T) {
		srv := httptest.NewServer(f.handler)
		defer srv.Close()
		handshake := func(origin string) (*bufio.Reader, net.Conn, *http.Response) {
			conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			req, _ := http.NewRequest("GET", srv.URL+"/grafana/api/live/ws", nil)
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Origin", origin)
			req.AddCookie(f.cookie)
			if err := req.Write(conn); err != nil {
				t.Fatal(err)
			}
			br := bufio.NewReader(conn)
			resp, err := http.ReadResponse(br, req)
			if err != nil {
				t.Fatal(err)
			}
			return br, conn, resp
		}
		br, conn, resp := handshake("http://localhost")
		defer conn.Close()
		if resp.StatusCode != http.StatusSwitchingProtocols {
			t.Fatalf("handshake: %s", resp.Status)
		}
		if _, err := conn.Write([]byte("ping\n")); err != nil {
			t.Fatal(err)
		}
		line, err := br.ReadString('\n')
		if err != nil || line != "ping\n" {
			t.Errorf("echo %q, %v", line, err)
		}
		if up := f.grafana.take(); up.Header.Get("X-WEBAUTH-USER") != "octocat" {
			t.Errorf("upgrade reached Grafana as %q", up.Header.Get("X-WEBAUTH-USER"))
		}

		_, other, resp := handshake("https://evil.example.com")
		defer other.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("cross-origin handshake: %s", resp.Status)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		f.telemetry.enable(false)
		defer f.telemetry.enable(true)
		req := httptest.NewRequest("GET", "/grafana/", nil)
		req.AddCookie(f.cookie)
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/server/telemetry" {
			t.Errorf("while off: %d to %q", rec.Code, rec.Header().Get("Location"))
		}
		if f.grafana.take() != nil {
			t.Error("a request reached Grafana while telemetry is off")
		}
	})
}
