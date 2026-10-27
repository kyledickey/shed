package store

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// installTelemetry makes recording providers the OpenTelemetry globals for
// the rest of the test.
func installTelemetry(t *testing.T) (*tracetest.SpanRecorder, *sdkmetric.ManualReader) {
	t.Helper()
	spans := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	prevTP, prevMP := otel.GetTracerProvider(), otel.GetMeterProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetMeterProvider(prevMP)
	})
	return spans, reader
}

func TestQuerySpansCarryStatementNotArguments(t *testing.T) {
	spans, reader := installTelemetry(t)
	s := newStore(t)
	ctx, parent := otel.Tracer("test").Start(context.Background(), "request")
	const name = "secret-project-name"
	p, err := s.CreateProject(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Project(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	parent.End()

	var insert, sel bool
	for _, sp := range spans.Ended() {
		for _, kv := range sp.Attributes() {
			if strings.Contains(kv.Value.String(), name) || strings.Contains(kv.Value.String(), p.ID) {
				t.Errorf("span %q attribute %s carries a query argument: %q", sp.Name(), kv.Key, kv.Value.String())
			}
		}
		q := attr(sp, "db.query.text")
		switch {
		case sp.Name() == "INSERT" && strings.Contains(q, "INSERT INTO projects"):
			insert = true
		case sp.Name() == "SELECT" && strings.Contains(q, "FROM projects"):
			sel = true
		}
		if sp.Name() != "request" && sp.Parent().SpanID() != parent.SpanContext().SpanID() {
			t.Errorf("span %q is not a child of the request span", sp.Name())
		}
	}
	if !insert || !sel {
		t.Errorf("want INSERT and SELECT spans with their statements, got %d spans", len(spans.Ended()))
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	if n := histogramCount(rm, "shed_store_query_duration_seconds"); n < 2 {
		t.Errorf("shed_store_query_duration_seconds count = %d, want at least 2", n)
	}
}

func TestQueriesWithoutRecordingParentHaveNoSpans(t *testing.T) {
	spans, _ := installTelemetry(t)
	s := newStore(t)
	mustProject(t, s, "p")
	if n := len(spans.Ended()); n != 0 {
		t.Errorf("got %d spans for queries outside a trace, want 0", n)
	}
}

func TestDeploymentTraceID(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	p := mustProject(t, s, "p")
	sv := mustService(t, s, Service{ProjectID: p.ID, Name: "web", Kind: "app"})
	d, err := s.CreateDeployment(ctx, Deployment{ServiceID: sv.ID, Status: StatusQueued, Trigger: TriggerManual})
	if err != nil {
		t.Fatal(err)
	}
	d.TraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	if err := s.UpdateDeployment(ctx, d); err != nil {
		t.Fatal(err)
	}
	got, err := s.Deployment(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TraceID != d.TraceID {
		t.Errorf("TraceID = %q, want %q", got.TraceID, d.TraceID)
	}
}

func attr(sp sdktrace.ReadOnlySpan, key string) string {
	for _, kv := range sp.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.String()
		}
	}
	return ""
}

func histogramCount(rm metricdata.ResourceMetrics, name string) uint64 {
	var n uint64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if h, ok := m.Data.(metricdata.Histogram[float64]); ok && m.Name == name {
				for _, dp := range h.DataPoints {
					n += dp.Count
				}
			}
		}
	}
	return n
}
