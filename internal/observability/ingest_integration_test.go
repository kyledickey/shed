//go:build integration

// This test needs root (the gateway listens inside containers' network
// namespaces), Docker, network access, and that no shed-telemetry-*
// containers or volumes exist. Run it under the shared lock:
//
//	sudo flock /tmp/shed-telemetry-integration.lock go test -tags integration -run IngestIntegration -v -timeout 20m ./internal/observability/
package observability_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/control"
	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/grafana"
	"github.com/kyledickey/shed/internal/netns"
	"github.com/kyledickey/shed/internal/observability"
	"github.com/kyledickey/shed/internal/otlp"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/victoria"
)

const (
	senderTrace = `{"resourceSpans":[{"resource":{"attributes":[{"key":"shed.service.id","value":{"stringValue":"spoofed"}}]},
"scopeSpans":[{"spans":[{"traceId":"5b8efff798038103d269b633813fc60c","spanId":"eee19b7ec3c1b174","name":"GET /checkout",
"kind":2,"startTimeUnixNano":"%[1]d","endTimeUnixNano":"%[2]d",
"attributes":[{"key":"card","value":{"stringValue":"hunter2-integration"}}]}]}]}]}`
	senderLog = `{"resourceLogs":[{"resource":{"attributes":[]},"scopeLogs":[{"logRecords":[{"timeUnixNano":"%[1]d",
"severityText":"INFO","body":{"stringValue":"otlp log from the sender"}}]}]}]}`
	containerLine = `{"msg":"order placed","level":"info","order":42,"token":"hunter2-integration"}`
)

// netnsOpener adapts internal/netns to observability.Namespaces, as
// cmd/shed does.
type netnsOpener struct{}

func (netnsOpener) ID(pid int) (uint64, error) { return netns.ID(pid) }

func (netnsOpener) Listen(ctx context.Context, pid int, addr string) (net.Listener, error) {
	return netns.Listen(ctx, pid, addr)
}

// syncBuffer collects log output written from several goroutines.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

// lines returns the lines that contain every one of parts.
func (b *syncBuffer) lines(parts ...string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, l := range strings.Split(b.b.String(), "\n") {
		ok := true
		for _, p := range parts {
			ok = ok && strings.Contains(l, p)
		}
		if ok {
			out = append(out, l)
		}
	}
	return out
}

