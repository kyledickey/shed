package observability

import (
	"context"
	"log/slog"
	"net/netip"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/host"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/telemetry"
	"github.com/kyledickey/shed/internal/victoria"
)

const scope = "github.com/kyledickey/shed/internal/observability"

// Watcher is what the Docker events follower and the egress worker read from
// the engine. *docker.Client implements it.
type Watcher interface {
	Events(ctx context.Context, since time.Time, fn func(docker.Event)) error
	IsOwnExec(id string) bool
	Inspect(ctx context.Context, id string) (docker.Container, error)
	List(ctx context.Context, labels map[string]string) ([]docker.Container, error)
	NetworkSubnets(ctx context.Context) ([]netip.Prefix, error)
}

var _ Watcher = (*docker.Client)(nil)

// Catalog names services and projects. *store.Store implements it.
type Catalog interface {
	Service(ctx context.Context, id string) (store.Service, error)
	Project(ctx context.Context, id string) (store.Project, error)
}

var _ Catalog = (*store.Store)(nil)

// Auditor records audit events. *audit.Log implements it.
type Auditor interface {
	Record(ctx context.Context, e audit.Event)
}

var _ Auditor = (*audit.Log)(nil)

// LogWriter writes to and searches VictoriaLogs. *victoria.Logs implements it.
type LogWriter interface {
	InsertJSONLines(ctx context.Context, records []victoria.Record, opts victoria.InsertOptions) error
	Query(ctx context.Context, logsql string, start, end time.Time, limit int) ([]victoria.Record, error)
}

var _ LogWriter = (*victoria.Logs)(nil)

// Conntrack reads the kernel's connection tracking table.
type Conntrack interface {
	// Flows returns the table's entries.
	Flows(ctx context.Context) ([]host.Flow, error)
	// EnableAccounting makes new connections count their packets and bytes.
	EnableAccounting() error
}

// Death describes a service container that died without shed or anyone else
// signalling it: it exited non-zero on its own or was killed for running out
// of memory.
type Death struct {
	ServiceID, Container string
	ExitCode             int
	OOM                  bool
}

// Security configures what the Manager records about containers: the Docker
// events follower, which runs whether telemetry is on or off, and the egress
// worker, which runs while it is on.
type Security struct {
	Docker    Watcher
	Catalog   Catalog
	Audit     Auditor
	Logs      LogWriter
	Conntrack Conntrack
	// Crashed, if set, is called from the events follower, whether telemetry
	// is on or not, for each [Death] of a container with a shed.service label.
	Crashed func(Death)
}

// security runs the Docker events follower and the egress worker.
type security struct {
	Security
	log *slog.Logger
	// on reports whether telemetry is enabled.
	on atomic.Bool

	restarts, ooms, egressBytes metric.Int64Counter

	// The delays below are fields so that tests can shorten them.
	settle                 time.Duration // how long an exec_create waits for shed to claim it
	minBackoff, maxBackoff time.Duration // between attempts to follow events
	interval               time.Duration // between conntrack dumps
	now                    func() time.Time
}

// newSecurity returns the follower and worker for cfg.
func newSecurity(cfg Security, log *slog.Logger) *security {
	s := &security{
		Security:   cfg,
		log:        log,
		settle:     time.Second,
		minBackoff: time.Second,
		maxBackoff: 30 * time.Second,
		interval:   10 * time.Second,
		now:        time.Now,
	}
	meter := otel.Meter(scope)
	s.restarts, _ = meter.Int64Counter("shed_container_restarts_total",
		metric.WithDescription("Starts of containers that had run before."))
	s.ooms, _ = meter.Int64Counter("shed_container_oom_kills_total",
		metric.WithDescription("Containers the kernel killed for running out of memory."))
	s.egressBytes, _ = meter.Int64Counter("shed_egress_bytes_total",
		metric.WithDescription("Bytes containers sent to addresses outside the container networks, by protocol."))
	return s
}

// installSecurity registers the follower and the worker with m.
func (m *Manager) installSecurity() {
	s := newSecurity(*m.cfg.Security, m.cfg.Log)
	m.AddAlways(s.followEvents)
	m.AddWorker(s.runEgress)
	m.OnEnabled(s.on.Store)
}

// Label names of the containers shed starts.
const (
	labelProject    = "shed.project"
	labelService    = "shed.service"
	labelDeployment = "shed.deployment"
	labelExporter   = "shed.exporter"
	labelTelemetry  = LabelComponent
)

// watched reports whether attrs, the labels of a container, are those of a
// container shed started.
func watched(attrs map[string]string) bool {
	for _, k := range []string{labelService, labelExporter, labelTelemetry} {
		if _, ok := attrs[k]; ok {
			return true
		}
	}
	return false
}

// shedService names shed itself in telemetry.
var shedService = telemetry.Service{ID: "shed", Name: "shed", Kind: "shed"}

// service names the service a container with the labels attrs belongs to:
// the one in shed.service, or shed.exporter for an exporter. Other
// containers, such as those of the telemetry stack, belong to shed.
func (s *security) service(ctx context.Context, attrs map[string]string) telemetry.Service {
	id := attrs[labelService]
	if id == "" {
		id = attrs[labelExporter]
	}
	if id == "" {
		return shedService
	}
	svc := telemetry.Service{ID: id, ProjectID: attrs[labelProject]}
	if sv, err := s.Catalog.Service(ctx, id); err == nil {
		svc.Name, svc.Kind, svc.ProjectID = sv.Name, sv.Kind, sv.ProjectID
	}
	if svc.ProjectID != "" {
		if p, err := s.Catalog.Project(ctx, svc.ProjectID); err == nil {
			svc.ProjectName = p.Name
		}
	}
	return svc
}

// streamFields are the VictoriaLogs stream fields of the records.
var streamFields = []string{"shed.project.id", "shed.service.id", "shed.source"}

// record returns a log record of source about svc at t.
func record(t time.Time, svc telemetry.Service, source, level, msg string) victoria.Record {
	return victoria.Record{
		"_time":             t.UTC().Format(time.RFC3339Nano),
		"_msg":              msg,
		"level":             level,
		"shed.source":       source,
		"shed.project.id":   svc.ProjectID,
		"shed.project.name": svc.ProjectName,
		"shed.service.id":   svc.ID,
		"shed.service.name": svc.Name,
		"shed.service.kind": svc.Kind,
	}
}

// ship writes records to VictoriaLogs. A failure is logged and the records
// are dropped: they describe the past, and the follower must not stall.
func (s *security) ship(ctx context.Context, records ...victoria.Record) {
	if len(records) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := s.Logs.InsertJSONLines(ctx, records, victoria.InsertOptions{StreamFields: streamFields}); err != nil {
		s.log.Warn("ship records to logs", "records", len(records), "err", err)
	}
}

// sleep waits for d or until ctx is done, and reports whether d passed.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
