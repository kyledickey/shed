package deploy

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/store"
)

// buildLog returns the whole build log of a finished deployment.
func (f *fixture) buildLog(t *testing.T, id string) string {
	t.Helper()
	var lines []string
	err := f.d.FollowLog(context.Background(), id,
		func(l string) { lines = append(lines, l) }, func(store.DeploymentStatus) {})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}

func assertLog(t *testing.T, log string, want, unwanted []string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(log, w) {
			t.Errorf("log lacks %q:\n%s", w, log)
		}
	}
	for _, u := range unwanted {
		if strings.Contains(log, u) {
			t.Errorf("log contains %q:\n%s", u, log)
		}
	}
}

func TestLineWriter(t *testing.T) {
	tests := []struct {
		name   string
		writes []string
		want   string
	}{
		{"strips timestamps", []string{"2026-10-04T10:00:00.123456789Z hello\n"}, "hello\n"},
		{"keeps other text", []string{"not-a-time hello\n"}, "not-a-time hello\n"},
		{"joins split lines", []string{"2026-10-04T10:00:00Z hel", "lo\n2026-10-04T10:00:01Z bye\n"}, "hello\nbye\n"},
		{"flushes a partial line", []string{"2026-10-04T10:00:00Z tail"}, "tail\n"},
		{"drops carriage returns", []string{"2026-10-04T10:00:00Z a\r\n"}, "a\n"},
		{"escapes step headings", []string{"2026-10-04T10:00:00Z ==> fake\n"}, " ==> fake\n"},
		{"keeps empty lines", []string{"2026-10-04T10:00:00Z \n"}, "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			lw := &lineWriter{out: &out}
			for _, w := range tt.writes {
				lw.Write([]byte(w))
			}
			lw.Flush()
			if out.String() != tt.want {
				t.Errorf("got %q, want %q", out.String(), tt.want)
			}
		})
	}
}

func TestDeployLogDetails(t *testing.T) {
	f := newFixture(t)
	f.docker.output = "2026-10-04T10:00:00.000000001Z listening on :8080\n"
	first := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if first.Status != store.StatusActive {
		t.Fatalf("status = %s (%q), want active", first.Status, first.Error)
	}
	assertLog(t, f.buildLog(t, first.ID), []string{
		"Name: shed-" + f.svc.ID + "-" + first.ID,
		"Image: shed/" + f.svc.ID,
		"Network: shed-" + f.svc.ProjectID + ", private address web:8080",
		"variables",
		"Started container c1",
		"Probing TCP connect to 10.0.0.1:8080",
		"\nlistening on :8080\n",
		"Healthy after",
		"Routing web.example.com → port 8080",
	}, []string{
		"hi from web", // a variable's value
		"2026-10-04T10:00:00",
		"Removing previous deployment",
	})

	second := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	assertLog(t, f.buildLog(t, second.ID), []string{
		"Removing previous deployment " + first.ID + " (container c1)",
	}, nil)
}

func TestDeployLogStopsPreviousWithVolumes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	v, err := f.st.CreateVolume(ctx, f.svc.ID, "/data")
	if err != nil {
		t.Fatal(err)
	}
	f.svc.PublicPort = 15432
	f.svc.StartCommand = "./serve"
	if err := f.st.UpdateService(ctx, f.svc); err != nil {
		t.Fatal(err)
	}
	if err := f.st.DeleteDomain(ctx, mustDomainID(t, f)); err != nil {
		t.Fatal(err)
	}
	first := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	second := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if second.Status != store.StatusActive {
		t.Fatalf("status = %s (%q), want active", second.Status, second.Error)
	}
	assertLog(t, f.buildLog(t, second.ID), []string{
		"==> Stopping previous deployment " + first.ID,
		"Stopped container shed-" + f.svc.ID + "-" + first.ID,
		"Volume: shed-vol-" + v.ID + " → /data",
		"Publishing host port 15432 → container port 8080",
		"Start command: ./serve",
		"No public domains",
	}, nil)
}

func mustDomainID(t *testing.T, f *fixture) string {
	t.Helper()
	ds, err := f.st.Domains(context.Background(), f.svc.ID)
	if err != nil || len(ds) == 0 {
		t.Fatalf("Domains() = %v, %v", ds, err)
	}
	return ds[0].ID
}

func TestDeployLogContainerExits(t *testing.T) {
	f := newFixture(t)
	f.docker.output = "2026-10-04T10:00:00Z panic: no database\n"
	f.docker.exitCode = 3
	dep := f.wait(t, f.deploy(t).ID, terminal)
	if dep.Status != store.StatusFailed || dep.Error != "container exited with code 3" {
		t.Fatalf("deployment = %s (%q), want failed with exit code", dep.Status, dep.Error)
	}
	log := f.buildLog(t, dep.ID)
	assertLog(t, log, []string{"panic: no database", "==> Deployment failed: container exited with code 3"}, nil)
	if strings.Index(log, "panic: no database") > strings.Index(log, "==> Deployment failed") {
		t.Errorf("container output comes after the failure:\n%s", log)
	}
}

func TestDeployLogReportsPendingHealth(t *testing.T) {
	f := newFixture(t)
	f.d.healthReport = 30 * time.Millisecond
	f.healthy = func() bool { return false }
	dep := f.wait(t, f.deploy(t).ID, terminal)
	if dep.Status != store.StatusFailed {
		t.Fatalf("status = %s, want failed", dep.Status)
	}
	log := f.buildLog(t, dep.ID)
	assertLog(t, log, []string{"Not ready yet: connection refused"}, []string{"Healthy after"})
	if n := strings.Count(log, "Not ready yet"); n > 10 {
		t.Errorf("%d progress lines in a 200ms health check; want them throttled", n)
	}
}

func TestDeployWithoutPort(t *testing.T) {
	tests := []struct {
		name       string
		exitCode   int
		wantStatus store.DeploymentStatus
		wantLog    []string
	}{
		{"keeps running", 0, store.StatusActive, []string{
			"Still running after", "private host web", "No port, so its domains are not routed",
		}},
		{"exits", 1, store.StatusFailed, []string{
			"==> Deployment failed: container exited with code 1",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.svc.Port = 0
			if err := f.st.UpdateService(context.Background(), f.svc); err != nil {
				t.Fatal(err)
			}
			f.docker.output = "2026-10-04T10:00:00Z worker ready\n"
			f.docker.exitCode = tt.exitCode
			dep := f.wait(t, f.deploy(t).ID, terminal)
			f.settle(t)
			if dep.Status != tt.wantStatus {
				t.Fatalf("status = %s (%q), want %s", dep.Status, dep.Error, tt.wantStatus)
			}
			want := append([]string{"==> Watching the container start", "worker ready"}, tt.wantLog...)
			assertLog(t, f.buildLog(t, dep.ID), want, nil)
		})
	}
}

func TestFollowerStopIsIdempotent(t *testing.T) {
	f := newFixture(t)
	var out strings.Builder
	j := &job{Deployer: f.d, out: &syncWriter{w: &out}}
	f.docker.output = "line\n"
	fl := j.follow(context.Background(), "missing")
	fl.drain(time.Second)
	fl.stop()
	fl.stop()
	if out.String() != "line\n" {
		t.Errorf("output = %q, want %q", out.String(), "line\n")
	}
}
