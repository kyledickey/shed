package deploy

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/telemetry"
)

const scope = "github.com/kyledickey/shed/internal/deploy"

// Pipeline stages: the names of their spans and the stage label of
// shed_deploy_stage_duration_seconds.
const (
	stageWaitForCI   = "wait_for_ci"
	stageBuild       = "build"
	stagePull        = "pull"
	stageStart       = "start"
	stageHealthCheck = "health_check"
	stageActivate    = "activate"
)

// instrument creates the Deployer's metrics. Creating an instrument fails
// only for an invalid name, and the instrument returned works regardless.
func (d *Deployer) instrument() {
	m := otel.Meter(scope)
	d.deployments, _ = m.Int64Counter("shed_deployments_total",
		metric.WithDescription("Deployments the pipeline ran, by trigger and outcome."))
	d.stages, _ = m.Float64Histogram("shed_deploy_stage_duration_seconds",
		metric.WithDescription("Duration of deploy pipeline stages."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600, 1800, 3600))
	depth, _ := m.Int64ObservableGauge("shed_deploy_queue_depth",
		metric.WithDescription("Deployments queued or running."))
	d.queueDepth, _ = m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		o.ObserveInt64(depth, d.inProgress())
		return nil
	}, depth)
}

// inProgress counts the deployments queued or running.
func (d *Deployer) inProgress() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	var n int64
	for _, w := range d.workers {
		if w.pending != nil {
			n++
		}
		if w.running != "" {
			n++
		}
	}
	return n
}

// startDeploy starts the root span of dep's run, linked to the span that
// queued it, and records the trace on dep while telemetry is on.
func startDeploy(ctx context.Context, dep *store.Deployment, queuedBy trace.SpanContext) (context.Context, trace.Span) {
	opts := []trace.SpanStartOption{trace.WithAttributes(
		attribute.String("shed.deployment.id", dep.ID),
		attribute.String("shed.deployment.trigger", string(dep.Trigger)),
	)}
	if queuedBy.IsValid() {
		opts = append(opts, trace.WithLinks(trace.Link{SpanContext: queuedBy}))
	}
	ctx, span := otel.Tracer(scope).Start(ctx, "deploy", opts...)
	if span.IsRecording() {
		dep.TraceID = span.SpanContext().TraceID().String()
	}
	return ctx, span
}

// service names the job's service in telemetry.
func (j *job) service() telemetry.Service {
	return telemetry.Service{
		ProjectID: j.project.ID, ProjectName: j.project.Name,
		ID: j.dep.ServiceID, Name: j.svc.Name, Kind: j.svc.Kind,
	}
}

// finish records the outcome of the job on its root span and in
// shed_deployments_total.
func (j *job) finish(ctx context.Context, span trace.Span) {
	svc := j.service()
	span.SetAttributes(svc.Attributes()...)
	span.SetAttributes(attribute.String("shed.deployment.status", string(j.dep.Status)))
	if j.dep.Status == store.StatusFailed {
		span.SetStatus(codes.Error, j.dep.Error)
	}
	j.deployments.Add(ctx, 1, metric.WithAttributes(append(svc.Labels(),
		attribute.String("trigger", string(j.dep.Trigger)),
		attribute.String("outcome", string(j.dep.Status)),
	)...))
	if j.dep.StartedAt == nil || j.dep.FinishedAt == nil {
		return
	}
	ev := telemetry.Event{ID: j.dep.ID, Service: svc, Start: *j.dep.StartedAt, End: *j.dep.FinishedAt}
	switch j.dep.Status {
	case store.StatusActive:
		ev.Kind, ev.Text = telemetry.EventDeployActive, "Deployment "+j.dep.ID+" active"+j.revision()
	case store.StatusFailed:
		ev.Kind, ev.Text = telemetry.EventDeployFailed, "Deployment "+j.dep.ID+" failed: "+j.dep.Error
	default:
		return
	}
	j.events(ev)
}

// started reports that the job's deployment started.
func (j *job) started() {
	kind := telemetry.EventDeployStarted
	if j.dep.Trigger == store.TriggerRedeploy {
		kind = telemetry.EventRedeploy
	}
	j.events(telemetry.Event{
		Kind: kind, ID: j.dep.ID, Service: j.service(), Start: *j.dep.StartedAt,
		Text: "Deployment " + j.dep.ID + " started (" + string(j.dep.Trigger) + ")" + j.revision(),
	})
}

// revision describes the deployment's commit or image for an event.
func (j *job) revision() string {
	switch {
	case j.dep.CommitSHA != "":
		return " at " + shortSHA(j.dep.CommitSHA)
	case j.dep.Image != "":
		return " of " + j.dep.Image
	}
	return ""
}

// serviceEvent reports an event of kind about svc now.
func (d *Deployer) serviceEvent(ctx context.Context, svc store.Service, kind telemetry.EventKind, text string) {
	s := telemetry.Service{ProjectID: svc.ProjectID, ID: svc.ID, Name: svc.Name, Kind: svc.Kind}
	if p, err := d.store.Project(ctx, svc.ProjectID); err == nil {
		s.ProjectName = p.Name
	}
	d.events(telemetry.Event{Kind: kind, Service: s, Start: time.Now(), Text: text})
}

// inStage runs fn as the pipeline stage name, in its own span, and records
// the stage's duration. A failure is recorded with variable values masked, as
// in the build log.
func (j *job) inStage(ctx context.Context, name string, fn func(context.Context) error) error {
	ctx, span := otel.Tracer(scope).Start(ctx, name)
	defer span.End()
	start := time.Now()
	err := fn(ctx)
	j.stages.Record(ctx, time.Since(start).Seconds(),
		metric.WithAttributes(append(j.service().Labels(), attribute.String("stage", name))...))
	if err != nil {
		span.SetStatus(codes.Error, j.redact(err.Error()))
	}
	return err
}
