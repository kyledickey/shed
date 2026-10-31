package telemetry

import (
	"context"
	"sync/atomic"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// The gates below make each signal's pipeline a no-op while on is false.

type gatedSampler struct {
	on   *atomic.Bool
	next sdktrace.Sampler
}

func (g gatedSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	if !g.on.Load() {
		return sdktrace.SamplingResult{Decision: sdktrace.Drop}
	}
	return g.next.ShouldSample(p)
}

func (g gatedSampler) Description() string { return "gated(" + g.next.Description() + ")" }

type gatedSpans struct {
	on *atomic.Bool
	sdktrace.SpanExporter
}

func (g gatedSpans) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if !g.on.Load() {
		return nil
	}
	return g.SpanExporter.ExportSpans(ctx, spans)
}

type gatedMetrics struct {
	on *atomic.Bool
	sdkmetric.Exporter
}

func (g gatedMetrics) Export(ctx context.Context, rm *metricdata.ResourceMetrics) error {
	if !g.on.Load() {
		return nil
	}
	return g.Exporter.Export(ctx, rm)
}

// gatedLogs refuses records before they are queued, so a disabled slog bridge
// costs no allocation.
type gatedLogs struct {
	on *atomic.Bool
	sdklog.Processor
}

func (g gatedLogs) Enabled(ctx context.Context, p sdklog.EnabledParameters) bool {
	return g.on.Load() && g.Processor.Enabled(ctx, p)
}

func (g gatedLogs) OnEmit(ctx context.Context, r *sdklog.Record) error {
	if !g.on.Load() {
		return nil
	}
	return g.Processor.OnEmit(ctx, r)
}

type gatedLogExporter struct {
	on *atomic.Bool
	sdklog.Exporter
}

func (g gatedLogExporter) Export(ctx context.Context, records []sdklog.Record) error {
	if !g.on.Load() {
		return nil
	}
	return g.Exporter.Export(ctx, records)
}
