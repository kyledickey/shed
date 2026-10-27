//go:build integration

// This test needs Docker, network access, and that no shed-telemetry-*
// containers or volumes exist. It runs the real stack through the Manager
// and a real Caddy in front of a test upstream, and checks what the edge
// wrote to VictoriaLogs and VictoriaMetrics. Other integration tests use the
// same containers, so run them one at a time:
//
//	flock /tmp/shed-telemetry-integration.lock go test -tags integration -run EdgeIntegration -v -timeout 20m ./internal/observability/
package observability_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/control"
	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/grafana"
	"github.com/kyledickey/shed/internal/observability"
	"github.com/kyledickey/shed/internal/proxy"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/telemetry"
	"github.com/kyledickey/shed/internal/victoria"
)

func TestEdgeIntegration(t *testing.T) {
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
	// Export shed's metrics every second instead of every minute.
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "1000")

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

	const secret = "edge-it-secret-value"
	project, err := st.CreateProject(ctx, "shop")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := st.CreateService(ctx, store.Service{ProjectID: project.ID, Name: "web", Kind: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetVariables(ctx, svc.ID, map[string]string{"API_KEY": secret}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateDomain(ctx, svc.ID, "edge.localhost", false); err != nil {
		t.Fatal(err)
	}

	addrs := observability.Addrs{Metrics: freeAddr(t), Logs: freeAddr(t), Traces: freeAddr(t), Grafana: freeAddr(t)}
	// shed's own providers, sending straight to VictoriaMetrics' OTLP
	// endpoint where the gateway would forward them.
	providers, err := telemetry.New(ctx, telemetry.Config{Endpoint: "http://" + addrs.Metrics + "/opentelemetry", Version: "test", Host: "test"})
	if err != nil {
		t.Fatal(err)
	}
	providers.Register()
	providers.SetEnabled(true)
	defer providers.Shutdown(context.Background())

	metrics := victoria.NewMetrics("http://"+addrs.Metrics, nil)
	logs := victoria.NewLogs("http://"+addrs.Logs, nil)
	stack := observability.New(observability.Config{
		Store:   st,
		Docker:  dc,
		Metrics: metrics,
		Logs:    logs,
		Traces:  victoria.NewTraces("http://"+addrs.Traces, nil),
		Grafana: func(pw string) observability.GrafanaAPI {
			return grafana.New("http://"+addrs.Grafana+"/grafana", pw, &http.Client{Timeout: 30 * time.Second})
		},
		Addrs:        addrs,
		Dir:          filepath.Join(t.TempDir(), "telemetry"),
		GrafanaURL:   "http://127.0.0.1/grafana/",
		OTLPEndpoint: "http://shed.internal:4318",
		Log:          log,
	})
	plane := control.New(control.Config{Store: st, Telemetry: stack, Log: log})
	edge := observability.NewEdge(observability.EdgeConfig{Store: st, Logs: logs, Mask: plane.ShedMask, Log: log})
	stack.AddWorker(edge.Run)

	var mu sync.Mutex
	var traceparents []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		traceparents = append(traceparents, r.Header.Get("Traceparent"))
		mu.Unlock()
		fmt.Fprint(w, "hello from upstream")
	}))
	defer upstream.Close()
	httpsPort := freePort(t)
	px := proxy.New(proxy.Config{
		HTTPPort: freePort(t), HTTPSPort: httpsPort, StorageDir: t.TempDir(),
		Access: edge.Handle,
		// No gateway listens here; spans are the ingest's to attribute.
		TracesURL: "http://" + freeAddr(t) + "/v1/traces",
	})
	defer px.Stop()
	stack.OnEnabled(func(on bool) {
		if err := px.SetTelemetry(on); err != nil {
			t.Errorf("SetTelemetry(%v): %v", on, err)
		}
	})
	if err := px.Apply([]proxy.Route{{Host: "edge.localhost", Upstream: strings.TrimPrefix(upstream.URL, "http://")}}); err != nil {
		t.Fatal(err)
	}

	set := func(on bool) {
		t.Helper()
		s := observability.DefaultSettings()
		s.Enabled = on
		if _, err := stack.SetSettings(ctx, s); err != nil {
			t.Fatal(err)
		}
		if err := stack.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
	}
	set(true)
	defer set(false)

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, fmt.Sprintf("127.0.0.1:%d", httpsPort))
			},
		},
	}
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	request := func(host string) int {
		t.Helper()
		// Only edge.localhost has a certificate; host is the Host header.
		req, _ := http.NewRequestWithContext(ctx, "GET", "https://edge.localhost/hello?token="+secret+"&page=2", nil)
		req.Host = host
		req.Header.Set("Traceparent", "00-"+traceID+"-00f067aa0ba902b7-01")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	// The worker reads the domain table before it takes entries; the first
	// request may also wait for the internal CA.
	deadline := time.Now().Add(time.Minute)
	for request("edge.localhost") != http.StatusOK {
		if time.Now().After(deadline) {
			t.Fatal("edge.localhost never answered 200")
		}
		time.Sleep(time.Second)
	}
	const n = 5
	for range n {
		if code := request("edge.localhost"); code != http.StatusOK {
			t.Fatalf("GET edge.localhost: %d", code)
		}
	}
	if code := request("nobody.localhost"); code != http.StatusNotFound {
		t.Errorf("GET nobody.localhost: %d, want 404", code)
	}

	mu.Lock()
	if len(traceparents) < n {
		t.Errorf("upstream saw %d requests, want at least %d", len(traceparents), n)
	}
	for _, tp := range traceparents {
		if !strings.HasPrefix(tp, "00-"+traceID+"-") {
			t.Errorf("upstream got traceparent %q, want one continuing trace %s", tp, traceID)
		}
	}
	mu.Unlock()

	poll := func(what string, ok func() (bool, string)) {
		t.Helper()
		deadline := time.Now().Add(90 * time.Second)
		for {
			done, last := ok()
			if done {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: last saw %s", what, last)
			}
			time.Sleep(2 * time.Second)
		}
	}

	poll("access records in VictoriaLogs", func() (bool, string) {
		recs, err := logs.Query(ctx, fmt.Sprintf(`_stream:{shed.source="edge",shed.service.id=%q}`, svc.ID), time.Time{}, time.Time{}, 0)
		if err != nil || len(recs) < n {
			return false, fmt.Sprintf("%d records, %v", len(recs), err)
		}
		for _, r := range recs {
			if r["host"] != "edge.localhost" || r["path"] != "/hello" || r["query"] != "token=***&page=2" ||
				r["status"] != "200" || r["method"] != "GET" || r["trace_id"] != traceID ||
				r["client_ip"] != "127.0.0.1" || r["tls_version"] != "TLS 1.3" ||
				r["shed.service.name"] != "web" || r["shed.project.id"] != project.ID {
				return false, fmt.Sprint(r)
			}
		}
		return true, ""
	})
	poll("unrouted access record in VictoriaLogs", func() (bool, string) {
		recs, err := logs.Query(ctx, `_stream:{shed.source="edge"} host:="nobody.localhost"`, time.Time{}, time.Time{}, 0)
		if err != nil || len(recs) != 1 || recs[0]["status"] != "404" {
			return false, fmt.Sprintf("%v, %v", recs, err)
		}
		return true, ""
	})

	queryAll := func(q string) (*victoria.Result, error) { return metrics.Query(ctx, q, time.Time{}) }
	poll("shed_http_requests_total in VictoriaMetrics", func() (bool, string) {
		res, err := queryAll(fmt.Sprintf(`sum(shed_http_requests_total{shed_service_id=%q,shed_service_name="web",shed_project_name="shop",shed_source="edge",host="edge.localhost",method="GET",code="200"})`, svc.ID))
		if err != nil || len(res.Series) != 1 || res.Series[0].Values[0].Value < n {
			return false, fmt.Sprintf("%+v, %v", res, err)
		}
		return true, ""
	})
	for _, q := range []string{
		fmt.Sprintf(`shed_http_request_duration_seconds_bucket{shed_service_id=%q,le="+Inf"}`, svc.ID),
		fmt.Sprintf(`shed_http_response_bytes_total{shed_service_id=%q}`, svc.ID),
		`shed_http_requests_total{shed_service_id="shed",shed_source="edge",host="unrouted",code="404"}`,
	} {
		poll(q, func() (bool, string) {
			res, err := queryAll(q)
			if err != nil || len(res.Series) == 0 {
				return false, fmt.Sprintf("%+v, %v", res, err)
			}
			t.Logf("%s: %v", q, res.Series[0].Labels)
			return true, ""
		})
	}
}

func freePort(t *testing.T) int {
	_, port, _ := net.SplitHostPort(freeAddr(t))
	var p int
	fmt.Sscan(port, &p)
	return p
}
