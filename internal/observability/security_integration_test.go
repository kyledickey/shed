//go:build integration

// This test needs root (conntrack is read over netlink), Docker, and network
// access, and that no shed-telemetry-* containers or volumes exist. It runs
// the real Manager and stack, starts a container labeled as a shed service
// that connects to 1.1.1.1:443, and checks what shed records about it:
//
//	sudo go test -tags integration -run SecurityIntegration -v -timeout 20m ./internal/observability/
package observability_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/grafana"
	"github.com/kyledickey/shed/internal/host"
	"github.com/kyledickey/shed/internal/observability"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/victoria"
)

type table struct{}

func (table) Flows(ctx context.Context) ([]host.Flow, error) { return host.ConntrackFlows(ctx) }
func (table) EnableAccounting() error                        { return host.EnableConntrackAccounting("") }

func TestSecurityIntegration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root: shed reads the conntrack table over netlink (CAP_NET_ADMIN); run with sudo")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skipf("needs a Docker daemon: %v", err)
	}
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

	prj, err := st.CreateProject(ctx, "egress-it")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := st.CreateService(ctx, store.Service{ProjectID: prj.ID, Name: "prober", Kind: "app", Image: "alpine:3"})
	if err != nil {
		t.Fatal(err)
	}

	addrs := observability.Addrs{Metrics: freeAddr(t), Logs: freeAddr(t), Traces: freeAddr(t), Grafana: freeAddr(t)}
	logs := victoria.NewLogs("http://"+addrs.Logs, nil)
	stack := observability.New(observability.Config{
		Store:   st,
		Docker:  dc,
		Metrics: victoria.NewMetrics("http://"+addrs.Metrics, nil),
		Logs:    logs,
		Traces:  victoria.NewTraces("http://"+addrs.Traces, nil),
		Grafana: func(pw string) observability.GrafanaAPI {
			return grafana.New("http://"+addrs.Grafana+"/grafana", pw, &http.Client{Timeout: 30 * time.Second})
		},
		Addrs:        addrs,
		Dir:          filepath.Join(t.TempDir(), "telemetry"),
		GrafanaURL:   "http://" + addrs.Grafana + "/grafana/",
		OTLPEndpoint: "http://shed.internal:4318",
		Security: &observability.Security{
			Docker: dc, Catalog: st, Audit: audit.New(st, log), Logs: logs, Conntrack: table{},
		},
		Log: log,
	})
	s := observability.DefaultSettings()
	s.Enabled = true
	if _, err := stack.SetSettings(ctx, s); err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { stack.Run(runCtx); close(done) }()
	defer func() { stop(); <-done }()

	// The service container is started once the stack is up, so that the
	// events follower and the egress worker are running.
	waitFor(t, 10*time.Minute, "the stack to run", func() error {
		status, err := stack.Status(ctx)
		if err != nil {
			return err
		}
		for _, c := range status.Components {
			if c.State != observability.Running {
				return fmt.Errorf("%s is %s", c.Name, c.State)
			}
		}
		return nil
	})

	name := "shed-it-prober-" + svc.ID
	if err := dc.PullImage(ctx, "alpine:3", io.Discard); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", name).Run() })
	id, err := dc.Run(ctx, docker.RunSpec{
		Name:  name,
		Image: "alpine:3",
		Cmd:   []string{"sh", "-c", "while true; do nc -w 2 1.1.1.1 443 </dev/null; sleep 1; done"},
		Labels: map[string]string{
			"shed.service": svc.ID, "shed.project": prj.ID, "shed.deployment": "dep-it",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	query := func(extra string) ([]victoria.Record, error) {
		q := fmt.Sprintf(`{shed.source="egress", shed.service.id=%q} dst:="1.1.1.1" dst_port:="443" %s`, svc.ID, extra)
		return logs.Query(ctx, q, time.Now().Add(-time.Hour), time.Time{}, 20)
	}
	var first, traffic []victoria.Record
	waitFor(t, 3*time.Minute, "an egress.first_seen record", func() (err error) {
		if first, err = query(`event:="egress.first_seen"`); err == nil && len(first) == 0 {
			err = fmt.Errorf("none yet")
		}
		return err
	})
	waitFor(t, 3*time.Minute, "an egress traffic record", func() (err error) {
		if traffic, err = query(`-event:*`); err == nil && len(traffic) == 0 {
			err = fmt.Errorf("none yet")
		}
		return err
	})
	if len(first) != 1 {
		t.Errorf("%d first_seen records for one destination, want 1", len(first))
	}
	for _, r := range append(first, traffic...) {
		if r["shed.service.name"] != "prober" || r["shed.project.id"] != prj.ID || r["proto"] != "tcp" {
			t.Errorf("record labels: %v", r)
		}
	}
	t.Logf("first_seen record: %v", first[0])
	t.Logf("traffic record: %v", traffic[0])
	if first[0]["level"] != "warn" {
		t.Errorf("first_seen level = %q, want warn", first[0]["level"])
	}

	// An exec shed starts is its own; one a person starts is audited.
	if err := dc.Exec(ctx, id, []string{"echo", "from-shed"}, nil, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("docker", "exec", name, "echo", "from-a-person").CombinedOutput(); err != nil {
		t.Fatalf("docker exec: %v\n%s", err, out)
	}
	var execs []store.AuditEvent
	waitFor(t, time.Minute, "a container.exec audit row", func() (err error) {
		execs, err = st.AuditEvents(ctx, store.AuditFilter{Action: "container.exec", ServiceID: svc.ID})
		if err == nil && len(execs) == 0 {
			err = fmt.Errorf("none yet")
		}
		return err
	})
	time.Sleep(3 * time.Second) // time for a wrong row for shed's exec to appear
	execs, err = st.AuditEvents(ctx, store.AuditFilter{Action: "container.exec", ServiceID: svc.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(execs) != 1 {
		t.Fatalf("container.exec rows = %+v, want one", execs)
	}
	t.Logf("audit row: %+v", execs[0])
	var detail map[string]string
	if err := json.Unmarshal([]byte(execs[0].Detail), &detail); err != nil {
		t.Fatal(err)
	}
	if execs[0].ActorKind != "docker" || execs[0].ProjectID != prj.ID || execs[0].Target != id ||
		!strings.Contains(detail["command"], "from-a-person") || detail["container"] != name {
		t.Errorf("row = %+v detail %v", execs[0], detail)
	}
}

func waitFor(t *testing.T, d time.Duration, what string, check func() error) {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		err := check()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiting for %s: %v", what, err)
		}
		time.Sleep(2 * time.Second)
	}
}