func TestIngestIntegration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root: the gateway enters containers' network namespaces; run with sudo")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	for _, c := range []string{"metrics", "logs", "traces", "grafana"} {
		name := "shed-telemetry-" + c
		if out, _ := exec.Command("docker", "ps", "-aq", "--filter", "name=^"+name+"$").Output(); len(out) > 0 {
			t.Fatalf("container %s exists; wait for the other run and retry", name)
		}
		if exec.Command("docker", "volume", "inspect", name).Run() == nil {
			t.Fatalf("volume %s exists; wait for the other run and retry", name)
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

	logs := &syncBuffer{}
	log := slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, logs), &slog.HandlerOptions{Level: slog.LevelInfo}))
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

	project, err := st.CreateProject(ctx, "it-"+store.NewID())
	if err != nil {
		t.Fatal(err)
	}
	svc, err := st.CreateService(ctx, store.Service{ProjectID: project.ID, Name: "web", Kind: "app", Image: "curlimages/curl"})
	if err != nil {
		t.Fatal(err)
	}
	collector, err := st.CreateService(ctx, store.Service{ProjectID: project.ID, Name: "collector", Kind: "app", Image: "alpine"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetVariables(ctx, svc.ID, map[string]string{"TOKEN": "hunter2-integration"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateDomain(ctx, svc.ID, "shop.it.example", false); err != nil {
		t.Fatal(err)
	}
	dep, err := st.CreateDeployment(ctx, store.Deployment{ServiceID: svc.ID, Status: store.StatusDeploying, Trigger: store.TriggerManual})
	if err != nil {
		t.Fatal(err)
	}
	network := "shed-" + project.ID
	if err := dc.EnsureNetwork(ctx, network); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dc.RemoveNetwork(context.Background(), network) })

	addrs := observability.Addrs{Metrics: freeAddr(t), Logs: freeAddr(t), Traces: freeAddr(t), Grafana: freeAddr(t)}
	_, portText, _ := net.SplitHostPort(freeAddr(t))
	port, _ := strconv.Atoi(portText)
	vm := victoria.NewMetrics("http://"+addrs.Metrics, nil)
	vl := victoria.NewLogs("http://"+addrs.Logs, nil)
	vt := victoria.NewTraces("http://"+addrs.Traces, nil)
	stack := observability.New(observability.Config{
		Store: st, Docker: dc, Metrics: vm, Logs: vl, Traces: vt,
		Grafana: func(pw string) observability.GrafanaAPI {
			return grafana.New("http://"+addrs.Grafana+"/grafana", pw, &http.Client{Timeout: 30 * time.Second})
		},
		Addrs:        addrs,
		Dir:          filepath.Join(t.TempDir(), "telemetry"),
		GrafanaURL:   "http://127.0.0.1/grafana/",
		OTLPEndpoint: fmt.Sprintf("http://localhost:%d", port),
		Security: &observability.Security{
			Docker: dc, Catalog: st, Audit: audit.New(st, log), Logs: vl, Conntrack: table{},
		},
		Log: log,
	})
	deployer := deploy.New(deploy.Config{Store: st, Docker: dc, LogDir: t.TempDir(), Log: log})
	defer deployer.Stop()
	auditLog := audit.New(st, log)
	plane := control.New(control.Config{Store: st, Deployer: deployer, Audit: auditLog, Telemetry: stack, BaseURL: "http://127.0.0.1", Log: log})
	gateway := observability.NewGateway(observability.GatewayConfig{
		Store: st, Docker: dc, Namespaces: netnsOpener{}, Masks: plane, Settings: stack.Settings, Port: port, Log: log,
	})
	receiver, err := otlp.New(otlp.Config{
		Attribute: gateway.Attribute, Mask: gateway.Mask, Caps: gateway.Caps,
		Forwarder: observability.NewForwarder(vm, vt, vl), Logger: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	stack.AddWorker(func(ctx context.Context) { gateway.Run(ctx, receiver) })
	stack.OnServiceContainer(gateway.Kick)
	stack.AddWorker(observability.NewLogShipper(observability.LogShipperConfig{
		Store: st, Docker: dc, Logs: vl, Masks: plane, Cap: receiver, Log: log,
	}).Run)
	stack.AddWorker(observability.NewAuditMirror(st, vl, log).Run)

	for _, image := range []string{"curlimages/curl", "alpine"} {
		if err := dc.PullImage(ctx, image, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	run := func(name string, sv store.Service, image, script string) string {
		t.Helper()
		id, err := dc.Run(ctx, docker.RunSpec{
			Name:    name,
			Image:   image,
			Cmd:     []string{"sh", "-c", script},
			Labels:  map[string]string{"shed.project": project.ID, "shed.service": sv.ID, "shed.deployment": dep.ID},
			Network: network,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if t.Failed() {
				out, _ := exec.Command("docker", "logs", "--tail", "40", name).CombinedOutput()
				t.Logf("%s output:\n%s", name, out)
			}
			dc.Remove(context.Background(), id)
		})
		return id
	}

	// An app that listens on the OTLP port itself, from before telemetry is
	// on, so it holds the port before the gateway tries.
	busy := run("shed-it-busy-"+project.ID, collector, "alpine", fmt.Sprintf("exec nc -lk -p %d", port))
	eventually(t, ctx, 30*time.Second, "the busy app listening", func() error {
		return exec.Command("docker", "exec", busy, "sh", "-c", fmt.Sprintf("netstat -ltn | grep -q ':%d '", port)).Run()
	})

	set := func(on bool) {
		t.Helper()
		s := observability.DefaultSettings()
		s.Enabled = on
		if _, err := plane.SetTelemetry(ctx, s); err != nil {
			t.Fatal(err)
		}
		if err := stack.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// Run follows the Docker events, which kick the gateway.
	runCtx, stopRun := context.WithCancel(ctx)
	ran := make(chan struct{})
	go func() {
		defer close(ran)
		stack.Run(runCtx)
	}()
	defer func() {
		stopRun()
		<-ran
	}()
	set(true)
	defer func() {
		set(false)
		if err := stack.DeleteData(context.Background()); err != nil {
			t.Errorf("delete telemetry data: %v", err)
		}
	}()

	// The sender posts to localhost, as an SDK does with the injected
	// endpoint, until the gateway answers, and again after every restart.
	now := time.Now().UnixNano()
	script := fmt.Sprintf(`echo '%s'
post() { curl -sS -m 5 -o /dev/stderr -w '%%{http_code}\n' -H 'Content-Type: application/json' -d "$2" http://localhost:%d/v1/$1; }
until [ "$(post traces '%s')" = 200 ]; do sleep 1; done
until [ "$(post logs '%s')" = 200 ]; do sleep 1; done
echo sent >&2
sleep 600`, containerLine, port,
		fmt.Sprintf(senderTrace, now, now+int64(50*time.Millisecond)), fmt.Sprintf(senderLog, now))
	name := "shed-it-sender-" + project.ID
	id := run(name, svc, "curlimages/curl", script)
	sent := func() int {
		out, _ := exec.Command("docker", "logs", name).CombinedOutput()
		return strings.Count(string(out), "sent\n")
	}
	// reaches reports whether the sender connects to the gateway at
	// localhost; any HTTP answer will do.
	reaches := func() error {
		out, err := exec.Command("docker", "exec", id, "curl", "-sS", "-m", "3", "-o", "/dev/null", "-w", "%{http_code}",
			fmt.Sprintf("http://localhost:%d/v1/traces", port)).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%v: %s", err, out)
		}
		return nil
	}

	expectLog := func(what, query string, check func(victoria.Record) error) {
		t.Helper()
		eventually(t, ctx, 2*time.Minute, what, func() error {
			recs, err := vl.Query(ctx, query, time.Time{}, time.Time{}, 10)
			if err != nil {
				return err
			}
			if len(recs) == 0 {
				return fmt.Errorf("no records for %s", query)
			}
			return check(recs[0])
		})
	}
	// VictoriaTraces stores spans as log records and answers LogsQL too,
	// which shows the resource attributes the Jaeger API leaves out.
	vtRecords := victoria.NewLogs("http://"+addrs.Traces, nil)
	expectTrace := func(what, service, serviceID, name string) {
		t.Helper()
		eventually(t, ctx, 2*time.Minute, what, func() error {
			hits, err := vt.SearchTraces(ctx, victoria.SearchParams{
				Service: service, Start: time.Now().Add(-time.Hour), End: time.Now().Add(time.Minute),
			})
			if err != nil {
				return err
			}
			if len(hits) == 0 {
				return fmt.Errorf("no trace of service %s", service)
			}
			tr, err := vt.Trace(ctx, hits[0].TraceID)
			if err != nil {
				return err
			}
			if len(tr.Spans) == 0 || tr.Spans[0].Name != name {
				return fmt.Errorf("trace %+v", tr)
			}
			if card := tr.Spans[0].Attributes["card"]; card != "***" {
				return fmt.Errorf("span attribute card = %q, want it masked", card)
			}
			recs, err := vtRecords.Query(ctx, fmt.Sprintf(`trace_id:%q`, hits[0].TraceID), time.Time{}, time.Time{}, 10)
			if err != nil {
				return err
			}
			for _, r := range recs {
				if r["resource_attr:service.name"] == service && r["resource_attr:shed.service.id"] == serviceID {
					return nil
				}
			}
			return fmt.Errorf("no span of %s with resource_attr:shed.service.id %s in %v", service, serviceID, recs)
		})
	}

	// Container output and the audit mirror need no traffic from containers.
	expectLog("container log in VictoriaLogs", fmt.Sprintf(`shed.service.id:%q shed.source:container stream:stdout`, svc.ID), func(r victoria.Record) error {
		if r["_msg"] != "order placed" || r["level"] != "info" || r["order"] != "42" || r["token"] != "***" ||
			r["stream"] != "stdout" || r["shed.deployment.id"] != dep.ID || r["shed.project.id"] != project.ID {
			return fmt.Errorf("record %v", r)
		}
		return nil
	})
	expectLog("audit mirror in VictoriaLogs", `shed.source:audit action:"telemetry.enable"`, func(r victoria.Record) error {
		if r["outcome"] != "ok" || r["shed.service.id"] != "shed" {
			return fmt.Errorf("record %v", r)
		}
		return nil
	})
	cursors, err := st.LogCursors(ctx)
	if err != nil || cursors[id].IsZero() {
		t.Errorf("log cursor of the sender = %v, %v; want it set", cursors[id], err)
	}

	// The loopback listener: an edge span is attributed by its host.
	edge := strings.ReplaceAll(fmt.Sprintf(senderTrace, now, now+int64(50*time.Millisecond)),
		`{"key":"shed.service.id","value":{"stringValue":"spoofed"}}`,
		`{"key":"service.name","value":{"stringValue":"caddy"}},{"key":"webengine.name","value":{"stringValue":"Caddy"}}`)
	edge = strings.Replace(edge, `"attributes":[{"key":"card"`, `"attributes":[{"key":"server.address","value":{"stringValue":"shop.it.example"}},{"key":"card"`, 1)
	resp, err := http.Post("http://127.0.0.1:"+portText+"/v1/traces", "application/json", strings.NewReader(edge))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("loopback edge span: %s", resp.Status)
	}
	expectTrace("edge span in VictoriaTraces", "caddy", svc.ID, "GET /checkout")

	// The listener inside the container: its own OTLP, sent to localhost.
	eventually(t, ctx, 30*time.Second, "OTLP from the container", func() error {
		if sent() >= 1 {
			return nil
		}
		return errors.New("not sent yet")
	})
	expectTrace("container trace in VictoriaTraces", "web", svc.ID, "GET /checkout")
	expectLog("OTLP log in VictoriaLogs", fmt.Sprintf(`shed.service.id:%q shed.source:otlp`, svc.ID), func(r victoria.Record) error {
		if r["_msg"] != "otlp log from the sender" || r["shed.project.id"] != project.ID {
			return fmt.Errorf("record %v", r)
		}
		return nil
	})

	// A restart gives the container a new namespace, where the listener is
	// opened again and answers.
	if out, err := exec.Command("docker", "restart", "-t", "1", id).CombinedOutput(); err != nil {
		t.Fatalf("docker restart: %v: %s", err, out)
	}
	eventually(t, ctx, 30*time.Second, "OTLP from the restarted container", func() error {
		if n := sent(); n < 2 {
			return fmt.Errorf("sent %d times", n)
		}
		return nil
	})

	// The app that holds the port gets no listener: the conflict is
	// reported on its service, logged once, and nothing else is affected.
	eventually(t, ctx, 30*time.Second, "the port conflict reported", func() error {
		if got := gateway.ListenerError(collector.ID); !strings.Contains(got, fmt.Sprintf("listens on port %d", port)) {
			return fmt.Errorf("listenerError = %q", got)
		}
		return nil
	})
	time.Sleep(12 * time.Second) // two more passes, each trying again
	if n := len(logs.lines("listen in container", busy)); n != 1 {
		t.Errorf("logged the conflict %d times, want once", n)
	}
	if out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", busy).Output(); err != nil || strings.TrimSpace(string(out)) != "true" {
		t.Errorf("busy app running = %q, %v", out, err)
	}
	if got := gateway.ListenerError(svc.ID); got != "" {
		t.Errorf("sender listenerError = %q, want none", got)
	}

	// Stopping the container closes its listener; starting it opens one.
	if out, err := exec.Command("docker", "stop", "-t", "1", id).CombinedOutput(); err != nil {
		t.Fatalf("docker stop: %v: %s", err, out)
	}
	eventually(t, ctx, 30*time.Second, "listener closed after stop", func() error {
		if len(logs.lines("telemetry gateway: closed listener", id)) == 0 {
			return errors.New("not closed")
		}
		return nil
	})
	if out, err := exec.Command("docker", "start", id).CombinedOutput(); err != nil {
		t.Fatalf("docker start: %v: %s", err, out)
	}
	eventually(t, ctx, 30*time.Second, "the gateway at localhost after a start", reaches)

	// The host firewall was not touched.
	if out, err := exec.Command("iptables", "-S").CombinedOutput(); err != nil {
		t.Logf("iptables -S: %v", err)
	} else if strings.Contains(string(out), "shed") {
		t.Errorf("iptables has shed rules:\n%s", out)
	}

	// Turning telemetry off closes every listener.
	set(false)
	if err := reaches(); err == nil {
		t.Error("the gateway still answers at localhost with telemetry off")
	}
	if got := gateway.ListenerError(collector.ID); got != "" {
		t.Errorf("listenerError with telemetry off = %q", got)
	}
}

// eventually calls check until it succeeds or timeout passes.
func eventually(t *testing.T, ctx context.Context, timeout time.Duration, what string, check func() error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		err := check()
		if err == nil {
			return
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			t.Fatalf("%s: %v", what, err)
		}
		time.Sleep(time.Second)
	}
}
