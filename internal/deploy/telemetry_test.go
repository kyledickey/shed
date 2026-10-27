package deploy

import (
	"context"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/telemetry"
	"github.com/kyledickey/shed/internal/telemetry/telemetrytest"
)

func TestDeploymentTrace(t *testing.T) {
	rec := telemetrytest.Install(t)
	f := newFixture(t)
	ctx := context.Background()
	svc := f.svc
	svc.WaitForCI = true
	if err := f.st.UpdateService(ctx, svc); err != nil {
		t.Fatal(err)
	}

	reqCtx, req := otel.Tracer("test").Start(ctx, "POST /api/services/{id}/deployments")
	dep, err := f.d.Deploy(reqCtx, f.svc.ID, store.TriggerManual, Commit{SHA: "abc1234def"})
	req.End()
	if err != nil {
		t.Fatal(err)
	}
	dep = f.wait(t, dep.ID, terminal)
	f.settle(t)
	if dep.Status != store.StatusActive {
		t.Fatalf("status = %s (%q), want active", dep.Status, dep.Error)
	}

	root := rec.Span("deploy")
	if root == nil {
		t.Fatal("no deploy span")
	}
	if root.Parent().IsValid() {
		t.Error("deploy span has a parent, want a root span")
	}
	if dep.TraceID == "" || dep.TraceID != root.SpanContext().TraceID().String() {
		t.Errorf("stored trace ID = %q, want the deploy span's %s", dep.TraceID, root.SpanContext().TraceID())
	}
	if !slices.ContainsFunc(root.Links(), func(l sdktrace.Link) bool {
		return l.SpanContext.SpanID() == req.SpanContext().SpanID()
	}) {
		t.Error("deploy span is not linked to the request span that queued it")
	}
	if got := telemetrytest.Attr(root, "shed.service.id"); got != f.svc.ID {
		t.Errorf("deploy span shed.service.id = %q, want %q", got, f.svc.ID)
	}

	var children []sdktrace.ReadOnlySpan
	for _, s := range rec.Spans() {
		if s.Parent().SpanID() == root.SpanContext().SpanID() && s.InstrumentationScope().Name == scope {
			children = append(children, s)
		}
	}
	sort.Slice(children, func(a, b int) bool { return children[a].StartTime().Before(children[b].StartTime()) })
	var stages []string
	for _, s := range children {
		stages = append(stages, s.Name())
	}
	want := []string{stageWaitForCI, stageBuild, stageStart, stageHealthCheck, stageActivate}
	if !slices.Equal(stages, want) {
		t.Errorf("stage spans = %q, want %q", stages, want)
	}

	labels := []attribute.KeyValue{
		attribute.String("shed_service_id", f.svc.ID),
		attribute.String("shed_service_name", "web"),
		attribute.String("shed_project_name", "demo"),
	}
	if n := rec.Sum(t, "shed_deployments_total", append(labels,
		attribute.String("trigger", "manual"), attribute.String("outcome", "active"))...); n != 1 {
		t.Errorf("shed_deployments_total{manual, active} = %d, want 1", n)
	}
	for _, stage := range want {
		if n := rec.Count(t, "shed_deploy_stage_duration_seconds", append(labels, attribute.String("stage", stage))...); n != 1 {
			t.Errorf("shed_deploy_stage_duration_seconds{stage=%q} count = %d, want 1", stage, n)
		}
	}
	if depth, ok := rec.Gauge(t, "shed_deploy_queue_depth"); !ok || depth != 0 {
		t.Errorf("shed_deploy_queue_depth = %d (recorded %v), want 0 once settled", depth, ok)
	}
}

func TestDeploymentTraceIDOnlyWhileRecording(t *testing.T) {
	telemetrytest.Install(t, sdktrace.WithSampler(sdktrace.NeverSample()))
	f := newFixture(t)
	dep := f.wait(t, f.deploy(t).ID, terminal)
	if dep.TraceID != "" {
		t.Errorf("trace ID = %q with spans not sampled, want empty", dep.TraceID)
	}
}

// eventLog records the events a Deployer reports.
type eventLog struct {
	mu     sync.Mutex
	events []telemetry.Event
}

func (l *eventLog) add(e telemetry.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func (l *eventLog) take() []telemetry.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.events
	l.events = nil
	return out
}

func kinds(events []telemetry.Event) []telemetry.EventKind {
	var out []telemetry.EventKind
	for _, e := range events {
		out = append(out, e.Kind)
	}
	return out
}

func TestEvents(t *testing.T) {
	f := newFixture(t)
	log := &eventLog{}
	f.d.events = log.add
	ctx := context.Background()

	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	events := log.take()
	if got, want := kinds(events), []telemetry.EventKind{telemetry.EventDeployStarted, telemetry.EventDeployActive}; !slices.Equal(got, want) {
		t.Fatalf("deploy events = %q, want %q", got, want)
	}
	want := telemetry.Service{ProjectID: f.svc.ProjectID, ProjectName: "demo", ID: f.svc.ID, Name: "web", Kind: "app"}
	for _, e := range events {
		if e.Service != want {
			t.Errorf("%s service = %+v, want %+v", e.Kind, e.Service, want)
		}
	}
	// The store keeps milliseconds.
	near := func(a, b time.Time) bool { return a.Sub(b).Abs() < time.Millisecond }
	if active := events[1]; !near(active.Start, *dep.StartedAt) || !near(active.End, *dep.FinishedAt) {
		t.Errorf("deploy.active spans %s to %s, want the deployment's %s to %s", active.Start, active.End, dep.StartedAt, dep.FinishedAt)
	}

	re, err := f.d.Redeploy(ctx, dep.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.wait(t, re.ID, terminal)
	f.settle(t)
	if err := f.d.RestartService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.d.StopService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	f.healthy = func() bool { return false }
	failed := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if failed.Status != store.StatusFailed {
		t.Fatalf("status = %s, want failed", failed.Status)
	}
	events = log.take()
	wantKinds := []telemetry.EventKind{
		telemetry.EventRedeploy, telemetry.EventDeployActive,
		telemetry.EventRestart, telemetry.EventStop,
		telemetry.EventDeployStarted, telemetry.EventDeployFailed,
	}
	if got := kinds(events); !slices.Equal(got, wantKinds) {
		t.Fatalf("events = %q, want %q", got, wantKinds)
	}
	for _, e := range events {
		if e.Service.ID != f.svc.ID || e.Service.ProjectName != "demo" {
			t.Errorf("%s service = %+v, want web in demo", e.Kind, e.Service)
		}
	}
}
