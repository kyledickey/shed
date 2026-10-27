// Package otlp is shed's OTLP/HTTP receiver, the heart of its telemetry
// gateway.
//
// A [Handler] serves POST /v1/traces, /v1/metrics, and /v1/logs with
// protobuf or JSON bodies, optionally gzip-compressed. For each request it
// attributes the records to a shed service, overwrites the shed.* resource
// attributes, masks string attribute values and log bodies, enforces the
// service's caps, records span metrics, and forwards what is left as
// protobuf. Records over a cap are dropped and the sender gets an OTLP
// partial success naming how many, so SDKs do not retry them.
//
// The handler knows nothing about listeners, containers, or stores: callers
// pass in how to attribute a request, how to mask, what the caps are, and
// where to forward.
package otlp

import (
	"context"
	"errors"
	"fmt"
	"hash/maphash"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
)

// MaxBodySize is the largest request body accepted, in bytes, both as sent
// and after decompression.
const MaxBodySize = 8 << 20

// Source is who a record belongs to: the values of the shed.* resource
// attributes stamped on it.
type Source struct {
	// ProjectID is shed.project.id, "" for shed itself.
	ProjectID string
	// ProjectName is shed.project.name.
	ProjectName string
	// ServiceID is shed.service.id, "shed" for shed itself. Caps and span
	// name limits are kept per service ID.
	ServiceID string
	// ServiceName is shed.service.name, and service.name when the sender
	// did not set one.
	ServiceName string
	// ServiceKind is shed.service.kind: "app", a database kind, or "shed".
	ServiceKind string
	// DeploymentID is shed.deployment.id.
	DeploymentID string
	// Origin is shed.source. Empty means "otlp".
	Origin string
}

// Attribution says who sent a request.
type Attribution struct {
	// Source is stamped on every record of the request.
	Source Source
	// Span, if set, attributes spans one by one. It sees the attributes of
	// the span's resource and of the span itself; when it returns true, the
	// span is moved to a resource of its own stamped with the returned
	// Source, and counts against that service's caps. The loopback listener
	// uses it to attribute the proxy's edge spans by host.
	Span func(resource, span Attrs) (Source, bool)
	// Trusted keeps attributes in shed's namespace on data points, log
	// records, and scopes. Only shed's own telemetry is trusted: its data
	// points name the service they are about. Anyone else's are removed,
	// since the stores merge them with the resource's shed.* attributes.
	Trusted bool
}

// Attrs is a read-only view of an OTLP attribute list.
type Attrs struct {
	kvs []*commonpb.KeyValue
}

// Lookup returns the value of the string attribute key, and whether there
// is one. An attribute of another type reports false.
func (a Attrs) Lookup(key string) (string, bool) {
	for _, kv := range a.kvs {
		if kv.GetKey() != key {
			continue
		}
		v, ok := kv.GetValue().GetValue().(*commonpb.AnyValue_StringValue)
		if !ok {
			return "", false
		}
		return v.StringValue, true
	}
	return "", false
}

// Caps are a service's ingest limits. A zero field means no limit.
type Caps struct {
	// SpansPerSecond is the token bucket rate for spans; the bucket holds
	// one second's worth.
	SpansPerSecond int
	// LogRecordsPerSecond is the token bucket rate for log records.
	LogRecordsPerSecond int
	// Series is the number of distinct metric name and attribute sets
	// accepted in the last hour.
	Series int
}

// Forwarder delivers rewritten requests to the stores. Each body is a
// protobuf-encoded OTLP Export*ServiceRequest.
type Forwarder interface {
	Traces(ctx context.Context, body []byte) error
	Metrics(ctx context.Context, body []byte) error
	Logs(ctx context.Context, body []byte) error
}

// Config holds a Handler's dependencies. Attribute and Forwarder are
// required.
type Config struct {
	// Attribute resolves who sent r. An error answers 503.
	Attribute func(r *http.Request) (Attribution, error)
	// Mask returns the masking function for a source's records, applied to
	// every string attribute value, span status message, and log body. Nil
	// means no masking.
	Mask func(Source) func(string) string
	// Caps returns a service's caps. Nil means no caps.
	Caps func(serviceID string) Caps
	// Forwarder receives every request that still has records after caps.
	Forwarder Forwarder
	// Logger logs forwarding failures. Nil discards.
	Logger *slog.Logger
	// Now is the clock for caps. Nil means time.Now.
	Now func() time.Time
}

