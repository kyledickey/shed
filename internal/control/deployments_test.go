package control

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/store"
)

func TestDeploymentsImageAvailability(t *testing.T) {
	for _, tc := range []struct {
		name      string
		available map[string]bool
		err       error
		want      *bool
	}{
		{name: "available", available: map[string]bool{"sha256:present": true}, want: new(true)},
		{name: "missing", available: map[string]bool{}, want: new(false)},
		{name: "docker unavailable", err: errors.New("docker unreachable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			svc := createApp(t, f.st)
			for _, image := range []string{"", "sha256:present"} {
				if _, err := f.st.CreateDeployment(ctx, store.Deployment{
					ServiceID: svc.ID, Status: store.StatusRemoved, Trigger: store.TriggerManual, Image: image,
				}); err != nil {
					t.Fatal(err)
				}
			}
			f.deployer.available, f.deployer.imagesErr = tc.available, tc.err
			page, err := f.plane.Deployments(ctx, svc.ID, Page{})
			if err != nil {
				t.Fatal(err)
			}
			deps := page.Deployments
			if len(deps) != 2 {
				t.Fatalf("deployments = %d, want 2", len(deps))
			}
			for _, d := range deps {
				got := d.ImageAvailable
				switch {
				case d.Image == "" && got != nil:
					t.Errorf("deployment without image: available = %v, want unset", *got)
				case d.Image != "" && (got == nil) != (tc.want == nil):
					t.Errorf("available = %v, want %v", got, tc.want)
				case d.Image != "" && got != nil && *got != *tc.want:
					t.Errorf("available = %v, want %v", *got, *tc.want)
				}
			}
		})
	}
}

func TestDeploymentsPage(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)
	var ids []string // Newest first.
	for range MaxPage + 5 {
		d, err := f.st.CreateDeployment(ctx, store.Deployment{
			ServiceID: svc.ID, Status: store.StatusRemoved, Trigger: store.TriggerManual,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append([]string{d.ID}, ids...)
	}
	for _, tt := range []struct {
		name     string
		page     Page
		first    string
		n        int
		wantNext string
	}{
		{"default", Page{}, ids[0], DefaultPage, ids[DefaultPage-1]},
		{"negative", Page{Limit: -1}, ids[0], DefaultPage, ids[DefaultPage-1]},
		{"limit", Page{Limit: 3}, ids[0], 3, ids[2]},
		{"capped", Page{Limit: 1000}, ids[0], MaxPage, ids[MaxPage-1]},
		{"after a cursor", Page{Before: ids[2], Limit: 3}, ids[3], 3, ids[5]},
		{"exactly the rest", Page{Before: ids[len(ids)-4], Limit: 3}, ids[len(ids)-3], 3, ""},
		{"short last page", Page{Before: ids[len(ids)-3], Limit: 5}, ids[len(ids)-2], 2, ""},
		{"unknown cursor", Page{Before: "nope"}, "", 0, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.plane.Deployments(ctx, svc.ID, tt.page)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Deployments) != tt.n || got.Next != tt.wantNext {
				t.Fatalf("Deployments(%+v) = %d, next %q; want %d, next %q", tt.page, len(got.Deployments), got.Next, tt.n, tt.wantNext)
			}
			if tt.n > 0 && got.Deployments[0].ID != tt.first {
				t.Errorf("first = %s, want %s", got.Deployments[0].ID, tt.first)
			}
		})
	}
	if _, err := f.plane.Deployments(ctx, "nope", Page{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Deployments(nope) = %v, want not found", err)
	}
}

