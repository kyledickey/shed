// Package metrics records the resource usage of service containers and of the
// host, and serves it as time series.
//
// A [Collector] samples every running service container through Docker at a
// fixed interval, turns the cumulative counters into rates, sums them per
// service, and stores one sample per service per tick. It samples the host
// alongside, into one host sample per tick. [Collector.Query] and
// [Collector.QueryHost] average the stored samples into a fixed number of
// buckets over a range.
//
// Docker, the host, and the store are reached through small interfaces so
// they can be replaced, for example by fakes in tests.
package metrics

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/host"
	"github.com/kyledickey/shed/internal/store"
)

// serviceLabel is the container label that internal/deploy sets to the ID of
// the service a container belongs to.
const serviceLabel = "shed.service"

// Docker is the subset of the Docker client the collector uses.
// *docker.Client implements it.
type Docker interface {
	List(ctx context.Context, labels map[string]string) ([]docker.Container, error)
	Inspect(ctx context.Context, id string) (docker.Container, error)
	Stats(ctx context.Context, id string) (docker.Stats, error)
}

// Host reads the host's resource usage. host.Reader implements it.
type Host interface {
	Read(ctx context.Context) (host.Stats, error)
}

// Store persists metric samples. *store.Store implements it.
type Store interface {
	InsertMetricSamples(ctx context.Context, samples []store.MetricSample) error
	MetricBuckets(ctx context.Context, serviceID string, from time.Time, step time.Duration, n int) ([]store.MetricBucket, error)
	InsertHostSample(ctx context.Context, sample store.HostSample) error
	HostBuckets(ctx context.Context, from time.Time, step time.Duration, n int) ([]store.HostBucket, error)
	DeleteMetricSamplesBefore(ctx context.Context, t time.Time) error
}

// Config configures a Collector.
type Config struct {
	Docker Docker
	// Host is sampled alongside the containers; nil means it is not.
	Host  Host
	Store Store
	// Interval is the time between samples; zero means 10 seconds.
	Interval time.Duration
	// Retention is how long samples are kept; zero means 7 days.
	Retention time.Duration
	// Now returns the current time; nil means time.Now.
	Now func() time.Time
	Log *slog.Logger
}

// pruneEvery is how often samples older than the retention are deleted.
const pruneEvery = time.Hour

// Collector samples container resource usage and answers queries over the
// stored samples.
type Collector struct {
	docker    Docker
	host      Host
	store     Store
	interval  time.Duration
	retention time.Duration
	now       func() time.Time
	log       *slog.Logger

	// Owned by the Run goroutine.
	prev       map[string]docker.Stats // last sample by container ID
	hostPrev   *host.Stats
	hostPrevAt time.Time
	lastPrune  time.Time
}

