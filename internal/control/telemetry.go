package control

import (
	"context"
	"errors"

	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/observability"
	"github.com/kyledickey/shed/internal/store"
)

// Telemetry runs the telemetry stack. *observability.Manager implements it.
type Telemetry interface {
	Status(ctx context.Context) (observability.Status, error)
	Settings(ctx context.Context) (observability.Settings, error)
	SetSettings(ctx context.Context, s observability.Settings) (observability.Settings, error)
	Restart(ctx context.Context, c observability.Component) error
	DeleteData(ctx context.Context) error
	Dropping(ctx context.Context, serviceID string) (bool, error)
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
	List(ctx context.Context, folderUID string) ([]observability.Dashboard, error)
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
	// Caps are the effective caps: the service's override or the defaults.
	Caps observability.Caps
	// Overridden is set if the service has its own caps.
	Overridden bool
	// Dropping is set if records were dropped in the last 5 minutes.
	Dropping bool
	// Grafana holds the service's folder, dashboards, and firing alerts;
	// it is empty while telemetry is off.
	Grafana observability.ServiceDashboards
}

// ServiceTelemetry returns a service's telemetry.
func (p *Plane) ServiceTelemetry(ctx context.Context, serviceID string) (ServiceTelemetry, error) {
	if _, err := p.store.Service(ctx, serviceID); err != nil {
		return ServiceTelemetry{}, err
	}
	var out ServiceTelemetry
	switch c, err := p.store.TelemetryCaps(ctx, serviceID); {
	case err == nil:
		out.Caps = observability.Caps{SpansPerSecond: c.SpansPerSecond, LogsPerSecond: c.LogsPerSecond, Series: c.Series}
		out.Overridden = true
	case errors.Is(err, store.ErrNotFound):
		s, err := p.telemetry.Settings(ctx)
		if err != nil {
			return ServiceTelemetry{}, err
		}
		out.Caps = s.DefaultCaps
	default:
		return ServiceTelemetry{}, err
	}
	dropping, err := p.telemetry.Dropping(ctx, serviceID)
	if err != nil {
		return ServiceTelemetry{}, err
	}
	out.Dropping = dropping
	if out.Grafana, err = p.dashboards.Service(ctx, serviceID); err != nil {
		return ServiceTelemetry{}, err
	}
	return out, nil
}

// SetServiceTelemetryCaps sets a service's own caps, or with nil returns it
// to the defaults. Each cap must be at least 1. The gateway and the log
// shipper apply them within seconds.
func (p *Plane) SetServiceTelemetryCaps(ctx context.Context, serviceID string, caps *observability.Caps) (ServiceTelemetry, error) {
	err := p.setServiceTelemetryCaps(ctx, serviceID, caps)
	p.recordService(ctx, err, "telemetry.caps.update", serviceID, map[string]any{"overridden": caps != nil})
	if err != nil {
		return ServiceTelemetry{}, err
	}
	return p.ServiceTelemetry(ctx, serviceID)
}

func (p *Plane) setServiceTelemetryCaps(ctx context.Context, serviceID string, caps *observability.Caps) error {
	if _, err := p.store.Service(ctx, serviceID); err != nil {
		return err
	}
	if caps == nil {
		return p.store.DeleteTelemetryCaps(ctx, serviceID)
	}
	if caps.SpansPerSecond < 1 || caps.LogsPerSecond < 1 || caps.Series < 1 {
		return errorf(ErrInvalid, "every cap must be at least 1")
	}
	return p.store.SetTelemetryCaps(ctx, serviceID, store.TelemetryCaps{
		SpansPerSecond: caps.SpansPerSecond, LogsPerSecond: caps.LogsPerSecond, Series: caps.Series,
	})
}
