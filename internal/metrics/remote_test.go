package metrics

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/host"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/victoria"
)

// memStore keeps samples and buckets them like the SQLite queries do: the
// average of the samples at whole seconds in [from + i*step, from + (i+1)*step).
type memStore struct {
	fakeStore
	services []store.MetricSample
	hosts    []store.HostSample
}

func (m *memStore) InsertMetricSamples(_ context.Context, s []store.MetricSample) error {
	m.services = append(m.services, s...)
	return nil
}

func (m *memStore) InsertHostSample(_ context.Context, s store.HostSample) error {
	m.hosts = append(m.hosts, s)
	return nil
}

func bucketIndex(ts, from time.Time, step time.Duration, n int) (int, bool) {
	if ts.Before(from) {
		return 0, false
	}
	i := int(ts.Sub(from) / step)
	return i, i < n
}

func (m *memStore) MetricBuckets(_ context.Context, id string, from time.Time, step time.Duration, n int) ([]store.MetricBucket, error) {
	sums := map[int]*store.MetricBucket{}
	counts := map[int]float64{}
	for _, s := range m.services {
		i, ok := bucketIndex(s.Time, from, step, n)
		if s.ServiceID != id || !ok {
			continue
		}
		b := sums[i]
		if b == nil {
			b = &store.MetricBucket{Index: i}
			sums[i] = b
		}
		b.CPU += s.CPU
		b.Memory += float64(s.Memory)
		b.NetRx += s.NetRx
		b.NetTx += s.NetTx
		b.DiskRead += s.DiskRead
		b.DiskWrite += s.DiskWrite
		counts[i]++
	}
	var out []store.MetricBucket
	for i := range n {
		if b := sums[i]; b != nil {
			c := counts[i]
			out = append(out, store.MetricBucket{Index: i, CPU: b.CPU / c, Memory: b.Memory / c,
				NetRx: b.NetRx / c, NetTx: b.NetTx / c, DiskRead: b.DiskRead / c, DiskWrite: b.DiskWrite / c})
		}
	}
	return out, nil
}

func (m *memStore) HostBuckets(_ context.Context, from time.Time, step time.Duration, n int) ([]store.HostBucket, error) {
	sums := map[int]*store.HostBucket{}
	counts := map[int]float64{}
	for _, s := range m.hosts {
		i, ok := bucketIndex(s.Time, from, step, n)
		if !ok {
			continue
		}
		b := sums[i]
		if b == nil {
			b = &store.HostBucket{Index: i}
			sums[i] = b
		}
		b.CPU += s.CPU
		b.Memory += float64(s.Memory)
		b.DiskUsed += float64(s.DiskUsed)
		b.NetRx += s.NetRx
		b.NetTx += s.NetTx
		b.DiskRead += s.DiskRead
		b.DiskWrite += s.DiskWrite
		counts[i]++
	}
	var out []store.HostBucket
	for i := range n {
		if b := sums[i]; b != nil {
			c := counts[i]
			out = append(out, store.HostBucket{Index: i, CPU: b.CPU / c, Memory: b.Memory / c, DiskUsed: b.DiskUsed / c,
				NetRx: b.NetRx / c, NetTx: b.NetTx / c, DiskRead: b.DiskRead / c, DiskWrite: b.DiskWrite / c})
		}
	}
	return out, nil
}

type promSample struct {
	name, labels string
	value        float64
	at           time.Time
}

// fakeRemote stores imported lines and answers the collector's range
// queries the way VictoriaMetrics evaluates them: avg_over_time over the
// window (t-W, t] at every step t, then avg across series.
type fakeRemote struct {
	mu        sync.Mutex
	samples   []promSample
	importErr error
	queryErr  error
	imports   int
	queries   []string
}

var promLine = regexp.MustCompile(`^(\w+)(\{.*\})? (\S+) (\d+)$`)

func (f *fakeRemote) ImportPrometheus(_ context.Context, lines []string) error {
	f.imports++
	if f.importErr != nil {
		return f.importErr
	}
	for _, l := range lines {
		m := promLine.FindStringSubmatch(l)
		if m == nil {
			return errors.New("bad line " + l)
		}
		v, _ := strconv.ParseFloat(m[3], 64)
		ms, _ := strconv.ParseInt(m[4], 10, 64)
		f.samples = append(f.samples, promSample{m[1], m[2], v, time.UnixMilli(ms).UTC()})
	}
	return nil
}