// New returns a Collector.
func New(cfg Config) *Collector {
	c := &Collector{
		docker:    cfg.Docker,
		host:      cfg.Host,
		store:     cfg.Store,
		interval:  cfg.Interval,
		retention: cfg.Retention,
		now:       cfg.Now,
		log:       cfg.Log,
		prev:      make(map[string]docker.Stats),
	}
	if c.interval <= 0 {
		c.interval = 10 * time.Second
	}
	if c.retention <= 0 {
		c.retention = 7 * 24 * time.Hour
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c
}

// Run samples containers every interval until ctx is done. Failures are
// logged and do not stop the loop.
func (c *Collector) Run(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		c.collect(ctx, c.now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// collect takes one sample of every running service container and stores
// the per-service sums of their rates since the previous sample.
func (c *Collector) collect(ctx context.Context, now time.Time) {
	now = now.Truncate(time.Second)
	if now.Sub(c.lastPrune) >= pruneEvery {
		if err := c.store.DeleteMetricSamplesBefore(ctx, now.Add(-c.retention)); err != nil {
			c.log.Warn("prune metric samples", "err", err)
		}
		c.lastPrune = now
	}
	c.collectHost(ctx, now)

	containers, err := c.docker.List(ctx, map[string]string{serviceLabel: ""})
	if err != nil {
		c.log.Warn("list containers for metrics", "err", err)
		return
	}
	seen := make(map[string]docker.Stats)
	sums := make(map[string]usage)
	for _, ctr := range containers {
		svc := ctr.Labels[serviceLabel]
		if !ctr.Running || svc == "" {
			continue
		}
		st, err := c.docker.Stats(ctx, ctr.ID)
		if err != nil {
			c.log.Warn("container stats", "container", ctr.ID, "err", err)
			continue
		}
		if st.Read.IsZero() {
			st.Read = now
		}
		seen[ctr.ID] = st
		prev, ok := c.prev[ctr.ID]
		if !ok {
			continue
		}
		if u, ok := rates(prev, st); ok {
			sums[svc] = sums[svc].add(u)
		}
	}
	c.prev = seen // Forget containers that are gone.

	if len(sums) == 0 {
		return
	}
	samples := make([]store.MetricSample, 0, len(sums))
	for _, svc := range slices.Sorted(maps.Keys(sums)) {
		u := sums[svc]
		samples = append(samples, store.MetricSample{
			ServiceID: svc, Time: now,
			CPU: u.cpu, Memory: int64(u.memory),
			NetRx: u.netRx, NetTx: u.netTx,
			DiskRead: u.diskRead, DiskWrite: u.diskWrite,
		})
	}
	if err := c.store.InsertMetricSamples(ctx, samples); err != nil {
		c.log.Warn("store metric samples", "err", err)
	}
}

// collectHost takes one sample of the host and stores its rates since the
// previous sample.
func (c *Collector) collectHost(ctx context.Context, now time.Time) {
	if c.host == nil {
		return
	}
	cur, err := c.host.Read(ctx)
	if err != nil {
		c.log.Warn("read host stats", "err", err)
		return
	}
	prev, prevAt := c.hostPrev, c.hostPrevAt
	c.hostPrev, c.hostPrevAt = &cur, now
	if prev == nil {
		return
	}
	u, ok := hostRates(*prev, cur, now.Sub(prevAt))
	if !ok {
		return
	}
	err = c.store.InsertHostSample(ctx, store.HostSample{
		Time: now, CPU: u.cpu, Memory: int64(u.memory), DiskUsed: int64(cur.DiskUsed),
		NetRx: u.netRx, NetTx: u.netTx, DiskRead: u.diskRead, DiskWrite: u.diskWrite,
	})
	if err != nil {
		c.log.Warn("store host sample", "err", err)
	}
}

// usage is resource usage over an interval: CPU in percent of one core,
// memory in bytes, and the rest in bytes per second.
type usage struct {
	cpu, memory         float64
	netRx, netTx        float64
	diskRead, diskWrite float64
}

func (u usage) add(v usage) usage {
	return usage{
		cpu: u.cpu + v.cpu, memory: u.memory + v.memory,
		netRx: u.netRx + v.netRx, netTx: u.netTx + v.netTx,
		diskRead: u.diskRead + v.diskRead, diskWrite: u.diskWrite + v.diskWrite,
	}
}

// rates returns the usage between two samples of the same container. It
// reports false if no time passed or a counter went backwards, as it does
// when the container restarts.
func rates(prev, cur docker.Stats) (usage, bool) {
	dt := cur.Read.Sub(prev.Read)
	if dt <= 0 || cur.CPUTotal < prev.CPUTotal ||
		cur.NetRx < prev.NetRx || cur.NetTx < prev.NetTx ||
		cur.DiskRead < prev.DiskRead || cur.DiskWrite < prev.DiskWrite {
		return usage{}, false
	}
	secs := dt.Seconds()
	dcpu := float64(cur.CPUTotal - prev.CPUTotal)
	u := usage{
		memory:    float64(cur.MemoryUsage),
		netRx:     float64(cur.NetRx-prev.NetRx) / secs,
		netTx:     float64(cur.NetTx-prev.NetTx) / secs,
		diskRead:  float64(cur.DiskRead-prev.DiskRead) / secs,
		diskWrite: float64(cur.DiskWrite-prev.DiskWrite) / secs,
	}
	if cur.SystemCPU > prev.SystemCPU && cur.OnlineCPUs > 0 {
		u.cpu = dcpu / float64(cur.SystemCPU-prev.SystemCPU) * float64(cur.OnlineCPUs) * 100
	} else {
		u.cpu = dcpu / float64(dt.Nanoseconds()) * 100
	}
	return u, true
}

// hostRates returns the host's usage between two readings dt apart. It
// reports false if no time passed or a counter went backwards.
func hostRates(prev, cur host.Stats, dt time.Duration) (usage, bool) {
	if dt <= 0 || cur.CPUTotal <= prev.CPUTotal ||
		cur.NetRx < prev.NetRx || cur.NetTx < prev.NetTx ||
		cur.DiskRead < prev.DiskRead || cur.DiskWrite < prev.DiskWrite {
		return usage{}, false
	}
	secs := dt.Seconds()
	total := float64(cur.CPUTotal - prev.CPUTotal)
	// Idle includes iowait, which the kernel may report going backwards, so
	// clamp busy time to the elapsed total.
	busy := min(max(total-(float64(cur.CPUIdle)-float64(prev.CPUIdle)), 0), total)
	return usage{
		cpu:       busy / total * float64(cur.CPUs) * 100,
		memory:    float64(cur.MemoryTotal - min(cur.MemoryAvailable, cur.MemoryTotal)),
		netRx:     float64(cur.NetRx-prev.NetRx) / secs,
		netTx:     float64(cur.NetTx-prev.NetTx) / secs,
		diskRead:  float64(cur.DiskRead-prev.DiskRead) / secs,
		diskWrite: float64(cur.DiskWrite-prev.DiskWrite) / secs,
	}, true
}

// Range is a time range of a metrics query.
type Range string

// Supported ranges.
const (
	Range1h  Range = "1h"
	Range6h  Range = "6h"
	Range24h Range = "24h"
	Range7d  Range = "7d"
)

// ErrInvalidRange is returned for a range that is not supported.
var ErrInvalidRange = errors.New("metrics: invalid range")

// ParseRange returns the range named s. An empty s means 1h.
func ParseRange(s string) (Range, error) {
	if s == "" {
		return Range1h, nil
	}
	if Range(s).duration() == 0 {
		return "", ErrInvalidRange
	}
	return Range(s), nil
}

func (r Range) duration() time.Duration {
	switch r {
	case Range1h:
		return time.Hour
	case Range6h:
		return 6 * time.Hour
	case Range24h:
		return 24 * time.Hour
	case Range7d:
		return 7 * 24 * time.Hour
	}
	return 0
}

// Buckets is the number of points in every series.
const Buckets = 180

// Series is a service's resource usage over a range, averaged into Buckets
// buckets. Bucket i covers [Start + i*Step, Start + (i+1)*Step). The last
// bucket is the one containing the current time if it has samples yet, and
// otherwise the last complete one. Nil entries mark buckets without samples.
type Series struct {
	Range Range
	Start time.Time
	Step  time.Duration
	// CPULimit is the CPU limit in cores; zero means unlimited.
	CPULimit float64
	// MemoryLimit is the memory limit in bytes; zero means unlimited.
	MemoryLimit int64
	// CPU is in percent of one core, Memory in bytes, and the others in
	// bytes per second.
	CPU, Memory         []*float64
	NetRx, NetTx        []*float64
	DiskRead, DiskWrite []*float64
}

// Query returns the resource usage of a service over r, with the limits of
// its running container.
func (c *Collector) Query(ctx context.Context, serviceID string, r Range) (Series, error) {
	d := r.duration()
	if d == 0 {
		return Series{}, ErrInvalidRange
	}
	// Fetch one extra bucket before the window, so that the window can shift
	// back a step when the in-progress bucket has no samples yet.
	start, step := window(c.now(), d)
	buckets, err := c.store.MetricBuckets(ctx, serviceID, start.Add(-step), step, Buckets+1)
	if err != nil {
		return Series{}, err
	}
	last := -1
	if n := len(buckets); n > 0 {
		last = buckets[n-1].Index
	}
	start, shift := settle(start, step, last)
	s := Series{
		Range: r, Start: start, Step: step,
		CPU: series(), Memory: series(), NetRx: series(), NetTx: series(),
		DiskRead: series(), DiskWrite: series(),
	}
	for _, b := range buckets {
		if i := b.Index - shift; i >= 0 && i < Buckets {
			s.CPU[i], s.Memory[i] = ptr(b.CPU), ptr(b.Memory)
			s.NetRx[i], s.NetTx[i] = ptr(b.NetRx), ptr(b.NetTx)
			s.DiskRead[i], s.DiskWrite[i] = ptr(b.DiskRead), ptr(b.DiskWrite)
		}
	}
	s.CPULimit, s.MemoryLimit, err = c.limits(ctx, serviceID)
	if err != nil {
		return Series{}, err
	}
	return s, nil
}

// window returns the start and bucket width of a range of length d ending
// with the bucket that contains now. Buckets are aligned to multiples of the
// width since the Unix epoch, so that they are stable between queries.
func window(now time.Time, d time.Duration) (start time.Time, step time.Duration) {
	step = d / Buckets
	width := int64(step / time.Second)
	end := (now.Unix()/width + 1) * width
	return time.Unix(end-width*Buckets, 0).UTC(), step
}

// settle picks the window for buckets fetched from one step before start,
// Buckets+1 of them, given the index of the last fetched bucket (-1 if none).
// If that is the in-progress bucket, the window starts at start; otherwise it
// ends at the last complete bucket. It returns the window's start and how far
// the fetched indexes are ahead of it.
func settle(start time.Time, step time.Duration, last int) (time.Time, int) {
	if last == Buckets {
		return start, 1
	}
	return start.Add(-step), 0
}

func series() []*float64 { return make([]*float64, Buckets) }

func ptr(v float64) *float64 { return &v }

// limits returns the configured CPU and memory limits of the service's
// running container, or zeros if nothing is running.
func (c *Collector) limits(ctx context.Context, serviceID string) (float64, int64, error) {
	containers, err := c.docker.List(ctx, map[string]string{serviceLabel: serviceID})
	if err != nil {
		return 0, 0, err
	}
	for _, ctr := range containers {
		if !ctr.Running {
			continue
		}
		info, err := c.docker.Inspect(ctx, ctr.ID)
		if docker.IsNotFound(err) {
			continue
		}
		if err != nil {
			return 0, 0, err
		}
		return info.CPULimit, info.MemoryLimit, nil
	}
	return 0, 0, nil
}

// HostSeries is the host's resource usage over a range, bucketed like
// [Series].
type HostSeries struct {
	Range Range
	Start time.Time
	Step  time.Duration
	// CPUs is the number of online CPUs.
	CPUs int
	// MemoryTotal and DiskTotal are the host's memory and the size of the
	// filesystem it reports, in bytes.
	MemoryTotal, DiskTotal int64
	// CPU is in percent of one core, Memory and DiskUsed in bytes, and the
	// others in bytes per second.
	CPU, Memory, DiskUsed []*float64
	NetRx, NetTx          []*float64
	DiskRead, DiskWrite   []*float64
}

// QueryHost returns the host's resource usage over r, with its current
// capacity.
func (c *Collector) QueryHost(ctx context.Context, r Range) (HostSeries, error) {
	d := r.duration()
	if d == 0 {
		return HostSeries{}, ErrInvalidRange
	}
	start, step := window(c.now(), d)
	buckets, err := c.store.HostBuckets(ctx, start.Add(-step), step, Buckets+1)
	if err != nil {
		return HostSeries{}, err
	}
	last := -1
	if n := len(buckets); n > 0 {
		last = buckets[n-1].Index
	}
	start, shift := settle(start, step, last)
	s := HostSeries{
		Range: r, Start: start, Step: step,
		CPU: series(), Memory: series(), DiskUsed: series(),
		NetRx: series(), NetTx: series(), DiskRead: series(), DiskWrite: series(),
	}
	for _, b := range buckets {
		if i := b.Index - shift; i >= 0 && i < Buckets {
			s.CPU[i], s.Memory[i], s.DiskUsed[i] = ptr(b.CPU), ptr(b.Memory), ptr(b.DiskUsed)
			s.NetRx[i], s.NetTx[i] = ptr(b.NetRx), ptr(b.NetTx)
			s.DiskRead[i], s.DiskWrite[i] = ptr(b.DiskRead), ptr(b.DiskWrite)
		}
	}
	if c.host != nil {
		h, err := c.host.Read(ctx)
		if err != nil {
			return HostSeries{}, err
		}
		s.CPUs, s.MemoryTotal, s.DiskTotal = h.CPUs, int64(h.MemoryTotal), int64(h.DiskTotal)
	}
	return s, nil
}
