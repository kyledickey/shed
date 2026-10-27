package observability

import (
	"context"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/kyledickey/shed/internal/proxy"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/telemetry"
	"github.com/kyledickey/shed/internal/victoria"
)

// EdgeStore holds the domain table and the names of the services it points
// at. *store.Store implements it.
type EdgeStore interface {
	AllDomains(ctx context.Context) ([]store.Domain, error)
	AllServices(ctx context.Context) ([]store.Service, error)
	Projects(ctx context.Context) ([]store.Project, error)
}

var _ EdgeStore = (*store.Store)(nil)

// LogsWriter writes records to VictoriaLogs. *victoria.Logs implements it.
type LogsWriter interface {
	InsertJSONLines(ctx context.Context, records []victoria.Record, opts victoria.InsertOptions) error
}

var _ LogsWriter = (*victoria.Logs)(nil)

// EdgeConfig configures an Edge.
type EdgeConfig struct {
	Store EdgeStore
	Logs  LogsWriter
	// Mask returns the function that masks variable values in access
	// records.
	Mask func(ctx context.Context) (func(string) string, error)
	// DashboardHost is the host name the dashboard is served at, attributed
	// to shed. Empty if the proxy does not serve it.
	DashboardHost string
	Log           *slog.Logger
}

const (
	// edgeQueue is how many access entries wait to be shipped before new
	// ones are dropped.
	edgeQueue = 4096
	// edgeBatch and edgeFlushEvery bound a batch of access records.
	edgeBatch      = 1000
	edgeFlushEvery = time.Second
	// edgeReloadEvery is how often the host table and masked values are
	// read again, besides after every route change.
	edgeReloadEvery = 30 * time.Second
	edgeShipTimeout = 10 * time.Second
	// unroutedHost is the host label of requests for hosts no domain names,
	// which clients choose freely. Their service labels are empty, which
	// VictoriaMetrics drops, so they count under shed's own labels.
	unroutedHost = "unrouted"
)

// shedService is shed itself, which the dashboard host belongs to.
var shedService = telemetry.Service{ID: "shed", Name: "shed", Kind: "shed"}

// edgeStreamFields identify an access record's log stream.
var edgeStreamFields = []string{"shed.project.id", "shed.service.id", "shed.source"}

// Edge turns the proxy's access entries into request metrics and access
// records while telemetry is on. Handle is called from the proxy; Run is a
// Worker.
type Edge struct {
	cfg EdgeConfig

	running atomic.Bool
	hosts   atomic.Pointer[map[string]telemetry.Service]
	queue   chan edgeEntry
	refresh chan struct{}

	requests metric.Int64Counter
	duration metric.Float64Histogram
	bytes    metric.Int64Counter
	dropped  metric.Int64Counter
}

// edgeEntry is an access entry and the service its host belongs to.
type edgeEntry struct {
	proxy.AccessEntry
	svc telemetry.Service
}

// NewEdge returns an Edge that ignores entries until Run.
func NewEdge(cfg EdgeConfig) *Edge {
	// Creating an instrument fails only for an invalid name, and the
	// instrument returned works regardless.
	m := otel.Meter("github.com/kyledickey/shed/internal/observability")
	e := &Edge{
		cfg:     cfg,
		queue:   make(chan edgeEntry, edgeQueue),
		refresh: make(chan struct{}, 1),
	}
	e.requests, _ = m.Int64Counter("shed_http_requests_total",
		metric.WithDescription("Requests the proxy served, by service, host, method, and status code."))
	e.duration, _ = m.Float64Histogram("shed_http_request_duration_seconds",
		metric.WithDescription("Time the proxy took to serve a request, by service, host, and status code."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30))
	e.bytes, _ = m.Int64Counter("shed_http_response_bytes_total",
		metric.WithDescription("Response body bytes the proxy sent, by service and host."))
	e.dropped, _ = m.Int64Counter("shed_telemetry_dropped_total",
		metric.WithDescription("Telemetry records dropped, by service, signal, and reason."))
	return e
}

// Refresh reads the domain table again. Call it after routes change.
func (e *Edge) Refresh() {
	select {
	case e.refresh <- struct{}{}:
	default:
	}
}

// Handle records a request. It never blocks: when the queue of records to
// ship is full, the record is dropped and counted.
func (e *Edge) Handle(a proxy.AccessEntry) {
	if !e.running.Load() {
		return
	}
	svc, ok := (*e.hosts.Load())[a.Host]
	host := a.Host
	if !ok {
		host = unroutedHost
	}
	ctx := context.Background()
	labels := slices.Clip(append(svc.Labels(), attribute.String("shed_source", "edge")))
	code := attribute.String("code", strconv.Itoa(a.Status))
	hostAttr := attribute.String("host", host)
	e.requests.Add(ctx, 1, metric.WithAttributes(append(labels, hostAttr, code, attribute.String("method", methodLabel(a.Method)))...))
	e.duration.Record(ctx, a.Duration.Seconds(), metric.WithAttributes(append(labels, hostAttr, code)...))
	e.bytes.Add(ctx, a.BytesOut, metric.WithAttributes(append(labels, hostAttr)...))

	select {
	case e.queue <- edgeEntry{a, svc}:
	default:
		e.dropped.Add(ctx, 1, metric.WithAttributes(append(labels,
			attribute.String("signal", "logs"), attribute.String("reason", "edge_queue_full"))...))
	}
}

