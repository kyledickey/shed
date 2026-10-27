package otlp

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

const (
	// seriesWindow is how long a series counts against the series cap
	// after its last data point.
	seriesWindow = time.Hour
	// sweepEvery bounds how often a full series set is scanned for
	// expired entries.
	sweepEvery = time.Minute
	// maxSpanNames and maxServiceNames are the number of span names and
	// sender service.names per service that span metrics label by name;
	// later names become otherName.
	maxSpanNames    = 200
	maxServiceNames = 20
	otherName       = "other"
)

// service is the per-service state behind caps and span metrics. It is
// guarded by Handler.mu.
type service struct {
	spans, logs bucket
	// series maps a series hash to when it last had a data point. It
	// never holds more entries than the series cap.
	series map[uint64]time.Time
	swept  time.Time
	// spanNames and serviceNames are the names span metrics label by.
	spanNames, serviceNames map[string]bool
}

// bucket is a token bucket holding at most one second's worth of tokens.
type bucket struct {
	tokens float64
	last   time.Time
}

// take removes up to n tokens refilled at rate per second and returns how
// many it removed. A rate of zero or less means no limit.
func (b *bucket) take(now time.Time, rate, n int) int {
	if rate <= 0 {
		return n
	}
	if b.last.IsZero() {
		b.tokens = float64(rate)
	} else if d := now.Sub(b.last); d > 0 {
		b.tokens = min(float64(rate), b.tokens+d.Seconds()*float64(rate))
	}
	b.last = now
	k := min(n, int(b.tokens))
	b.tokens -= float64(k)
	return k
}

// admit reports whether a data point of series h fits under limit, and
// records it if so. A limit of zero or less means no limit.
func (s *service) admit(now time.Time, limit int, h uint64) bool {
	if limit <= 0 {
		return true
	}
	if s.series == nil {
		s.series = map[uint64]time.Time{}
	}
	if _, ok := s.series[h]; ok {
		s.series[h] = now
		return true
	}
	if len(s.series) >= limit && now.Sub(s.swept) >= sweepEvery {
		s.swept = now
		for k, t := range s.series {
			if now.Sub(t) > seriesWindow {
				delete(s.series, k)
			}
		}
	}
	if len(s.series) >= limit {
		return false
	}
	s.series[h] = now
	return true
}

// capped returns name, or otherName once names holds max other names.
func capped(names *map[string]bool, name string, max int) string {
	if (*names)[name] {
		return name
	}
	if len(*names) >= max {
		return otherName
	}
	if *names == nil {
		*names = map[string]bool{}
	}
	(*names)[name] = true
	return name
}

// service returns the state of serviceID. The caller holds h.mu.
func (h *Handler) service(serviceID string) *service {
	s := h.services[serviceID]
	if s == nil {
		s = &service{}
		h.services[serviceID] = s
	}
	return s
}

// spanSample is one span to record in shed_span_duration_seconds.
type spanSample struct {
	src Source
	// serviceName is the service.name of the span's resource, masked.
	serviceName string
	name        string
	status      tracepb.Status_StatusCode
	seconds     float64
}

var statusCodes = map[tracepb.Status_StatusCode]string{
	tracepb.Status_STATUS_CODE_UNSET: "unset",
	tracepb.Status_STATUS_CODE_OK:    "ok",
	tracepb.Status_STATUS_CODE_ERROR: "error",
}

func (h *Handler) recordSpans(ctx context.Context, samples []spanSample) {
	if len(samples) == 0 {
		return
	}
	h.mu.Lock()
	for i := range samples {
		s := &samples[i]
		svc := h.service(s.src.ServiceID)
		s.name = capped(&svc.spanNames, s.name, maxSpanNames)
		s.serviceName = capped(&svc.serviceNames, s.serviceName, maxServiceNames)
	}
	h.mu.Unlock()
	for _, s := range samples {
		status, ok := statusCodes[s.status]
		if !ok {
			status = "unset"
		}
		h.duration.Record(ctx, s.seconds, metric.WithAttributes(
			attribute.String("shed_project_id", s.src.ProjectID),
			attribute.String("shed_project_name", s.src.ProjectName),
			attribute.String("shed_service_id", s.src.ServiceID),
			attribute.String("shed_service_name", s.src.ServiceName),
			attribute.String("shed_service_kind", s.src.ServiceKind),
			attribute.String("service_name", s.serviceName),
			attribute.String("span_name", s.name),
			attribute.String("status_code", status),
		))
	}
}
