package metrics

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/victoria"
)

// Remote is the metrics store of the telemetry stack. While telemetry is on
// (see [Collector.SetRemote]) the collector pushes every tick to it instead
// of writing SQLite, and queries read from it. *victoria.Metrics implements
// it.
type Remote interface {
	ImportPrometheus(ctx context.Context, lines []string) error
	QueryRange(ctx context.Context, q string, start, end time.Time, step time.Duration) (*victoria.Result, error)
}

var _ Remote = (*victoria.Metrics)(nil)

// SetRemote turns pushing to and querying the remote store on or off. It
// does nothing without a Remote in the config.
func (c *Collector) SetRemote(on bool) {
	c.remoteOn.Store(on && c.remote != nil)
}

// The series pushed for a service's containers and for the host.
const (
	mCPU       = "shed_container_cpu_percent"
	mMemory    = "shed_container_memory_bytes"
	mNetRx     = "shed_container_network_rx_bytes_per_second"
	mNetTx     = "shed_container_network_tx_bytes_per_second"
	mDiskRead  = "shed_container_disk_read_bytes_per_second"
	mDiskWrite = "shed_container_disk_write_bytes_per_second"
	mCPULimit  = "shed_container_cpu_limit_percent"
	mMemLimit  = "shed_container_memory_limit_bytes"

	hCPU       = "shed_host_cpu_percent"
	hMemory    = "shed_host_memory_bytes"
	hDiskUsed  = "shed_host_disk_used_bytes"
	hNetRx     = "shed_host_network_rx_bytes_per_second"
	hNetTx     = "shed_host_network_tx_bytes_per_second"
	hDiskRead  = "shed_host_disk_read_bytes_per_second"
	hDiskWrite = "shed_host_disk_write_bytes_per_second"
	hMemTotal  = "shed_host_memory_total_bytes"
	hDiskTotal = "shed_host_disk_total_bytes"
)

// hostID is the shed.service.id of the host's series.
const hostID = "shed"

// limits are the resource limits of a service's running containers. Zero
// means unlimited.
type limits struct {
	cpuPercent float64
	memory     int64
}

// serviceTick is one service's sample of a tick with the labels it is
// pushed under.
type serviceTick struct {
	store.MetricSample
	labels string
	limits limits
}

// hostTick is the host's sample of a tick.
type hostTick struct {
	store.HostSample
	memoryTotal, diskTotal uint64
}

// pushTick sends a tick to the remote store as one import. It returns false
// when the remote is off or refused the samples, so the caller can write
// them to SQLite instead.
func (c *Collector) pushTick(ctx context.Context, now time.Time, services []serviceTick, h *hostTick) bool {
	if !c.remoteOn.Load() {
		return false
	}
	ms := strconv.FormatInt(now.UnixMilli(), 10)
	var lines []string
	add := func(name, labels string, v float64) {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return
		}
		lines = append(lines, name+labels+" "+strconv.FormatFloat(v, 'g', -1, 64)+" "+ms)
	}
	for _, s := range services {
		add(mCPU, s.labels, s.CPU)
		add(mMemory, s.labels, float64(s.Memory))
		add(mNetRx, s.labels, s.NetRx)
		add(mNetTx, s.labels, s.NetTx)
		add(mDiskRead, s.labels, s.DiskRead)
		add(mDiskWrite, s.labels, s.DiskWrite)
		add(mCPULimit, s.labels, s.limits.cpuPercent)
		add(mMemLimit, s.labels, float64(s.limits.memory))
	}
	if h != nil {
		l := labelSet{{"shed_service_id", hostID}, {"shed_service_name", hostID}, {"shed_service_kind", hostID}, {"shed_source", "sampler"}}.String()
		add(hCPU, l, h.CPU)
		add(hMemory, l, float64(h.Memory))
		add(hDiskUsed, l, float64(h.DiskUsed))
		add(hNetRx, l, h.NetRx)
		add(hNetTx, l, h.NetTx)
		add(hDiskRead, l, h.DiskRead)
		add(hDiskWrite, l, h.DiskWrite)
		add(hMemTotal, l, float64(h.memoryTotal))
		add(hDiskTotal, l, float64(h.diskTotal))
	}
	if err := c.remote.ImportPrometheus(ctx, lines); err != nil {
		c.log.Warn("push metric samples to telemetry; storing them in shed.db", "err", err)
		return false
	}
	return true
}

// labelSet is an ordered set of Prometheus labels.
type labelSet [][2]string

// String renders the set as `{k="v",...}`, leaving out empty values.
func (l labelSet) String() string {
	var b strings.Builder
	b.WriteByte('{')
	for _, kv := range l {
		if kv[1] == "" {
			continue
		}
		if b.Len() > 1 {
			b.WriteByte(',')
		}
		b.WriteString(kv[0])
		b.WriteByte('=')
		b.WriteString(strconv.Quote(kv[1]))
	}
	b.WriteByte('}')
	return b.String()
}