// Run ships access records in batches until ctx is done.
func (e *Edge) Run(ctx context.Context) {
	mask := e.reload(ctx, nil)
	e.running.Store(true)
	defer e.running.Store(false)

	reload := time.NewTicker(edgeReloadEvery)
	defer reload.Stop()
	flush := time.NewTicker(edgeFlushEvery)
	defer flush.Stop()
	var batch []victoria.Record
	ship := func(ctx context.Context) {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(ctx, edgeShipTimeout)
		defer cancel()
		if err := e.cfg.Logs.InsertJSONLines(ctx, batch, victoria.InsertOptions{StreamFields: edgeStreamFields}); err != nil {
			e.cfg.Log.Warn("ship access records", "records", len(batch), "err", err)
		}
		batch = batch[:0]
	}
	for {
		select {
		case <-ctx.Done():
			ship(context.WithoutCancel(ctx))
			return
		case <-e.refresh:
			mask = e.reload(ctx, mask)
		case <-reload.C:
			mask = e.reload(ctx, mask)
		case en := <-e.queue:
			// Without the values to mask, nothing may be shipped.
			if mask == nil {
				continue
			}
			batch = append(batch, edgeRecord(en, mask))
			if len(batch) >= edgeBatch {
				ship(ctx)
			}
		case <-flush.C:
			ship(ctx)
		}
	}
}

// reload reads the host table and the values to mask. On failure it keeps
// what it had, and returns mask.
func (e *Edge) reload(ctx context.Context, mask func(string) string) func(string) string {
	hosts, err := e.resolve(ctx)
	if err != nil {
		e.cfg.Log.Warn("read edge hosts", "err", err)
		if e.hosts.Load() == nil {
			hosts = map[string]telemetry.Service{}
		}
	}
	if hosts != nil {
		e.hosts.Store(&hosts)
	}
	m, err := e.cfg.Mask(ctx)
	if err != nil {
		e.cfg.Log.Warn("read values to mask in access records", "err", err)
		return mask
	}
	return m
}

// resolve maps each domain's host to its service, and the dashboard host to
// shed.
func (e *Edge) resolve(ctx context.Context) (map[string]telemetry.Service, error) {
	domains, err := e.cfg.Store.AllDomains(ctx)
	if err != nil {
		return nil, err
	}
	services, err := e.cfg.Store.AllServices(ctx)
	if err != nil {
		return nil, err
	}
	projects, err := e.cfg.Store.Projects(ctx)
	if err != nil {
		return nil, err
	}
	projectNames := make(map[string]string, len(projects))
	for _, p := range projects {
		projectNames[p.ID] = p.Name
	}
	byID := make(map[string]telemetry.Service, len(services))
	for _, s := range services {
		byID[s.ID] = telemetry.Service{
			ProjectID: s.ProjectID, ProjectName: projectNames[s.ProjectID],
			ID: s.ID, Name: s.Name, Kind: s.Kind,
		}
	}
	hosts := make(map[string]telemetry.Service, len(domains)+1)
	for _, d := range domains {
		if s, ok := byID[d.ServiceID]; ok {
			hosts[d.Host] = s
		}
	}
	if h := strings.ToLower(e.cfg.DashboardHost); h != "" {
		hosts[h] = shedService
	}
	return hosts, nil
}

// edgeRecord is the access record of en, with variable values masked.
func edgeRecord(en edgeEntry, mask func(string) string) victoria.Record {
	status := strconv.Itoa(en.Status)
	path := maskEncoded(en.Path, mask)
	return victoria.Record{
		"_time":             en.Time.UTC().Format(time.RFC3339Nano),
		"_msg":              en.Method + " " + path + " " + status,
		"shed.project.id":   en.svc.ProjectID,
		"shed.project.name": en.svc.ProjectName,
		"shed.service.id":   en.svc.ID,
		"shed.service.name": en.svc.Name,
		"shed.service.kind": en.svc.Kind,
		"shed.source":       "edge",
		"client_ip":         en.ClientIP,
		"method":            en.Method,
		"host":              en.Host,
		"path":              path,
		"query":             maskEncoded(en.Query, mask),
		"status":            status,
		"duration":          strconv.FormatFloat(en.Duration.Seconds(), 'f', -1, 64),
		"bytes_in":          strconv.FormatInt(en.BytesIn, 10),
		"bytes_out":         strconv.FormatInt(en.BytesOut, 10),
		"user_agent":        mask(en.UserAgent),
		"referer":           maskEncoded(en.Referer, mask),
		"tls_version":       en.TLSVersion,
		"trace_id":          en.TraceID,
	}
}

// maskEncoded masks s, a percent-encoded path, query, or URL. A value that
// only appears once s is decoded is masked in the decoded form.
func maskEncoded(s string, mask func(string) string) string {
	if dec, err := url.QueryUnescape(s); err == nil && dec != s {
		if m := mask(dec); m != dec {
			return m
		}
	}
	return mask(s)
}

// methodLabel is method if it is a standard HTTP method, else "other":
// clients choose the method, and a label must not grow without bound.
func methodLabel(method string) string {
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE":
		return method
	}
	return "other"
}
