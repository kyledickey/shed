package control

import (
	"context"
	"errors"
	"slices"
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

func (f *fakeTelemetry) DeleteData(context.Context) error {
	if f.s.Enabled {
		return observability.ErrEnabled
	}
	return nil
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
