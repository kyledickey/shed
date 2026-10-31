// Package telemetrytest records what instrumented code sends to the
// OpenTelemetry globals, for tests.
package telemetrytest

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Recorder holds the spans and metrics recorded since Install.
type Recorder struct {
	spans  *tracetest.SpanRecorder
	reader *sdkmetric.ManualReader
}

// Install makes recording tracer and meter providers the OpenTelemetry
// globals until the test ends. Instruments created before Install do not
// record into it. Tests that call Install must not run in parallel.
func Install(t *testing.T, opts ...sdktrace.TracerProviderOption) *Recorder {
	t.Helper()
	r := &Recorder{spans: tracetest.NewSpanRecorder(), reader: sdkmetric.NewManualReader()}
	prevTP, prevMP := otel.GetTracerProvider(), otel.GetMeterProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(append(opts, sdktrace.WithSpanProcessor(r.spans))...))
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(r.reader)))
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetMeterProvider(prevMP)
	})
	return r
}

// Spans returns the spans that have ended.
func (r *Recorder) Spans() []sdktrace.ReadOnlySpan { return r.spans.Ended() }

// Span returns the first ended span named name, or nil.
func (r *Recorder) Span(name string) sdktrace.ReadOnlySpan {
	for _, s := range r.spans.Ended() {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

// Attr returns the value of the attribute key of s, or "".
func Attr(s sdktrace.ReadOnlySpan, key string) string {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.String()
		}
	}
	return ""
}

// Sum returns the total of the counter name over the data points that carry
// every attribute of attrs.
func (r *Recorder) Sum(t *testing.T, name string, attrs ...attribute.KeyValue) int64 {
	t.Helper()
	var n int64
	for _, m := range r.metrics(t, name) {
		if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
			for _, dp := range sum.DataPoints {
				if has(dp.Attributes, attrs) {
					n += dp.Value
				}
			}
		}
	}
	return n
}

// Count returns the number of measurements of the histogram name over the
// data points that carry every attribute of attrs.
func (r *Recorder) Count(t *testing.T, name string, attrs ...attribute.KeyValue) uint64 {
	t.Helper()
	var n uint64
	for _, m := range r.metrics(t, name) {
		switch h := m.Data.(type) {
		case metricdata.Histogram[float64]:
			n += count(h.DataPoints, attrs)
		case metricdata.Histogram[int64]:
			n += count(h.DataPoints, attrs)
		}
	}
	return n
}

func count[N int64 | float64](dps []metricdata.HistogramDataPoint[N], attrs []attribute.KeyValue) uint64 {
	var n uint64
	for _, dp := range dps {
		if has(dp.Attributes, attrs) {
			n += dp.Count
		}
	}
	return n
}

// Gauge returns the last value of the int64 gauge name, and whether it was
// recorded.
func (r *Recorder) Gauge(t *testing.T, name string) (int64, bool) {
	t.Helper()
	for _, m := range r.metrics(t, name) {
		if g, ok := m.Data.(metricdata.Gauge[int64]); ok && len(g.DataPoints) > 0 {
			return g.DataPoints[0].Value, true
		}
	}
	return 0, false
}

func (r *Recorder) metrics(t *testing.T, name string) []metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := r.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	var out []metricdata.Metrics
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				out = append(out, m)
			}
		}
	}
	return out
}

func has(set attribute.Set, attrs []attribute.KeyValue) bool {
	for _, kv := range attrs {
		if v, ok := set.Value(kv.Key); !ok || v != kv.Value {
			return false
		}
	}
	return true
}
