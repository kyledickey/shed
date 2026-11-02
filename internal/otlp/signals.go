package otlp

import (
	"context"
	"hash/maphash"
	"time"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

var traces = signal[*coltracepb.ExportTraceServiceRequest]{
	name: "traces",
	new:  func() *coltracepb.ExportTraceServiceRequest { return &coltracepb.ExportTraceServiceRequest{} },
	count: func(req *coltracepb.ExportTraceServiceRequest) int64 {
		var n int64
		for _, rs := range req.ResourceSpans {
			n += int64(countSpans(rs))
		}
		return n
	},
	process: (*Handler).traces,
	forward: Forwarder.Traces,
	response: func(o outcome) proto.Message {
		resp := &coltracepb.ExportTraceServiceResponse{}
		if o.rejected > 0 {
			resp.PartialSuccess = &coltracepb.ExportTracePartialSuccess{
				RejectedSpans: o.rejected,
				ErrorMessage:  "spans over the service's rate cap were dropped",
			}
		}
		return resp
	},
}

var metrics = signal[*colmetricspb.ExportMetricsServiceRequest]{
	name: "metrics",
	new:  func() *colmetricspb.ExportMetricsServiceRequest { return &colmetricspb.ExportMetricsServiceRequest{} },
	count: func(req *colmetricspb.ExportMetricsServiceRequest) int64 {
		var n int64
		keep := func(dataPoint) bool { n++; return true }
		for _, rm := range req.ResourceMetrics {
			for _, sm := range rm.ScopeMetrics {
				for _, m := range sm.Metrics {
					filterPoints(m, keep)
				}
			}
		}
		return n
	},
	process: (*Handler).metrics,
	forward: Forwarder.Metrics,
	response: func(o outcome) proto.Message {
		resp := &colmetricspb.ExportMetricsServiceResponse{}
		if o.rejected > 0 {
			resp.PartialSuccess = &colmetricspb.ExportMetricsPartialSuccess{
				RejectedDataPoints: o.rejected,
				ErrorMessage:       "data points of series over the service's series cap were dropped",
			}
		}
		return resp
	},
}

var logs = signal[*collogspb.ExportLogsServiceRequest]{
	name: "logs",
	new:  func() *collogspb.ExportLogsServiceRequest { return &collogspb.ExportLogsServiceRequest{} },
	count: func(req *collogspb.ExportLogsServiceRequest) int64 {
		var n int64
		for _, rl := range req.ResourceLogs {
			for _, sl := range rl.ScopeLogs {
				n += int64(len(sl.LogRecords))
			}
		}
		return n
	},
	process: (*Handler).logs,
	forward: Forwarder.Logs,
	response: func(o outcome) proto.Message {
		resp := &collogspb.ExportLogsServiceResponse{}
		if o.rejected > 0 {
			resp.PartialSuccess = &collogspb.ExportLogsPartialSuccess{
				RejectedLogRecords: o.rejected,
				ErrorMessage:       "log records over the service's rate cap were dropped",
			}
		}
		return resp
	},
}

func (h *Handler) traces(ctx context.Context, req *coltracepb.ExportTraceServiceRequest, a Attribution) outcome {
	var o outcome
	var kept []*tracepb.ResourceSpans
	for _, g := range splitSpans(req.ResourceSpans, a) {
		src, rs := g.src, g.rs
		n := countSpans(rs)
		k := h.take(src.ServiceID, n, h.caps(src.ServiceID).SpansPerSecond, func(s *service) *bucket { return &s.spans })
		h.drop(ctx, src.ServiceID, "traces", reasonRateCap, int64(n-k))
		o.accepted += int64(k)
		o.rejected += int64(n - k)
		if k == 0 {
			continue
		}
		rs.ScopeSpans = keepFirst(rs.ScopeSpans, func(ss *tracepb.ScopeSpans) *[]*tracepb.Span { return &ss.Spans }, k)

		mask := h.masker(src)
		rs.Resource = stamp(rs.Resource, src, mask)
		serviceName, _ := Attrs{rs.Resource.Attributes}.Lookup(serviceNameKey)
		for _, ss := range rs.ScopeSpans {
			maskAttrs(ss.Scope.GetAttributes(), mask)
			for _, span := range ss.Spans {
				maskAttrs(span.Attributes, mask)
				if span.Kind != tracepb.Span_SPAN_KIND_CLIENT {
					span.Attributes = dropClientAttrs(span.Attributes)
				}
				if mask != nil && span.Status != nil {
					span.Status.Message = mask(span.Status.Message)
				}
				for _, e := range span.Events {
					maskAttrs(e.GetAttributes(), mask)
				}
				for _, l := range span.Links {
					maskAttrs(l.GetAttributes(), mask)
				}
				if span.Kind == tracepb.Span_SPAN_KIND_SERVER || span.Kind == tracepb.Span_SPAN_KIND_CONSUMER {
					o.spans = append(o.spans, spanSample{
						src:         src,
						serviceName: serviceName,
						name:        span.Name,
						status:      span.GetStatus().GetCode(),
						seconds:     spanSeconds(span),
					})
				}
			}
		}
		kept = append(kept, rs)
	}
	req.ResourceSpans = kept
	return o
}

func spanSeconds(span *tracepb.Span) float64 {
	if span.EndTimeUnixNano < span.StartTimeUnixNano {
		return 0
	}
	return float64(span.EndTimeUnixNano-span.StartTimeUnixNano) / 1e9
}

func (h *Handler) logs(ctx context.Context, req *collogspb.ExportLogsServiceRequest, a Attribution) outcome {
	var o outcome
	src := a.Source
	limit := h.caps(src.ServiceID).LogRecordsPerSecond
	mask := h.masker(src)
	kept := req.ResourceLogs[:0]
	for _, rl := range req.ResourceLogs {
		n := 0
		for _, sl := range rl.ScopeLogs {
			n += len(sl.LogRecords)
		}
		k := h.take(src.ServiceID, n, limit, func(s *service) *bucket { return &s.logs })
		h.drop(ctx, src.ServiceID, "logs", reasonRateCap, int64(n-k))
		o.accepted += int64(k)
		o.rejected += int64(n - k)
		if k == 0 {
			continue
		}
		rl.ScopeLogs = keepFirst(rl.ScopeLogs, func(sl *logspb.ScopeLogs) *[]*logspb.LogRecord { return &sl.LogRecords }, k)

		rl.Resource = stamp(rl.Resource, src, mask)
		for _, sl := range rl.ScopeLogs {
			if !a.Trusted && sl.Scope != nil {
				sl.Scope.Attributes = dropReserved(sl.Scope.Attributes)
			}
			maskAttrs(sl.Scope.GetAttributes(), mask)
			for _, lr := range sl.LogRecords {
				if !a.Trusted {
					lr.Attributes = dropReserved(lr.Attributes)
					// VictoriaLogs flattens a map body into fields too.
					if kv := lr.Body.GetKvlistValue(); kv != nil {
						kv.Values = dropReserved(kv.Values)
					}
				}
				maskAttrs(lr.Attributes, mask)
				maskValue(lr.Body, mask)
			}
		}
		kept = append(kept, rl)
	}
	req.ResourceLogs = kept
	return o
}

func (h *Handler) metrics(ctx context.Context, req *colmetricspb.ExportMetricsServiceRequest, a Attribution) outcome {
	var o outcome
	src := a.Source
	limit := h.caps(src.ServiceID).Series
	mask := h.masker(src)
	now := h.now()
	kept := req.ResourceMetrics[:0]
	for _, rm := range req.ResourceMetrics {
		rm.Resource = stamp(rm.Resource, src, mask)
		var rh maphash.Hash
		rh.SetSeed(h.seed)
		hashAttrs(&rh, rm.Resource.Attributes)
		resource := rh.Sum64()

		scopes := rm.ScopeMetrics[:0]
		for _, sm := range rm.ScopeMetrics {
			if !a.Trusted && sm.Scope != nil {
				sm.Scope.Attributes = dropReserved(sm.Scope.Attributes)
			}
			maskAttrs(sm.Scope.GetAttributes(), mask)
			ms := sm.Metrics[:0]
			for _, m := range sm.Metrics {
				maskAttrs(m.Metadata, mask)
				keep := func(p dataPoint) bool {
					if !a.Trusted {
						dropPointReserved(p)
					}
					maskAttrs(p.GetAttributes(), mask)
					if e, ok := p.(interface{ GetExemplars() []*metricspb.Exemplar }); ok {
						for _, ex := range e.GetExemplars() {
							maskAttrs(ex.GetFilteredAttributes(), mask)
						}
					}
					if limit > 0 && !h.admit(now, src.ServiceID, limit, h.series(resource, m.Name, p.GetAttributes())) {
						o.rejected++
						return false
					}
					o.accepted++
					return true
				}
				if filterPoints(m, keep) > 0 {
					ms = append(ms, m)
				}
			}
			sm.Metrics = ms
			if len(ms) > 0 {
				scopes = append(scopes, sm)
			}
		}
		rm.ScopeMetrics = scopes
		if len(scopes) > 0 {
			kept = append(kept, rm)
		}
	}
	req.ResourceMetrics = kept
	h.drop(ctx, src.ServiceID, "metrics", reasonSeriesCap, o.rejected)
	return o
}

// series hashes a metric's identity as the stores see it: the resource
// attributes, the metric name, and the data point attributes.
func (h *Handler) series(resource uint64, name string, attrs []*commonpb.KeyValue) uint64 {
	var mh maphash.Hash
	mh.SetSeed(h.seed)
	hashUint64(&mh, resource)
	hashString(&mh, name)
	hashAttrs(&mh, attrs)
	return mh.Sum64()
}

// admit reports whether a data point of series fits under serviceID's
// series cap of limit, and records it if so.
func (h *Handler) admit(now time.Time, serviceID string, limit int, series uint64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.service(serviceID).admit(now, limit, series)
}

// take removes up to n tokens from the bucket of serviceID that pick
// selects, and returns how many it removed.
func (h *Handler) take(serviceID string, n, rate int, pick func(*service) *bucket) int {
	now := h.now()
	h.mu.Lock()
	defer h.mu.Unlock()
	return pick(h.service(serviceID)).take(now, rate, n)
}

// sourcedSpans is a resource's spans that belong to one source.
type sourcedSpans struct {
	src Source
	rs  *tracepb.ResourceSpans
}

// splitSpans attributes each resource's spans. Without a per-span
// attribution, every resource keeps the request's source; with one, a
// resource is split into one resource per source its spans resolve to.
func splitSpans(in []*tracepb.ResourceSpans, a Attribution) []sourcedSpans {
	out := make([]sourcedSpans, 0, len(in))
	for _, rs := range in {
		if a.Span == nil {
			out = append(out, sourcedSpans{a.Source, rs})
			continue
		}
		res := Attrs{rs.GetResource().GetAttributes()}
		groups := map[Source]*tracepb.ResourceSpans{}
		var order []Source
		for _, ss := range rs.ScopeSpans {
			scopes := map[Source]*tracepb.ScopeSpans{}
			for _, span := range ss.Spans {
				src, ok := a.Span(res, Attrs{span.Attributes})
				if !ok {
					src = a.Source
				}
				g := groups[src]
				if g == nil {
					g = &tracepb.ResourceSpans{SchemaUrl: rs.SchemaUrl}
					if rs.Resource != nil {
						g.Resource = proto.CloneOf(rs.Resource)
					}
					groups[src] = g
					order = append(order, src)
				}
				sc := scopes[src]
				if sc == nil {
					sc = &tracepb.ScopeSpans{SchemaUrl: ss.SchemaUrl}
					if ss.Scope != nil {
						sc.Scope = proto.CloneOf(ss.Scope)
					}
					scopes[src] = sc
					g.ScopeSpans = append(g.ScopeSpans, sc)
				}
				sc.Spans = append(sc.Spans, span)
			}
		}
		for _, src := range order {
			out = append(out, sourcedSpans{src, groups[src]})
		}
	}
	return out
}

func countSpans(rs *tracepb.ResourceSpans) int {
	n := 0
	for _, ss := range rs.ScopeSpans {
		n += len(ss.Spans)
	}
	return n
}

// keepFirst keeps the first n records across scopes, in order, and drops
// scopes left without records.
func keepFirst[S, R any](scopes []S, records func(S) *[]R, n int) []S {
	out := scopes[:0]
	for _, s := range scopes {
		rs := records(s)
		if len(*rs) > n {
			*rs = (*rs)[:n]
		}
		n -= len(*rs)
		if len(*rs) > 0 {
			out = append(out, s)
		}
	}
	return out
}

// dataPoint is what every OTLP metric data point type has in common.
type dataPoint interface {
	GetAttributes() []*commonpb.KeyValue
}

// dropPointReserved removes the reserved attributes of p.
func dropPointReserved(p dataPoint) {
	switch p := p.(type) {
	case *metricspb.NumberDataPoint:
		p.Attributes = dropReserved(p.Attributes)
	case *metricspb.HistogramDataPoint:
		p.Attributes = dropReserved(p.Attributes)
	case *metricspb.ExponentialHistogramDataPoint:
		p.Attributes = dropReserved(p.Attributes)
	case *metricspb.SummaryDataPoint:
		p.Attributes = dropReserved(p.Attributes)
	}
}

// filterPoints keeps the data points of m for which keep returns true and
// returns how many are left.
func filterPoints(m *metricspb.Metric, keep func(dataPoint) bool) int {
	switch d := m.Data.(type) {
	case *metricspb.Metric_Gauge:
		if d.Gauge != nil {
			d.Gauge.DataPoints = filter(d.Gauge.DataPoints, keep)
			return len(d.Gauge.DataPoints)
		}
	case *metricspb.Metric_Sum:
		if d.Sum != nil {
			d.Sum.DataPoints = filter(d.Sum.DataPoints, keep)
			return len(d.Sum.DataPoints)
		}
	case *metricspb.Metric_Histogram:
		if d.Histogram != nil {
			d.Histogram.DataPoints = filter(d.Histogram.DataPoints, keep)
			return len(d.Histogram.DataPoints)
		}
	case *metricspb.Metric_ExponentialHistogram:
		if d.ExponentialHistogram != nil {
			d.ExponentialHistogram.DataPoints = filter(d.ExponentialHistogram.DataPoints, keep)
			return len(d.ExponentialHistogram.DataPoints)
		}
	case *metricspb.Metric_Summary:
		if d.Summary != nil {
			d.Summary.DataPoints = filter(d.Summary.DataPoints, keep)
			return len(d.Summary.DataPoints)
		}
	}
	return 0
}

func filter[P dataPoint](pts []P, keep func(dataPoint) bool) []P {
	out := pts[:0]
	for _, p := range pts {
		if keep(p) {
			out = append(out, p)
		}
	}
	return out
}