var rangeQuery = regexp.MustCompile(`^avg\(avg_over_time\((\w+)\{shed_service_id="(\w+)"\}\[(\d+)s\] offset 1ms\)\)$`)

func (f *fakeRemote) QueryRange(_ context.Context, q string, start, end time.Time, step time.Duration) (*victoria.Result, error) {
	f.mu.Lock()
	f.queries = append(f.queries, q)
	f.mu.Unlock()
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	m := rangeQuery.FindStringSubmatch(q)
	if m == nil {
		return nil, errors.New("unexpected query " + q)
	}
	w, _ := strconv.Atoi(m[3])
	window := time.Duration(w) * time.Second
	res := &victoria.Result{Type: "matrix", Series: []victoria.Series{}}
	var series victoria.Series
	for t := start; !t.After(end); t = t.Add(step) {
		at := t.Add(-time.Millisecond)
		var sum, n float64
		for _, s := range f.samples {
			if s.name == m[1] && strings.Contains(s.labels, `shed_service_id="`+m[2]+`"`) && s.at.After(at.Add(-window)) && !s.at.After(at) {
				sum += s.value
				n++
			}
		}
		if n > 0 {
			series.Values = append(series.Values, victoria.Sample{Time: t, Value: sum / n})
		}
	}
	if len(series.Values) > 0 {
		res.Series = append(res.Series, series)
	}
	return res, nil
}

func (f *fakeRemote) names() map[string]bool {
	out := map[string]bool{}
	for _, s := range f.samples {
		out[s.name] = true
	}
	return out
}

// tickWorld feeds ticks 10 s apart, across several 20 s buckets, to a
// collector.
type tickWorld struct {
	d *fakeDocker
	h *fakeHost
}

func newWorld() *tickWorld {
	return &tickWorld{
		d: &fakeDocker{
			containers: []docker.Container{{ID: "a1", Running: true, Labels: map[string]string{serviceLabel: "svcA"}}},
			limits:     map[string]docker.Container{"a1": {CPULimit: 1.5, MemoryLimit: 512 << 20}},
			stats:      map[string]docker.Stats{},
		},
		h: &fakeHost{},
	}
}

func (w *tickWorld) tick(c *Collector, i int, at time.Time) {
	secs := float64(i * 10)
	w.d.stats["a1"] = docker.Stats{Read: at, CPUTotal: uint64(float64(i*i) * 1e9), MemoryUsage: uint64(100 + i*10), NetRx: uint64(i * 1000)}
	w.h.stats = host.Stats{
		CPUs: 2, CPUTotal: uint64(secs * 200), CPUIdle: uint64(secs * 150),
		MemoryTotal: 8 << 30, MemoryAvailable: uint64(4<<30 - i*1000),
		NetRx: uint64(i * 500), DiskTotal: 100 << 30, DiskUsed: uint64(20<<30 + i*4096),
	}
	c.collect(context.Background(), at)
}