// serviceLabels returns the labels each service's series carry, by service
// ID: the shed.* resource attributes of the label schema.
func (c *Collector) serviceLabels(ctx context.Context) (map[string]string, error) {
	services, err := c.store.AllServices(ctx)
	if err != nil {
		return nil, err
	}
	projects, err := c.store.Projects(ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(projects))
	for _, p := range projects {
		names[p.ID] = p.Name
	}
	out := make(map[string]string, len(services))
	for _, s := range services {
		out[s.ID] = labelSet{
			{"shed_project_id", s.ProjectID}, {"shed_project_name", names[s.ProjectID]},
			{"shed_service_id", s.ID}, {"shed_service_name", s.Name}, {"shed_service_kind", s.Kind},
			{"shed_source", "sampler"},
		}.String()
	}
	return out, nil
}

// remoteMetricBuckets reads the averages of the service's samples from the remote
// store, shaped like [Store.MetricBuckets].
func (c *Collector) remoteMetricBuckets(ctx context.Context, serviceID string, from time.Time, step time.Duration, n int) ([]store.MetricBucket, error) {
	sel := "{shed_service_id=" + strconv.Quote(serviceID) + "}"
	rows, err := c.remoteRows(ctx, []string{
		mCPU + sel, mMemory + sel, mNetRx + sel, mNetTx + sel, mDiskRead + sel, mDiskWrite + sel,
	}, from, step, n)
	if err != nil {
		return nil, err
	}
	out := make([]store.MetricBucket, 0, len(rows))
	for _, r := range rows {
		out = append(out, store.MetricBucket{
			Index: r.index, CPU: r.v[0], Memory: r.v[1], NetRx: r.v[2], NetTx: r.v[3], DiskRead: r.v[4], DiskWrite: r.v[5],
		})
	}
	return out, nil
}

// remoteHostBuckets is like remoteMetricBuckets for the host.
func (c *Collector) remoteHostBuckets(ctx context.Context, from time.Time, step time.Duration, n int) ([]store.HostBucket, error) {
	sel := "{shed_service_id=" + strconv.Quote(hostID) + "}"
	rows, err := c.remoteRows(ctx, []string{
		hCPU + sel, hMemory + sel, hDiskUsed + sel, hNetRx + sel, hNetTx + sel, hDiskRead + sel, hDiskWrite + sel,
	}, from, step, n)
	if err != nil {
		return nil, err
	}
	out := make([]store.HostBucket, 0, len(rows))
	for _, r := range rows {
		out = append(out, store.HostBucket{
			Index: r.index, CPU: r.v[0], Memory: r.v[1], DiskUsed: r.v[2],
			NetRx: r.v[3], NetTx: r.v[4], DiskRead: r.v[5], DiskWrite: r.v[6],
		})
	}
	return out, nil
}

// row holds the value of every selector in one bucket.
type row struct {
	index int
	v     []float64
}

// remoteRows averages each selector over the n buckets of width step from
// from, like SQLite's AVG over [from + i*step, from + (i+1)*step). It
// evaluates avg_over_time at the end of each bucket, 1 ms back, where the
// store's (t-step, t] window covers exactly the bucket's whole seconds; the
// evaluation times are multiples of step, so the store has nothing to
// align. Buckets without samples are left out.
func (c *Collector) remoteRows(ctx context.Context, selectors []string, from time.Time, step time.Duration, n int) ([]row, error) {
	if step < time.Second || step%time.Second != 0 {
		return nil, fmt.Errorf("metrics: bucket width %v is not whole seconds", step)
	}
	start, end := from.Add(step), from.Add(time.Duration(n)*step)
	window := strconv.FormatInt(int64(step/time.Second), 10) + "s"
	results := make([]*victoria.Result, len(selectors))
	errs := make([]error, len(selectors))
	var wg sync.WaitGroup
	for i, sel := range selectors {
		wg.Go(func() {
			q := fmt.Sprintf("avg(avg_over_time(%s[%s] offset 1ms))", sel, window)
			results[i], errs[i] = c.remote.QueryRange(ctx, q, start, end, step)
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	byIndex := make(map[int]*row)
	for i, res := range results {
		for _, s := range res.Series {
			for _, p := range s.Values {
				if math.IsNaN(p.Value) {
					continue
				}
				idx := int(math.Round(float64(p.Time.Sub(from))/float64(step))) - 1
				if idx < 0 || idx >= n {
					continue
				}
				r := byIndex[idx]
				if r == nil {
					r = &row{index: idx, v: make([]float64, len(selectors))}
					byIndex[idx] = r
				}
				r.v[i] = p.Value
			}
		}
	}
	rows := make([]row, 0, len(byIndex))
	for idx := 0; idx < n; idx++ {
		if r := byIndex[idx]; r != nil {
			rows = append(rows, *r)
		}
	}
	return rows, nil
}
