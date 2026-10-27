package control

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/observability"
)

// fakeTelemetry keeps settings and enforces the manager's rules about when
// data can be deleted and components restarted.
type fakeTelemetry struct {
	s observability.Settings
}

func (f *fakeTelemetry) Status(context.Context) (observability.Status, error) {
	return observability.Status{Settings: f.s}, nil
}

func (f *fakeTelemetry) Settings(context.Context) (observability.Settings, error) { return f.s, nil }

func (f *fakeTelemetry) SetSettings(_ context.Context, s observability.Settings) (observability.Settings, error) {
	if err := s.Validate(); err != nil {
		return observability.Settings{}, err
	}
	f.s = s
	return s, nil
}

func (f *fakeTelemetry) Restart(_ context.Context, c observability.Component) error {
	if c != observability.Grafana {
		return observability.ErrUnknownComponent
	}
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

// fakeDashboards counts refreshes and knows one dashboard.
type fakeDashboards struct {
	refreshes atomic.Int32
}

func (d *fakeDashboards) Refresh() { d.refreshes.Add(1) }

func (d *fakeDashboards) List(context.Context, string) ([]observability.Dashboard, error) {
	return nil, nil
}

func (d *fakeDashboards) Reset(_ context.Context, uid string) error {
	if uid != "svc-logs-s1" {
		return observability.ErrNotDashboard
	}
	return nil
}

func (d *fakeDashboards) Restore(context.Context) error { return nil }

func (d *fakeDashboards) Service(context.Context, string) (observability.ServiceDashboards, error) {
	return observability.ServiceDashboards{FolderURL: "https://shed.example.com/grafana/dashboards/f/s-s1/"}, nil
}

func TestDashboardsAudit(t *testing.T) {
	f := newFixture(t)
	ctx := audit.WithActor(context.Background(), audit.Actor{Kind: audit.User, Name: "octocat"})
	if err := f.plane.ResetDashboard(ctx, "svc-logs-s1"); err != nil {
		t.Fatal(err)
	}
	if err := f.plane.ResetDashboard(ctx, "elsewhere"); !errors.Is(err, observability.ErrNotDashboard) {
		t.Fatalf("reset of a foreign dashboard = %v, want ErrNotDashboard", err)
	} else if e, ok := Explain(err); !ok || !errors.Is(e, ErrNotFound) {
		t.Errorf("Explain(%v) = %v, want ErrNotFound", err, e)
	}
	if err := f.plane.RestoreDashboards(ctx); err != nil {
		t.Fatal(err)
	}
	type event struct {
		action, target string
		outcome        audit.Outcome
	}
	var got []event
	for _, e := range f.audit.take() {
		got = append(got, event{e.Action, e.Target, e.Outcome})
	}
	want := []event{
		{"telemetry.dashboard.reset", "svc-logs-s1", audit.OK},
		{"telemetry.dashboard.reset", "elsewhere", audit.Failed},
		{"telemetry.dashboards.restore", "", audit.OK},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("recorded %+v, want %+v", got, want)
	}
}

func TestDashboardsFollowProjectsAndServices(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	pr, err := f.plane.CreateProject(ctx, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.plane.RenameProject(ctx, pr.ID, "shop"); err != nil {
		t.Fatal(err)
	}
	svc, err := f.plane.CreateService(ctx, pr.ID, NewService{Name: "web", Kind: "app", Image: "nginx"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.plane.DeleteService(ctx, svc.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.plane.DeleteProject(ctx, pr.ID); err != nil {
		t.Fatal(err)
	}
	if n := f.dash.refreshes.Load(); n != 5 {
		t.Errorf("dashboards refreshed %d times, want once per create, rename, and delete (5)", n)
	}
}

func TestTelemetryAudit(t *testing.T) {
	f := newFixture(t)
	ctx := audit.WithActor(context.Background(), audit.Actor{Kind: audit.User, Name: "octocat"})
	on := observability.DefaultSettings()
	on.Enabled = true
	longer := on
	longer.LogsRetentionDays, longer.DiskLimitGB = 30, 40
	invalid := longer
	invalid.Enabled, invalid.DiskLimitGB = false, 0
	off := longer
	off.Enabled = false

	type event struct {
		action  string
		outcome audit.Outcome
		target  string
		fields  []string
	}
	steps := []struct {
		name string
		run  func() error
		kind error
		want []event
	}{
		{"enable", func() error { _, err := f.plane.SetTelemetry(ctx, on); return err }, nil,
			[]event{{action: "telemetry.enable", outcome: audit.OK}}},
		{"save unchanged", func() error { _, err := f.plane.SetTelemetry(ctx, on); return err }, nil,
			[]event{{action: "telemetry.settings.update", outcome: audit.OK, fields: []string{}}}},
		{"change retention and disk", func() error { _, err := f.plane.SetTelemetry(ctx, longer); return err }, nil,
			[]event{{action: "telemetry.settings.update", outcome: audit.OK, fields: []string{"logsRetentionDays", "diskLimitGB"}}}},
		{"delete while enabled", func() error { return f.plane.DeleteTelemetryData(ctx) }, ErrConflict,
			[]event{{action: "telemetry.data.delete", outcome: audit.Failed}}},
		{"restart", func() error { _, err := f.plane.RestartTelemetryComponent(ctx, "grafana"); return err }, nil,
			[]event{{action: "telemetry.component.restart", outcome: audit.OK, target: "grafana"}}},
		{"restart unknown", func() error { _, err := f.plane.RestartTelemetryComponent(ctx, "postgres"); return err }, ErrNotFound,
			[]event{{action: "telemetry.component.restart", outcome: audit.Failed, target: "postgres"}}},
		{"disable with an invalid disk limit", func() error { _, err := f.plane.SetTelemetry(ctx, invalid); return err }, ErrInvalid,
			[]event{
				{action: "telemetry.disable", outcome: audit.Failed},
				{action: "telemetry.settings.update", outcome: audit.Failed, fields: []string{"diskLimitGB"}},
			}},
		{"disable", func() error { _, err := f.plane.SetTelemetry(ctx, off); return err }, nil,
			[]event{{action: "telemetry.disable", outcome: audit.OK}}},
		{"restart while disabled", func() error { _, err := f.plane.RestartTelemetryComponent(ctx, "grafana"); return err }, ErrConflict,
			[]event{{action: "telemetry.component.restart", outcome: audit.Failed, target: "grafana"}}},
		{"delete", func() error { return f.plane.DeleteTelemetryData(ctx) }, nil,
			[]event{{action: "telemetry.data.delete", outcome: audit.OK}}},
	}
	for _, step := range steps {
		err := step.run()
		if step.kind == nil && err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if step.kind != nil {
			if e, ok := Explain(err); !ok || !errors.Is(e, step.kind) {
				t.Fatalf("%s: err = %v, want %v", step.name, err, step.kind)
			}
		}
		got := f.audit.take()
		if len(got) != len(step.want) {
			t.Fatalf("%s: recorded %+v, want %+v", step.name, got, step.want)
		}
		for i, w := range step.want {
			e := got[i]
			if e.Action != w.action || e.Outcome != w.outcome || e.Target != w.target || e.Actor.Name != "octocat" {
				t.Errorf("%s: recorded %+v, want %+v", step.name, e, w)
			}
			if w.fields != nil && !slices.Equal(e.Detail["fields"].([]string), w.fields) {
				t.Errorf("%s: fields %v, want %v", step.name, e.Detail["fields"], w.fields)
			}
		}
	}
}

func TestServiceTelemetryCaps(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)
	defaults := observability.DefaultSettings().DefaultCaps

	got, err := f.plane.ServiceTelemetry(ctx, svc.ID)
	if err != nil || got.Caps != defaults || got.Overridden {
		t.Fatalf("defaults: %+v, %v", got, err)
	}
	f.audit.take()

	own := observability.Caps{SpansPerSecond: 10, LogsPerSecond: 20, Series: 30}
	got, err = f.plane.SetServiceTelemetryCaps(ctx, svc.ID, &own)
	if err != nil || got.Caps != own || !got.Overridden {
		t.Fatalf("override: %+v, %v", got, err)
	}
	if _, err := f.plane.SetServiceTelemetryCaps(ctx, svc.ID, &observability.Caps{SpansPerSecond: 1, LogsPerSecond: 0, Series: 1}); !errors.Is(err, ErrInvalid) {
		t.Errorf("zero cap: %v, want ErrInvalid", err)
	}
	got, err = f.plane.SetServiceTelemetryCaps(ctx, svc.ID, nil)
	if err != nil || got.Caps != defaults || got.Overridden {
		t.Fatalf("reset: %+v, %v", got, err)
	}
	if _, err := f.plane.ServiceTelemetry(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown service: %v, want ErrNotFound", err)
	}

	var outcomes []string
	for _, e := range f.audit.take() {
		if e.Action != "telemetry.caps.update" || e.ServiceID != svc.ID {
			t.Errorf("recorded %s on %s", e.Action, e.ServiceID)
		}
		outcomes = append(outcomes, string(e.Outcome))
	}
	if want := []string{"ok", "failed", "ok"}; !slices.Equal(outcomes, want) {
		t.Errorf("outcomes %v, want %v", outcomes, want)
	}
}