// Handler is the OTLP/HTTP receiver. It is safe for concurrent use.
type Handler struct {
	cfg  Config
	log  *slog.Logger
	now  func() time.Time
	seed maphash.Seed

	ingested metric.Int64Counter
	dropped  metric.Int64Counter
	duration metric.Float64Histogram

	mu       sync.Mutex
	services map[string]*service
}

// Drop reasons, the reason label of shed_telemetry_dropped_total.
const (
	reasonRateCap   = "rate_cap"
	reasonSeriesCap = "series_cap"
)

// New returns a Handler for cfg.
func New(cfg Config) (*Handler, error) {
	if cfg.Attribute == nil || cfg.Forwarder == nil {
		return nil, errors.New("otlp: Attribute and Forwarder are required")
	}
	h := &Handler{
		cfg:      cfg,
		log:      cfg.Logger,
		now:      cfg.Now,
		seed:     maphash.MakeSeed(),
		services: map[string]*service{},
	}
	if h.log == nil {
		h.log = slog.New(slog.DiscardHandler)
	}
	if h.now == nil {
		h.now = time.Now
	}
	m := otel.Meter("github.com/kyledickey/shed/internal/otlp")
	var err error
	if h.ingested, err = m.Int64Counter("shed_telemetry_ingested_total",
		metric.WithDescription("Telemetry records accepted by the OTLP gateway.")); err != nil {
		return nil, fmt.Errorf("otlp: ingested counter: %w", err)
	}
	if h.dropped, err = m.Int64Counter("shed_telemetry_dropped_total",
		metric.WithDescription("Telemetry records dropped by the OTLP gateway.")); err != nil {
		return nil, fmt.Errorf("otlp: dropped counter: %w", err)
	}
	if h.duration, err = m.Float64Histogram("shed_span_duration_seconds",
		metric.WithDescription("Duration of server and consumer spans."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10)); err != nil {
		return nil, fmt.Errorf("otlp: span duration histogram: %w", err)
	}
	return h, nil
}

// ServeHTTP serves POST /v1/traces, /v1/metrics, and /v1/logs.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/v1/traces":
		serve(h, w, r, traces)
	case "/v1/metrics":
		serve(h, w, r, metrics)
	case "/v1/logs":
		serve(h, w, r, logs)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) drop(ctx context.Context, serviceID, signal, reason string, n int64) {
	if n == 0 {
		return
	}
	h.dropped.Add(ctx, n, metric.WithAttributes(
		attribute.String("shed_service_id", serviceID),
		attribute.String("signal", signal),
		attribute.String("reason", reason),
	))
}

func (h *Handler) caps(serviceID string) Caps {
	if h.cfg.Caps == nil {
		return Caps{}
	}
	return h.cfg.Caps(serviceID)
}

func (h *Handler) masker(src Source) func(string) string {
	if h.cfg.Mask == nil {
		return nil
	}
	return h.cfg.Mask(src)
}

// AdmitLogs takes n log records from serviceID's log cap, the bucket its
// OTLP logs draw from, and returns how many fit. The rest are counted as
// dropped (rate_cap). Records shed collects itself, such as container
// output, use it so one cap covers everything a service logs.
func (h *Handler) AdmitLogs(ctx context.Context, serviceID string, n int) int {
	k := h.take(serviceID, n, h.caps(serviceID).LogRecordsPerSecond, func(s *service) *bucket { return &s.logs })
	h.drop(ctx, serviceID, "logs", reasonRateCap, int64(n-k))
	return k
}

// CountIngested adds n records of signal ("traces", "metrics", or "logs")
// that a store accepted to shed_telemetry_ingested_total, for records shed
// collects itself.
func (h *Handler) CountIngested(ctx context.Context, signal string, n int) {
	h.ingested.Add(ctx, int64(n), metric.WithAttributes(attribute.String("signal", signal)))
}
