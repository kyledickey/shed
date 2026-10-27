package deploy

import (
	"context"
	"slices"
	"sort"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/kyledickey/shed/internal/store"
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