func TestDeploymentViewHidesRuntime(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)
	dep, err := f.st.CreateDeployment(ctx, store.Deployment{
		ServiceID: svc.ID, Status: store.StatusActive, Trigger: store.TriggerManual, CommitSHA: "abc",
		Runtime: &store.DeploymentRuntime{Env: map[string]string{"TOKEN": "secret-value"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.plane.Deployment(ctx, dep.ID)
	if err != nil || got.ID != dep.ID || got.CommitSHA != "abc" {
		t.Fatalf("Deployment() = %+v, %v", got, err)
	}
	view, err := f.plane.Service(ctx, svc.ID)
	if err != nil || view.LatestDeployment == nil || view.LatestDeployment.ID != dep.ID {
		t.Fatalf("Service().LatestDeployment = %+v, %v", view.LatestDeployment, err)
	}
	for _, v := range []any{got, *view.LatestDeployment} {
		if s := fmt.Sprintf("%+v", v); strings.Contains(s, "secret-value") {
			t.Errorf("view = %s, want no runtime environment", s)
		}
	}
}

func TestDeployResolvesBranchHead(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)
	_, err := f.plane.Deploy(ctx, svc.ID)
	wantKind(t, err, ErrConflict, "GitHub App is not configured")

	f.github = true
	dep, err := f.plane.Deploy(ctx, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dep.Trigger != store.TriggerManual || dep.CommitSHA != "head" {
		t.Errorf("deployment = %+v, want a manual deployment of head", dep)
	}
}

func TestLogLinesClamped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, n := range []int{-5, 0, 10, MaxLogLines + 1} {
		if _, err := f.plane.BuildLog(ctx, "dep", n); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := f.deployer.logLines, []int{1, 1, 10, MaxLogLines}; !slices.Equal(got, want) {
		t.Errorf("lines asked = %v, want %v", got, want)
	}
}

func TestRuntimeLogsSnapshot(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)

	_, err := f.plane.RuntimeLogs(ctx, svc.ID, 10)
	if !errors.Is(err, deploy.ErrNoContainer) {
		t.Errorf("without a container: error = %v, want ErrNoContainer", err)
	}
	if _, err := f.plane.RuntimeLogs(ctx, "nope", 10); !errors.Is(err, ErrNotFound) {
		t.Errorf("RuntimeLogs(nope) = %v, want not found", err)
	}

	f.deployer.runtimeLog = "2026-10-05T10:00:00Z one\r\n2026-10-05T10:00:01Z two\nlast"
	lines, err := f.plane.RuntimeLogs(ctx, svc.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-10-05T10:00:00Z one", "2026-10-05T10:00:01Z two", "last"}
	if !slices.Equal(lines, want) {
		t.Errorf("lines = %q, want %q", lines, want)
	}
	if !slices.Equal(f.deployer.follow, []bool{false, false}) {
		t.Errorf("follow = %v, want snapshots only", f.deployer.follow)
	}
}

func TestLineBufferBounds(t *testing.T) {
	var b lineBuffer
	long := strings.Repeat("x", maxLineBytes+5)
	b.Write([]byte("a\nb"))
	b.Write([]byte("c\n" + long + "\n"))
	got := b.done()
	want := []string{"a", "bc", strings.Repeat("x", maxLineBytes), "xxxxx"}
	if !slices.Equal(got, want) {
		t.Errorf("lines = %.40q, want %.40q", got, want)
	}
	var empty lineBuffer
	if got := empty.done(); got == nil || len(got) != 0 {
		t.Errorf("empty = %#v, want an empty slice", got)
	}
}

func TestShedLogMasksVariables(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)
	if err := f.st.SetVariables(ctx, svc.ID, map[string]string{"API_KEY": "sk-live-123456", "DEBUG": "1"}); err != nil {
		t.Fatal(err)
	}
	f.logs = fakeLogs{"msg=one", "msg=two key=sk-live-123456 debug=1", "msg=three"}

	got, err := f.plane.ShedLog(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"msg=two key=*** debug=1", "msg=three"}; !slices.Equal(got, want) {
		t.Errorf("ShedLog(2) = %q, want %q", got, want)
	}
}

func TestVariableNames(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)
	saved := map[string]string{
		"API_KEY": "secret-value", "DATABASE_URL": "${{ db.DATABASE_URL }}", "PORT": "3000",
	}
	f.deployer.resolved.Saved = saved
	f.deployer.resolved.Env = map[string]string{
		"API_KEY": "secret-value", "DATABASE_URL": "postgres://u:p@db/x", "PORT": "3000",
		"SHED_SERVICE_NAME": "web", "SHED_PRIVATE_DOMAIN": "web",
	}
	got, err := f.plane.VariableNames(ctx, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := VariableNames{Variables: []VariableName{
		{Name: "API_KEY"},
		{Name: "DATABASE_URL", Reference: true},
		{Name: "PORT"},
		{Name: "SHED_PRIVATE_DOMAIN", Injected: true},
		{Name: "SHED_SERVICE_NAME", Injected: true},
	}}
	if !slices.Equal(got.Variables, want.Variables) || got.ResolveError != "" {
		t.Errorf("VariableNames() = %+v, want %+v", got, want)
	}

	f.deployer.resolved, f.deployer.resolveErr = deploy.Resolved{Saved: saved}, errors.New("vars: reference cycle: web.A -> web.A")
	got, err = f.plane.VariableNames(ctx, svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Variables) != 3 || !strings.Contains(got.ResolveError, "reference cycle") {
		t.Errorf("unresolvable: %+v, want own names and the error", got)
	}
	f.deployer.resolved, f.deployer.resolveErr = deploy.Resolved{}, store.ErrNotFound
	if _, err := f.plane.VariableNames(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("VariableNames(nope) = %v, want not found", err)
	}
}

func TestSetVariablesRejectsNames(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)
	for _, name := range []string{"", "1A", "A-B", "A B"} {
		_, err := f.plane.SetVariables(ctx, svc.ID, map[string]string{name: "x"})
		wantKind(t, err, ErrInvalid, "invalid variable name")
	}
	got, err := f.plane.SetVariables(ctx, svc.ID, map[string]string{"_OK": "1", "A1": "2"})
	if err != nil || len(got) != 2 {
		t.Errorf("SetVariables() = %v, %v", got, err)
	}
}

func TestBranches(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, err := f.plane.Branches(ctx, "octo", "app")
	wantKind(t, err, ErrConflict, "GitHub App is not configured")

	f.github = true
	if got, err := f.plane.Branches(ctx, "octo", "app"); err != nil || !slices.Equal(got, []string{"main", "dev"}) {
		t.Errorf("Branches() = %v, %v", got, err)
	}
	_, err = f.plane.Branches(ctx, "octo", "app/../x")
	wantKind(t, err, ErrInvalid, "repository must be owner/name")
	_, err = f.plane.Branches(ctx, "octo", "missing")
	wantKind(t, err, ErrUpstream, "404")
}

func TestMetricsRange(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)
	if m, err := f.plane.ServiceMetrics(ctx, svc.ID, ""); err != nil || m.Range != "1h" {
		t.Errorf("ServiceMetrics(\"\") = %v, %v; want 1h", m.Range, err)
	}
	_, err := f.plane.ServiceMetrics(ctx, svc.ID, "2h")
	wantKind(t, err, ErrInvalid, "range must be one of")
	_, err = f.plane.HostMetrics(ctx, "2h")
	wantKind(t, err, ErrInvalid, "range must be one of")
	if _, err := f.plane.ServiceMetrics(ctx, "nope", "7d"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ServiceMetrics(nope) = %v, want not found", err)
	}
}
