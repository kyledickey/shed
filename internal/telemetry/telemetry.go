// Package telemetry builds shed's own OpenTelemetry tracer, meter, and logger
// providers. They export OTLP/HTTP to shed's loopback gateway and can be
// switched on and off at runtime. [Providers.Register] makes them the
// OpenTelemetry globals, so instrumented packages call otel.Tracer and
// otel.Meter instead of taking providers as parameters. It also defines the
// [Event]s that packages report for dashboards to mark.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Config configures New.
type Config struct {
	// Endpoint is the OTLP/HTTP base URL, for example http://127.0.0.1:4318.
	Endpoint string
	// Version is the shed version, recorded as service.version.
	Version string
	// Host is the host name, recorded as host.name.
	Host string
}

// Providers holds shed's tracer, meter, and logger providers.
type Providers struct {
	enabled atomic.Bool
	tp      *sdktrace.TracerProvider
	mp      *sdkmetric.MeterProvider
	lp      *sdklog.LoggerProvider
}

// New returns providers that export to cfg.Endpoint once enabled. They start
// disabled. New does not contact the endpoint.
func New(ctx context.Context, cfg Config) (*Providers, error) {
	base := strings.TrimRight(cfg.Endpoint, "/")
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("service.name", "shed"),
		attribute.String("service.version", cfg.Version),
		attribute.String("host.name", cfg.Host),
		attribute.String("shed.service.id", "shed"),
		attribute.String("shed.service.kind", "shed"),
		attribute.String("shed.source", "shed"),
	))
	if err != nil {
		return nil, fmt.Errorf("telemetry: resource: %w", err)
	}

	p := &Providers{}
	te, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(base+"/v1/traces"))
	if err != nil {
		return nil, fmt.Errorf("telemetry: trace exporter: %w", err)
	}
	me, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(base+"/v1/metrics"))
	if err != nil {
		return nil, fmt.Errorf("telemetry: metric exporter: %w", err)
	}
	le, err := otlploghttp.New(ctx, otlploghttp.WithEndpointURL(base+"/v1/logs"))
	if err != nil {
		return nil, fmt.Errorf("telemetry: log exporter: %w", err)
	}

	p.tp = sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(gatedSampler{on: &p.enabled, next: sdktrace.ParentBased(sdktrace.AlwaysSample())}),
		sdktrace.WithBatcher(gatedSpans{on: &p.enabled, SpanExporter: te}),
	)
	p.mp = sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(gatedMetrics{on: &p.enabled, Exporter: me})),
	)
	p.lp = sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(gatedLogs{on: &p.enabled, Processor: sdklog.NewBatchProcessor(gatedLogExporter{on: &p.enabled, Exporter: le})}),
	)
	if err := runtime.Start(runtime.WithMeterProvider(p.mp)); err != nil {
		return nil, errors.Join(fmt.Errorf("telemetry: runtime metrics: %w", err), p.Shutdown(ctx))
	}
	return p, nil
}

// TracerProvider returns the tracer provider.
func (p *Providers) TracerProvider() trace.TracerProvider { return p.tp }

// MeterProvider returns the meter provider.
func (p *Providers) MeterProvider() metric.MeterProvider { return p.mp }

// LoggerProvider returns the logger provider.
func (p *Providers) LoggerProvider() log.LoggerProvider { return p.lp }

// Register makes p the global tracer, meter, and logger provider and installs
// the W3C trace context and baggage propagators. cmd/shed calls it once at
// startup; tests use the providers directly instead.
func (p *Providers) Register() {
	otel.SetTracerProvider(p.tp)
	otel.SetMeterProvider(p.mp)
	global.SetLoggerProvider(p.lp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
}

// SetEnabled turns export on or off. While off, spans are not recorded and
// anything already queued is dropped at export time.
func (p *Providers) SetEnabled(on bool) { p.enabled.Store(on) }

// Shutdown flushes pending data, if enabled, and stops the providers.
func (p *Providers) Shutdown(ctx context.Context) error {
	return errors.Join(p.tp.Shutdown(ctx), p.mp.Shutdown(ctx), p.lp.Shutdown(ctx))
}