func TestSamplerSinkSwitch(t *testing.T) {
	remote := &fakeRemote{}
	st := &memStore{}
	w := newWorld()
	c := New(Config{Docker: w.d, Host: w.h, Store: st, Remote: remote, Log: slog.New(slog.DiscardHandler)})

	// Remote off: SQLite only, nothing pushed.
	w.tick(c, 0, t0)
	w.tick(c, 1, t0.Add(10*time.Second))
	if remote.imports != 0 || len(st.services) != 1 || len(st.hosts) != 1 {
		t.Fatalf("off: %d imports, %d service rows, %d host rows; want 0, 1, 1", remote.imports, len(st.services), len(st.hosts))
	}

	// Remote on: pushed, and SQLite is left alone.
	c.SetRemote(true)
	w.tick(c, 2, t0.Add(20*time.Second))
	if remote.imports != 1 || len(st.services) != 1 || len(st.hosts) != 1 {
		t.Fatalf("on: %d imports, %d service rows, %d host rows; want 1, 1, 1", remote.imports, len(st.services), len(st.hosts))
	}
	for _, name := range []string{
		mCPU, mMemory, mNetRx, mNetTx, mDiskRead, mDiskWrite, mCPULimit, mMemLimit,
		hCPU, hMemory, hDiskUsed, hNetRx, hNetTx, hDiskRead, hDiskWrite, hMemTotal, hDiskTotal,
	} {
		if !remote.names()[name] {
			t.Errorf("%s was not pushed", name)
		}
	}
	for _, s := range remote.samples {
		switch s.name {
		case mCPULimit:
			if s.value != 150 {
				t.Errorf("cpu limit %v, want 150 percent", s.value)
			}
			if want := `{shed_project_id="prj",shed_project_name="acme",shed_service_id="svcA",shed_service_name="web",shed_service_kind="app",shed_source="sampler"}`; s.labels != want {
				t.Errorf("labels %s, want %s", s.labels, want)
			}
		case mMemLimit:
			if s.value != 512<<20 {
				t.Errorf("memory limit %v", s.value)
			}
		case hMemTotal:
			if s.value != 8<<30 {
				t.Errorf("host memory total %v", s.value)
			}
		}
		if !s.at.Equal(t0.Add(20 * time.Second)) {
			t.Errorf("%s stamped %v, want the tick's time", s.name, s.at)
		}
	}

	// A refused push falls back to SQLite for that tick.
	remote.importErr = errors.New("down")
	w.tick(c, 3, t0.Add(30*time.Second))
	if len(st.services) != 2 || len(st.hosts) != 2 {
		t.Errorf("refused push: %d service rows, %d host rows; want 2, 2", len(st.services), len(st.hosts))
	}

	// Turned off again: SQLite.
	c.SetRemote(false)
	remote.importErr = nil
	w.tick(c, 4, t0.Add(40*time.Second))
	if remote.imports != 3-1 || len(st.services) != 3 {
		t.Errorf("off again: %d imports, %d service rows", remote.imports, len(st.services))
	}
}

func TestSetRemoteWithoutRemote(t *testing.T) {
	c := New(Config{Docker: newWorld().d, Store: &memStore{}, Log: slog.New(slog.DiscardHandler)})
	c.SetRemote(true)
	if c.remoteOn.Load() {
		t.Error("remote on without a Remote")
	}
}

func rangesEqual(a, b []*float64) bool {
	return reflect.DeepEqual(a, b)
}

