package control

import (
	"context"

	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/observability"
)

// Telemetry runs the telemetry stack. *observability.Manager implements it.
type Telemetry interface {
	Status(ctx context.Context) (observability.Status, error)
	Settings(ctx context.Context) (observability.Settings, error)
	SetSettings(ctx context.Context, s observability.Settings) (observability.Settings, error)
	Restart(ctx context.Context, c observability.Component) error
	DeleteData(ctx context.Context) error
}

var _ Telemetry = (*observability.Manager)(nil)

// Dashboards keeps telemetry's Grafana folders and dashboards in step with
// projects and services. *observability.Dashboards implements it.
type Dashboards interface {
	// Refresh asks for the folders and dashboards to be reconciled soon,
	// after a project or service was created, renamed, or deleted. It does
	// not block.
	Refresh()
	Reset(ctx context.Context, uid string) error
	Restore(ctx context.Context) error
	Service(ctx context.Context, id string) (observability.ServiceDashboards, error)
}

var _ Dashboards = (*observability.Dashboards)(nil)

// Telemetry returns the telemetry settings and the state of the stack.
func (p *Plane) Telemetry(ctx context.Context) (observability.Status, error) {
	return p.telemetry.Status(ctx)
}

// TelemetryEnabled reports whether telemetry is on.
func (p *Plane) TelemetryEnabled(ctx context.Context) (bool, error) {
	s, err := p.telemetry.Settings(ctx)
	return s.Enabled, err
}

// SetTelemetry saves the telemetry settings; the stack follows them in the
// background. Turning telemetry on or off is recorded as telemetry.enable or
// telemetry.disable, other changes as telemetry.settings.update.
func (p *Plane) SetTelemetry(ctx context.Context, s observability.Settings) (observability.Status, error) {
	old, err := p.telemetry.Settings(ctx)
	if err != nil {
		return observability.Status{}, err
	}
	_, err = p.telemetry.SetSettings(ctx, s)
	if s.Enabled != old.Enabled {
		action := "telemetry.disable"
		if s.Enabled {
			action = "telemetry.enable"
		}
		p.record(ctx, err, audit.Event{Action: action})
	}
	fields := changed([]field{
		{"metricsRetentionDays", s.MetricsRetentionDays != old.MetricsRetentionDays},
		{"logsRetentionDays", s.LogsRetentionDays != old.LogsRetentionDays},
		{"tracesRetentionDays", s.TracesRetentionDays != old.TracesRetentionDays},
		{"diskLimitGB", s.DiskLimitGB != old.DiskLimitGB},
		{"defaultCaps", s.DefaultCaps != old.DefaultCaps},
	})
	if len(fields) > 0 || s.Enabled == old.Enabled {
		p.record(ctx, err, audit.Event{Action: "telemetry.settings.update", Detail: map[string]any{"fields": fields}})
	}
	if err != nil {
		return observability.Status{}, err
	}
	return p.telemetry.Status(ctx)
}

// RestartTelemetryComponent restarts one component's container.
func (p *Plane) RestartTelemetryComponent(ctx context.Context, name string) (observability.Status, error) {
	err := p.telemetry.Restart(ctx, observability.Component(name))
	p.record(ctx, err, audit.Event{Action: "telemetry.component.restart", Target: name})
	if err != nil {
		return observability.Status{}, err
	}
	return p.telemetry.Status(ctx)
}

// DeleteTelemetryData removes the telemetry containers and everything they
// stored. Telemetry must be off.
func (p *Plane) DeleteTelemetryData(ctx context.Context) error {
	err := p.telemetry.DeleteData(ctx)
	p.record(ctx, err, audit.Event{Action: "telemetry.data.delete"})
	return err
}

// ResetDashboard overwrites one of shed's default dashboards with its
// current template, discarding a user's changes.
func (p *Plane) ResetDashboard(ctx context.Context, uid string) error {
	err := p.dashboards.Reset(ctx, uid)
	p.record(ctx, err, audit.Event{Action: "telemetry.dashboard.reset", Target: uid})
	return err
}

// RestoreDashboards recreates the default dashboards a user deleted.
func (p *Plane) RestoreDashboards(ctx context.Context) error {
	err := p.dashboards.Restore(ctx)
	p.record(ctx, err, audit.Event{Action: "telemetry.dashboards.restore"})
	return err
}

// ServiceTelemetry is a service's telemetry, as its page shows it.
type ServiceTelemetry struct {
	// Grafana holds the service's folder, dashboards, and firing alerts;
	// it is empty while telemetry is off.
	Grafana observability.ServiceDashboards
}

// ServiceTelemetry returns the telemetry of service id.
func (p *Plane) ServiceTelemetry(ctx context.Context, id string) (ServiceTelemetry, error) {
	g, err := p.dashboards.Service(ctx, id)
	if err != nil {
		return ServiceTelemetry{}, err
	}
	return ServiceTelemetry{Grafana: g}, nil
}
