package api

import (
	"context"
	"net/http"
	"time"

	"github.com/kyledickey/shed/internal/metrics"
)

// Metrics answers resource usage queries. *metrics.Collector implements it.
type Metrics interface {
	Query(ctx context.Context, serviceID string, r metrics.Range) (metrics.Series, error)
	QueryHost(ctx context.Context, r metrics.Range) (metrics.HostSeries, error)
}

var _ Metrics = (*metrics.Collector)(nil)

type metricsJSON struct {
	Range       metrics.Range `json:"range"`
	Start       time.Time     `json:"start"`
	Step        float64       `json:"step"`
	CPULimit    float64       `json:"cpuLimit"`
	MemoryLimit int64         `json:"memoryLimit"`
	CPU         []*float64    `json:"cpu"`
	Memory      []*float64    `json:"memory"`
	NetRx       []*float64    `json:"netRx"`
	NetTx       []*float64    `json:"netTx"`
	DiskRead    []*float64    `json:"diskRead"`
	DiskWrite   []*float64    `json:"diskWrite"`
}

type hostMetricsJSON struct {
	Range       metrics.Range `json:"range"`
	Start       time.Time     `json:"start"`
	Step        float64       `json:"step"`
	CPUs        int           `json:"cpus"`
	MemoryTotal int64         `json:"memoryTotal"`
	DiskTotal   int64         `json:"diskTotal"`
	CPU         []*float64    `json:"cpu"`
	Memory      []*float64    `json:"memory"`
	DiskUsed    []*float64    `json:"diskUsed"`
	NetRx       []*float64    `json:"netRx"`
	NetTx       []*float64    `json:"netTx"`
	DiskRead    []*float64    `json:"diskRead"`
	DiskWrite   []*float64    `json:"diskWrite"`
}

func (s *Server) hostMetrics(w http.ResponseWriter, r *http.Request) error {
	rng, err := metrics.ParseRange(r.URL.Query().Get("range"))
	if err != nil {
		return errorf(http.StatusBadRequest, "range must be one of 1h, 6h, 24h, or 7d")
	}
	m, err := s.metrics.QueryHost(r.Context(), rng)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, hostMetricsJSON{
		Range:       m.Range,
		Start:       m.Start,
		Step:        m.Step.Seconds(),
		CPUs:        m.CPUs,
		MemoryTotal: m.MemoryTotal,
		DiskTotal:   m.DiskTotal,
		CPU:         m.CPU,
		Memory:      m.Memory,
		DiskUsed:    m.DiskUsed,
		NetRx:       m.NetRx,
		NetTx:       m.NetTx,
		DiskRead:    m.DiskRead,
		DiskWrite:   m.DiskWrite,
	})
}

func (s *Server) serviceMetrics(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	rng, err := metrics.ParseRange(r.URL.Query().Get("range"))
	if err != nil {
		return errorf(http.StatusBadRequest, "range must be one of 1h, 6h, 24h, or 7d")
	}
	svc, err := s.store.Service(ctx, r.PathValue("id"))
	if err != nil {
		return err
	}
	m, err := s.metrics.Query(ctx, svc.ID, rng)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, metricsJSON{
		Range:       m.Range,
		Start:       m.Start,
		Step:        m.Step.Seconds(),
		CPULimit:    m.CPULimit,
		MemoryLimit: m.MemoryLimit,
		CPU:         m.CPU,
		Memory:      m.Memory,
		NetRx:       m.NetRx,
		NetTx:       m.NetTx,
		DiskRead:    m.DiskRead,
		DiskWrite:   m.DiskWrite,
	})
}
