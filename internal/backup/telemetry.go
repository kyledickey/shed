package backup

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/kyledickey/shed/internal/telemetry"
)

const scope = "github.com/kyledickey/shed/internal/backup"

// instrument creates the Manager's metrics. Creating an instrument fails only
// for an invalid name, and the instrument returned works regardless.
func (m *Manager) instrument() {
	meter := otel.Meter(scope)
	m.runs, _ = meter.Int64Counter("shed_backups_total",
		metric.WithDescription("Backups and restores that ran, by kind and outcome."))
	m.bytes, _ = meter.Int64Histogram("shed_backup_bytes",
		metric.WithDescription("Size of the archives of successful backups."),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(1<<20, 10<<20, 100<<20, 1<<30, 10<<30, 100<<30))
	m.duration, _ = meter.Float64Histogram("shed_backup_duration_seconds",
		metric.WithDescription("Duration of backups and restores."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 30, 60, 300, 600, 1800, 3600, 7200))
}

// service names the service serviceID in telemetry. Backups of shed.db name
// none, so they carry shed's own labels.
func (m *Manager) service(ctx context.Context, serviceID string) (telemetry.Service, bool) {
	if serviceID == "" {
		return telemetry.Service{}, false
	}
	s := telemetry.Service{ID: serviceID}
	if sv, err := m.store.Service(ctx, serviceID); err == nil {
		s.Name, s.Kind, s.ProjectID = sv.Name, sv.Kind, sv.ProjectID
		if p, err := m.store.Project(ctx, sv.ProjectID); err == nil {
			s.ProjectName = p.Name
		}
	}
	return s, true
}

// finish ends span, the span of a job of kind backup or restore for
// serviceID, with the ID id, that started at start, and records the job's
// outcome. size is the archive size of a successful backup.
func (m *Manager) finish(ctx context.Context, span trace.Span, kind, id, serviceID string, start time.Time, err error, size int64) {
	defer span.End()
	ctx = context.WithoutCancel(ctx)
	var labels []attribute.KeyValue
	svc, ok := m.service(ctx, serviceID)
	if ok {
		span.SetAttributes(svc.Attributes()...)
		labels = svc.Labels()
	}
	outcome := "succeeded"
	if err != nil {
		outcome = "failed"
		span.SetStatus(codes.Error, err.Error())
	}
	m.events(outcomeEvent(kind, id, svc, start, err))
	kindAttr := attribute.String("kind", kind)
	m.runs.Add(ctx, 1, metric.WithAttributes(append(labels, kindAttr, attribute.String("outcome", outcome))...))
	m.duration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(append(labels, kindAttr)...))
	if err == nil && kind == "backup" {
		m.bytes.Record(ctx, size, metric.WithAttributes(labels...))
	}
}

// outcomeEvent is the event of a job of kind backup or restore, with the ID id, of svc that
// started at start and ended now with err.
func outcomeEvent(kind, id string, svc telemetry.Service, start time.Time, err error) telemetry.Event {
	target := "shed.db"
	if svc.ID != "" {
		target = svc.Name
	}
	e := telemetry.Event{ID: id, Service: svc, Start: start, End: time.Now()}
	switch {
	case kind == "restore" && err != nil:
		e.Kind, e.Text = telemetry.EventRestoreFailed, "Restore of "+target+" failed: "+err.Error()
	case kind == "restore":
		e.Kind, e.Text = telemetry.EventRestore, "Restored "+target
	case err != nil:
		e.Kind, e.Text = telemetry.EventBackupFailed, "Backup of "+target+" failed: "+err.Error()
	default:
		e.Kind, e.Text = telemetry.EventBackup, "Backed up "+target
	}
	return e
}

// step runs fn, a step of a backup or restore, in a span named name.
func step(ctx context.Context, name string, fn func(context.Context) error) error {
	ctx, span := otel.Tracer(scope).Start(ctx, name)
	defer span.End()
	err := fn(ctx)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}
