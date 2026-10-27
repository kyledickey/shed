package observability

import (
	"context"
	"sync"
	"time"

	"github.com/kyledickey/shed/internal/docker"
)

// State is how a component is doing.
type State string

// The states of a component.
const (
	Running  State = "running"
	Starting State = "starting"
	Stopped  State = "stopped"
	Failed   State = "failed"
)

// ComponentStatus is the state and resource use of one component.
type ComponentStatus struct {
	Name    Component
	State   State
	Version string
	// Error is the last failure reconciling the component, "" if none.
	Error string
	// CPUPercent and MemoryBytes are nil while the container is not
	// running; CPUPercent also until a second sample exists.
	CPUPercent  *float64
	MemoryBytes *int64
	MemoryLimit int64
	// DiskBytes is the store's own count of its data; nil for Grafana and
	// stores that do not answer.
	DiskBytes *int64
}

// Ingest is what the gateway accepted, per second over the last minute.
type Ingest struct {
	Spans, Logs, Points float64
}

// Drop counts records dropped in the last hour for one reason and signal.
type Drop struct {
	Reason, Signal string
	Count          float64
}

// Alert is a firing Grafana alert.
type Alert struct {
	Name string
	// ServiceID is the service the alert is about, "" if none.
	ServiceID string
	Since     time.Time
}

// Status is the telemetry stack as the Telemetry page shows it.
type Status struct {
	Settings Settings
	// Components is empty while no container exists and telemetry is off.
	Components   []ComponentStatus
	Ingest       Ingest
	Dropped      []Drop
	Alerts       []Alert
	GrafanaURL   string
	OTLPEndpoint string
}

// probeTimeout bounds each query Status makes, so that a component that
// hangs does not hold the page.
const probeTimeout = 3 * time.Second

// Status returns the settings, every component's state and resource use,
// the ingest and drop rates, and the firing alerts.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	s, err := m.Settings(ctx)
	if err != nil {
		return Status{}, err
	}
	st := Status{Settings: s, GrafanaURL: m.cfg.GrafanaURL, OTLPEndpoint: m.cfg.OTLPEndpoint, Dropped: []Drop{}, Alerts: []Alert{}}
	running, err := m.containers(ctx)
	if err != nil {
		return Status{}, err
	}
	m.stateMu.Lock()
	errs := make(map[Component]string, len(m.errs))
	for c, e := range m.errs {
		errs[c] = e
	}
	m.stateMu.Unlock()

	var pw string
	if s.Enabled || running[Grafana] != nil {
		if pw, err = m.adminPassword(ctx); err != nil {
			return Status{}, err
		}
	}
	st.Components = []ComponentStatus{}
	for _, c := range order {
		if running[c] != nil || s.Enabled {
			st.Components = append(st.Components, ComponentStatus{
				Name: c, Version: components[c].version, MemoryLimit: components[c].memory, Error: errs[c],
			})
		}
	}
	var wg sync.WaitGroup
	for i := range st.Components {
		cs := &st.Components[i]
		wg.Go(func() { m.probe(ctx, cs, running[cs.Name], s.Enabled, pw) })
	}
	if s.Enabled {
		wg.Go(func() { st.Ingest, st.Dropped = m.rates(ctx) })
		wg.Go(func() {
			if a := m.alerts(ctx, pw); a != nil {
				st.Alerts = a
			}
		})
	}
	wg.Wait()
	return st, nil
}

// probe fills in cs from Docker and the component's own endpoints.
func (m *Manager) probe(ctx context.Context, cs *ComponentStatus, cur *docker.Container, enabled bool, pw string) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	healthy := false
	if cur != nil && cur.State == "running" {
		if st, err := m.cfg.Docker.Stats(ctx, cur.ID); err == nil {
			mem := int64(st.MemoryUsage)
			cs.MemoryBytes = &mem
			cs.CPUPercent = m.cpuPercent(cur.ID, st)
		}
		if cs.Name == Grafana {
			_, err := m.cfg.Grafana(pw).Health(ctx)
			healthy = err == nil
		} else {
			healthy = m.victoria(cs.Name).Health(ctx) == nil
			if n, err := m.victoria(cs.Name).DataSize(ctx); err == nil {
				cs.DiskBytes = &n
			}
		}
	}
	cs.State = state(enabled, cur, healthy, cs.Error)
}

// state derives a component's state from its container, whether it
// answered its health check, and its last reconcile failure.
func state(enabled bool, cur *docker.Container, healthy bool, failure string) State {
	switch {
	case failure != "":
		return Failed
	case cur == nil:
		return Starting
	case cur.State == "restarting" || cur.State == "dead":
		return Failed
	case cur.State == "running" && healthy:
		return Running
	case cur.State == "running":
		return Starting
	case enabled:
		return Starting
	}
	return Stopped
}

// cpuPercent returns the CPU use since the previous sample of container id,
// as the metrics sampler computes it, or nil for the first sample.
func (m *Manager) cpuPercent(id string, cur docker.Stats) *float64 {
	m.stateMu.Lock()
	prev, ok := m.samples[id]
	m.samples[id] = cur
	m.stateMu.Unlock()
	if !ok || !cur.Read.After(prev.Read) || cur.CPUTotal < prev.CPUTotal {
		return nil
	}
	dcpu := float64(cur.CPUTotal - prev.CPUTotal)
	var pct float64
	if cur.SystemCPU > prev.SystemCPU && cur.OnlineCPUs > 0 {
		pct = dcpu / float64(cur.SystemCPU-prev.SystemCPU) * float64(cur.OnlineCPUs) * 100
	} else {
		pct = dcpu / float64(cur.Read.Sub(prev.Read).Nanoseconds()) * 100
	}
	return &pct
}

// The queries behind the ingest and drop numbers.
const (
	ingestQuery  = `sum by (signal) (rate(shed_telemetry_ingested_total[1m]))`
	droppedQuery = `sum by (reason, signal) (increase(shed_telemetry_dropped_total[1h])) > 0`
)

// rates returns the ingest rate per signal and the drops of the last hour,
// zero while nothing records them.
func (m *Manager) rates(ctx context.Context) (Ingest, []Drop) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	var in Ingest
	drops := []Drop{}
	if res, err := m.cfg.Metrics.Query(ctx, ingestQuery, time.Time{}); err == nil {
		for _, s := range res.Series {
			if len(s.Values) == 0 {
				continue
			}
			switch v := s.Values[0].Value; s.Labels["signal"] {
			case "traces":
				in.Spans = v
			case "logs":
				in.Logs = v
			case "metrics":
				in.Points = v
			}
		}
	}
	if res, err := m.cfg.Metrics.Query(ctx, droppedQuery, time.Time{}); err == nil {
		for _, s := range res.Series {
			if len(s.Values) > 0 {
				drops = append(drops, Drop{Reason: s.Labels["reason"], Signal: s.Labels["signal"], Count: s.Values[0].Value})
			}
		}
	}
	return in, drops
}

// alerts returns Grafana's firing alerts, or nil if Grafana does not
// answer.
func (m *Manager) alerts(ctx context.Context, pw string) []Alert {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	firing, err := m.cfg.Grafana(pw).FiringAlerts(ctx)
	if err != nil {
		return nil
	}
	out := make([]Alert, 0, len(firing))
	for _, a := range firing {
		out = append(out, Alert{Name: a.Labels["alertname"], ServiceID: a.Labels["shed_service_id"], Since: a.ActiveAt})
	}
	return out
}