// TestRemoteBucketsMatchSQLite feeds the same ticks to the remote store and
// to SQLite and checks that both query paths bucket them identically.
func TestRemoteBucketsMatchSQLite(t *testing.T) {
	sqlite := &memStore{}
	remote := &fakeRemote{}
	remoteStore := &memStore{}
	wa, wb := newWorld(), newWorld()
	a := New(Config{Docker: wa.d, Host: wa.h, Store: sqlite, Log: slog.New(slog.DiscardHandler)})
	b := New(Config{Docker: wb.d, Host: wb.h, Store: remoteStore, Remote: remote, Log: slog.New(slog.DiscardHandler)})
	b.SetRemote(true)

	// 17 ticks, 10 s apart, from 12:00:00; some buckets hold two samples.
	for i := range 17 {
		at := t0.Add(time.Duration(i) * 10 * time.Second)
		wa.tick(a, i, at)
		wb.tick(b, i, at)
	}
	if len(sqlite.services) == 0 || len(remoteStore.services) != 0 {
		t.Fatalf("setup: sqlite has %d rows, remote path's sqlite %d", len(sqlite.services), len(remoteStore.services))
	}
	now := t0.Add(170*time.Second + 3*time.Second)
	a.now, b.now = func() time.Time { return now }, func() time.Time { return now }

	for _, r := range []Range{Range1h, Range6h} {
		want, err := a.Query(context.Background(), "svcA", r)
		if err != nil {
			t.Fatal(err)
		}
		got, err := b.Query(context.Background(), "svcA", r)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Start.Equal(want.Start) || got.Step != want.Step {
			t.Errorf("%s: window %v/%v, want %v/%v", r, got.Start, got.Step, want.Start, want.Step)
		}
		pairs := [][2][]*float64{{got.CPU, want.CPU}, {got.Memory, want.Memory}, {got.NetRx, want.NetRx}, {got.NetTx, want.NetTx}, {got.DiskRead, want.DiskRead}, {got.DiskWrite, want.DiskWrite}}
		for i, p := range pairs {
			if !rangesEqual(p[0], p[1]) {
				t.Errorf("%s: series %d differs from the SQLite path:\n got %v\nwant %v", r, i, deref(p[0]), deref(p[1]))
			}
		}
		if n := countPoints(got.CPU); n == 0 {
			t.Errorf("%s: no points at all", r)
		}

		wantH, err := a.QueryHost(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		gotH, err := b.QueryHost(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		if !gotH.Start.Equal(wantH.Start) {
			t.Errorf("%s host: start %v, want %v", r, gotH.Start, wantH.Start)
		}
		for i, p := range [][2][]*float64{{gotH.CPU, wantH.CPU}, {gotH.Memory, wantH.Memory}, {gotH.DiskUsed, wantH.DiskUsed}, {gotH.NetRx, wantH.NetRx}} {
			if !rangesEqual(p[0], p[1]) {
				t.Errorf("%s host: series %d differs:\n got %v\nwant %v", r, i, deref(p[0]), deref(p[1]))
			}
		}
	}
}

func countPoints(s []*float64) int {
	n := 0
	for _, p := range s {
		if p != nil {
			n++
		}
	}
	return n
}

func deref(s []*float64) []float64 {
	var out []float64
	for _, p := range s {
		if p == nil {
			out = append(out, math.NaN())
		} else {
			out = append(out, *p)
		}
	}
	return out
}

func TestQueryFallsBackToSQLite(t *testing.T) {
	now := t0.Add(5 * time.Second)
	sqlite := &memStore{services: []store.MetricSample{{ServiceID: "svcA", Time: t0.Add(-time.Minute), CPU: 42}}}
	remote := &fakeRemote{}
	w := newWorld()
	c := New(Config{Docker: w.d, Store: sqlite, Remote: remote, Now: func() time.Time { return now }, Log: slog.New(slog.DiscardHandler)})
	c.SetRemote(true)

	// The remote has nothing: older samples in SQLite still show.
	s, err := c.Query(context.Background(), "svcA", Range1h)
	if err != nil {
		t.Fatal(err)
	}
	if got := countPoints(s.CPU); got != 1 {
		t.Errorf("empty remote: %d points, want the SQLite sample", got)
	}

	// The remote fails: same.
	remote.queryErr = errors.New("down")
	s, err = c.Query(context.Background(), "svcA", Range1h)
	if err != nil {
		t.Fatal(err)
	}
	if got := countPoints(s.CPU); got != 1 {
		t.Errorf("failing remote: %d points, want the SQLite sample", got)
	}

	// Off: the remote is not asked.
	remote.queries = nil
	c.SetRemote(false)
	if _, err := c.Query(context.Background(), "svcA", Range1h); err != nil {
		t.Fatal(err)
	}
	if len(remote.queries) != 0 {
		t.Errorf("queried the remote while off: %v", remote.queries)
	}
}

func TestPushTickSkipsNonFiniteValues(t *testing.T) {
	remote := &fakeRemote{}
	c := New(Config{Docker: newWorld().d, Store: &memStore{}, Remote: remote, Log: slog.New(slog.DiscardHandler)})
	c.SetRemote(true)
	svc := serviceTick{MetricSample: store.MetricSample{ServiceID: "svcA", CPU: math.NaN(), NetRx: math.Inf(1), Memory: 5}, labels: `{shed_service_id="svcA"}`}
	if !c.pushTick(context.Background(), t0, []serviceTick{svc}, nil) {
		t.Fatal("push failed")
	}
	if remote.names()[mCPU] || remote.names()[mNetRx] || !remote.names()[mMemory] {
		t.Errorf("pushed %v", remote.names())
	}
}

func TestLabelSetEscapes(t *testing.T) {
	got := labelSet{{"a", `x"y\z`}, {"b", ""}, {"c", "ok"}}.String()
	if want := `{a="x\"y\\z",c="ok"}`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
