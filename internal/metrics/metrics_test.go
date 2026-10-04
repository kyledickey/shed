package metrics

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/host"
	"github.com/kyledickey/shed/internal/store"
)

type fakeDocker struct {
	containers []docker.Container
	stats      map[string]docker.Stats
	limits     map[string]docker.Container
	listed     []map[string]string
}

func (f *fakeDocker) List(_ context.Context, labels map[string]string) ([]docker.Container, error) {
	f.listed = append(f.listed, labels)
	var out []docker.Container
	for _, c := range f.containers {
		match := true
		for k, v := range labels {
			if got, ok := c.Labels[k]; !ok || v != "" && got != v {
				match = false
			}
		}
		if match {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeDocker) Inspect(_ context.Context, id string) (docker.Container, error) {
	return f.limits[id], nil
}

func (f *fakeDocker) Stats(_ context.Context, id string) (docker.Stats, error) {
	st, ok := f.stats[id]
	if !ok {
		return docker.Stats{}, errors.New("no stats")
	}
	return st, nil
}

type fakeStore struct {
	inserted [][]store.MetricSample
	buckets  []store.MetricBucket
	query    struct {
		serviceID string
		from      time.Time
		step      time.Duration
		n         int
	}
	prunedBefore []time.Time
	hostInserted []store.HostSample
	hostBuckets  []store.HostBucket
	hostFrom     time.Time
}

func (f *fakeStore) InsertMetricSamples(_ context.Context, s []store.MetricSample) error {
	f.inserted = append(f.inserted, s)
	return nil
}

func (f *fakeStore) MetricBuckets(_ context.Context, serviceID string, from time.Time, step time.Duration, n int) ([]store.MetricBucket, error) {
	f.query.serviceID, f.query.from, f.query.step, f.query.n = serviceID, from, step, n
	return f.buckets, nil
}

func (f *fakeStore) InsertHostSample(_ context.Context, s store.HostSample) error {
	f.hostInserted = append(f.hostInserted, s)
	return nil
}

func (f *fakeStore) HostBuckets(_ context.Context, from time.Time, _ time.Duration, _ int) ([]store.HostBucket, error) {
	f.hostFrom = from
	return f.hostBuckets, nil
}

func (f *fakeStore) DeleteMetricSamplesBefore(_ context.Context, t time.Time) error {
	f.prunedBefore = append(f.prunedBefore, t)
	return nil
}

type fakeHost struct {
	stats host.Stats
	err   error
}

func (f *fakeHost) Read(context.Context) (host.Stats, error) { return f.stats, f.err }

var t0 = time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

func TestRates(t *testing.T) {
	prev := docker.Stats{
		Read: t0, CPUTotal: 1e9, SystemCPU: 100e9, OnlineCPUs: 4,
		MemoryUsage: 50, NetRx: 1000, NetTx: 2000, DiskRead: 0, DiskWrite: 100,
	}
	tests := []struct {
		name   string
		cur    docker.Stats
		want   usage
		wantOK bool
	}{
		{
			name: "system cpu",
			// 0.5s of CPU over 10s of host time on 4 CPUs: 20% of one core.
			cur: docker.Stats{
				Read: t0.Add(10 * time.Second), CPUTotal: 1.5e9, SystemCPU: 110e9, OnlineCPUs: 4,
				MemoryUsage: 80, NetRx: 2000, NetTx: 2500, DiskRead: 300, DiskWrite: 100,
			},
			want:   usage{cpu: 20, memory: 80, netRx: 100, netTx: 50, diskRead: 30},
			wantOK: true,
		},
		{
			name: "wall clock fallback",
			// 2s of CPU over 10s of wall time: 20% of one core.
			cur: docker.Stats{
				Read: t0.Add(10 * time.Second), CPUTotal: 3e9, SystemCPU: 100e9,
				NetRx: 1000, NetTx: 2000, DiskWrite: 100,
			},
			want:   usage{cpu: 20},
			wantOK: true,
		},
		{
			name: "counter reset",
			cur:  docker.Stats{Read: t0.Add(10 * time.Second), CPUTotal: 2e9, NetRx: 10},
		},
		{
			name: "no time passed",
			cur:  prev,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := rates(prev, tt.cur)
			if ok != tt.wantOK || !approx(got, tt.want) {
				t.Errorf("rates() = %+v, %v; want %+v, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func approx(a, b usage) bool {
	av := []float64{a.cpu, a.memory, a.netRx, a.netTx, a.diskRead, a.diskWrite}
	bv := []float64{b.cpu, b.memory, b.netRx, b.netTx, b.diskRead, b.diskWrite}
	for i := range av {
		if math.Abs(av[i]-bv[i]) > 1e-9 {
			return false
		}
	}
	return true
}

func TestCollect(t *testing.T) {
	label := func(svc string) map[string]string { return map[string]string{serviceLabel: svc} }
	d := &fakeDocker{
		containers: []docker.Container{
			{ID: "a1", Running: true, Labels: label("svcA")},
			{ID: "a2", Running: true, Labels: label("svcA")}, // overlapping deploy
			{ID: "b1", Running: true, Labels: label("svcB")},
			{ID: "stopped", Running: false, Labels: label("svcC")},
		},
		stats: map[string]docker.Stats{
			"a1":      {Read: t0, CPUTotal: 0, MemoryUsage: 10},
			"a2":      {Read: t0, CPUTotal: 0, MemoryUsage: 20},
			"b1":      {Read: t0, NetRx: 0},
			"stopped": {Read: t0},
		},
	}
	st := &fakeStore{}
	c := New(Config{Docker: d, Store: st, Log: slog.New(slog.DiscardHandler)})

	c.collect(context.Background(), t0)
	if len(st.inserted) != 0 {
		t.Fatalf("first collect inserted %v, want nothing", st.inserted)
	}
	if got := d.listed[0]; !reflect.DeepEqual(got, map[string]string{serviceLabel: ""}) {
		t.Errorf("listed with %v", got)
	}
	if want := []time.Time{t0.Add(-7 * 24 * time.Hour)}; !reflect.DeepEqual(st.prunedBefore, want) {
		t.Errorf("pruned before %v, want %v", st.prunedBefore, want)
	}

	t1 := t0.Add(10 * time.Second)
	d.stats["a1"] = docker.Stats{Read: t1, CPUTotal: 1e9, MemoryUsage: 15} // 10%
	d.stats["a2"] = docker.Stats{Read: t1, CPUTotal: 2e9, MemoryUsage: 25} // 20%
	d.stats["b1"] = docker.Stats{Read: t1, NetRx: 500}
	c.collect(context.Background(), t1.Add(300*time.Millisecond))

	want := []store.MetricSample{
		{ServiceID: "svcA", Time: t1, CPU: 30, Memory: 40},
		{ServiceID: "svcB", Time: t1, NetRx: 50},
	}
	if len(st.inserted) != 1 || !reflect.DeepEqual(st.inserted[0], want) {
		t.Errorf("inserted %+v, want %+v", st.inserted, want)
	}
	if len(st.prunedBefore) != 1 {
		t.Errorf("pruned %d times within an hour, want once", len(st.prunedBefore))
	}

	// A container that disappears is forgotten; when one with the same ID
	// comes back, it needs a fresh first sample.
	d.containers = d.containers[2:3]
	c.collect(context.Background(), t1.Add(10*time.Second))
	if _, ok := c.prev["a1"]; ok {
		t.Error("a1 still remembered after it disappeared")
	}
}

func TestParseRange(t *testing.T) {
	tests := []struct {
		in      string
		want    Range
		wantErr bool
	}{
		{"", Range1h, false},
		{"1h", Range1h, false},
		{"6h", Range6h, false},
		{"24h", Range24h, false},
		{"7d", Range7d, false},
		{"30d", "", true},
		{"1H", "", true},
	}
	for _, tt := range tests {
		got, err := ParseRange(tt.in)
		if got != tt.want || (err != nil) != tt.wantErr {
			t.Errorf("ParseRange(%q) = %q, %v", tt.in, got, err)
		}
		if err != nil && !errors.Is(err, ErrInvalidRange) {
			t.Errorf("ParseRange(%q) error = %v, want ErrInvalidRange", tt.in, err)
		}
	}
}

func TestWindow(t *testing.T) {
	tests := []struct {
		r         Range
		now       time.Time
		wantStart time.Time
		wantStep  time.Duration
	}{
		// 12:00:05 is in the bucket [12:00:00, 12:00:20), the last one.
		{Range1h, t0.Add(5 * time.Second), t0.Add(20*time.Second - time.Hour), 20 * time.Second},
		{Range1h, t0, t0.Add(20*time.Second - time.Hour), 20 * time.Second},
		{Range6h, t0.Add(119 * time.Second), t0.Add(2*time.Minute - 6*time.Hour), 2 * time.Minute},
		{Range24h, t0, t0.Add(480*time.Second - 24*time.Hour), 480 * time.Second},
		{Range7d, t0, time.Unix((t0.Unix()/3360+1)*3360-3360*180, 0).UTC(), 3360 * time.Second},
	}
	for _, tt := range tests {
		start, step := window(tt.now, tt.r.duration())
		if !start.Equal(tt.wantStart) || step != tt.wantStep {
			t.Errorf("window(%v, %s) = %v, %v; want %v, %v", tt.now, tt.r, start, step, tt.wantStart, tt.wantStep)
		}
		end := start.Add(step * Buckets)
		if !end.After(tt.now) || end.Add(-step).After(tt.now) {
			t.Errorf("window(%v, %s): last bucket [%v, %v) does not contain now", tt.now, tt.r, end.Add(-step), end)
		}
	}
}

func TestQuery(t *testing.T) {
	// The bucket containing now is [12:00:00, 12:00:20); the window ending
	// with it starts at 11:00:20.
	now := t0.Add(5 * time.Second)
	current := t0.Add(20*time.Second - time.Hour)
	full := store.MetricBucket{CPU: 1, Memory: 2, NetRx: 3, NetTx: 4, DiskRead: 5, DiskWrite: 6}
	at := func(i int, b store.MetricBucket) store.MetricBucket { b.Index = i; return b }

	tests := []struct {
		name      string
		buckets   []store.MetricBucket // indexed from one step before current
		wantStart time.Time
		wantFirst *float64 // CPU of entry 0
	}{
		{
			name:      "in-progress bucket has samples",
			buckets:   []store.MetricBucket{at(0, store.MetricBucket{CPU: 99}), at(1, full), at(Buckets, store.MetricBucket{CPU: 7})},
			wantStart: current,
			wantFirst: ptr(1),
		},
		{
			name:      "in-progress bucket empty",
			buckets:   []store.MetricBucket{at(0, full), at(Buckets-1, store.MetricBucket{CPU: 7})},
			wantStart: current.Add(-20 * time.Second),
			wantFirst: ptr(1),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &fakeDocker{
				containers: []docker.Container{
					{ID: "old", Running: false, Labels: map[string]string{serviceLabel: "svc"}},
					{ID: "new", Running: true, Labels: map[string]string{serviceLabel: "svc"}},
				},
				limits: map[string]docker.Container{"new": {CPULimit: 1.5, MemoryLimit: 512 << 20}},
			}
			st := &fakeStore{buckets: tt.buckets}
			c := New(Config{Docker: d, Store: st, Now: func() time.Time { return now }, Log: slog.New(slog.DiscardHandler)})

			s, err := c.Query(context.Background(), "svc", Range1h)
			if err != nil {
				t.Fatal(err)
			}
			if st.query.serviceID != "svc" || !st.query.from.Equal(current.Add(-20*time.Second)) ||
				st.query.step != 20*time.Second || st.query.n != Buckets+1 {
				t.Errorf("MetricBuckets called with %+v", st.query)
			}
			if s.Range != Range1h || !s.Start.Equal(tt.wantStart) || s.Step != 20*time.Second ||
				s.CPULimit != 1.5 || s.MemoryLimit != 512<<20 {
				t.Errorf("Query() = range %s start %v step %v limits %v %v; want start %v",
					s.Range, s.Start, s.Step, s.CPULimit, s.MemoryLimit, tt.wantStart)
			}
			for name, series := range map[string][]*float64{
				"cpu": s.CPU, "memory": s.Memory, "netRx": s.NetRx, "netTx": s.NetTx, "diskRead": s.DiskRead, "diskWrite": s.DiskWrite,
			} {
				if len(series) != Buckets {
					t.Errorf("len(%s) = %d, want %d", name, len(series), Buckets)
				}
			}
			last := Buckets - 1
			if s.CPU[0] == nil || *s.CPU[0] != *tt.wantFirst || s.DiskWrite[0] == nil || *s.DiskWrite[0] != 6 {
				t.Errorf("entry 0: cpu %v, diskWrite %v", s.CPU[0], s.DiskWrite[0])
			}
			if s.CPU[last] == nil || *s.CPU[last] != 7 || s.Memory[last] == nil || *s.Memory[last] != 0 {
				t.Errorf("last entry: cpu %v, memory %v; want 7, 0", s.CPU[last], s.Memory[last])
			}
			if s.CPU[1] != nil {
				t.Errorf("cpu[1] = %v, want nil", *s.CPU[1])
			}
		})
	}
}

func TestQueryErrorsAndNoContainer(t *testing.T) {
	c := New(Config{Docker: &fakeDocker{}, Store: &fakeStore{}, Now: func() time.Time { return t0 }, Log: slog.New(slog.DiscardHandler)})
	if _, err := c.Query(context.Background(), "svc", "2h"); !errors.Is(err, ErrInvalidRange) {
		t.Errorf("Query(2h) error = %v, want ErrInvalidRange", err)
	}
	s, err := c.Query(context.Background(), "svc", Range7d)
	if err != nil || s.CPULimit != 0 || s.MemoryLimit != 0 || len(s.CPU) != Buckets || s.CPU[Buckets-1] != nil {
		t.Errorf("Query() with no data = limits %v %v, %d points, %v; want zeros and %d nil points",
			s.CPULimit, s.MemoryLimit, len(s.CPU), err, Buckets)
	}
}

func TestHostRates(t *testing.T) {
	prev := host.Stats{
		CPUs: 4, CPUTotal: 1000, CPUIdle: 800, MemoryTotal: 1000, MemoryAvailable: 400,
		NetRx: 100, NetTx: 100, DiskRead: 0, DiskWrite: 0, DiskUsed: 5,
	}
	tests := []struct {
		name   string
		cur    host.Stats
		want   usage
		wantOK bool
	}{
		{
			// 100 of 400 ticks busy on 4 CPUs: one core's worth.
			name: "rates",
			cur: host.Stats{
				CPUs: 4, CPUTotal: 1400, CPUIdle: 1100, MemoryTotal: 1000, MemoryAvailable: 300,
				NetRx: 200, NetTx: 150, DiskRead: 1000, DiskWrite: 500,
			},
			want:   usage{cpu: 100, memory: 700, netRx: 10, netTx: 5, diskRead: 100, diskWrite: 50},
			wantOK: true,
		},
		{
			name: "idle going backwards is clamped",
			cur: host.Stats{
				CPUs: 4, CPUTotal: 1100, CPUIdle: 750, MemoryTotal: 1000,
				NetRx: 100, NetTx: 100,
			},
			want:   usage{cpu: 400, memory: 1000},
			wantOK: true,
		},
		{name: "no cpu time passed", cur: prev},
		{name: "counter reset", cur: host.Stats{CPUTotal: 2000, NetRx: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := hostRates(prev, tt.cur, 10*time.Second)
			if ok != tt.wantOK || !approx(got, tt.want) {
				t.Errorf("hostRates() = %+v, %v; want %+v, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestCollectHost(t *testing.T) {
	h := &fakeHost{stats: host.Stats{CPUs: 2, CPUTotal: 100, CPUIdle: 100, MemoryTotal: 100, DiskUsed: 7}}
	st := &fakeStore{}
	c := New(Config{Docker: &fakeDocker{}, Host: h, Store: st, Log: slog.New(slog.DiscardHandler)})

	c.collect(context.Background(), t0)
	if len(st.hostInserted) != 0 {
		t.Fatalf("first collect inserted %v, want nothing", st.hostInserted)
	}
	h.err = errors.New("boom")
	c.collect(context.Background(), t0.Add(10*time.Second))
	h.err = nil
	h.stats = host.Stats{CPUs: 2, CPUTotal: 300, CPUIdle: 200, MemoryTotal: 100, MemoryAvailable: 40, NetRx: 2000, DiskUsed: 9}
	c.collect(context.Background(), t0.Add(20*time.Second))

	want := []store.HostSample{{Time: t0.Add(20 * time.Second), CPU: 100, Memory: 60, DiskUsed: 9, NetRx: 100}}
	if !reflect.DeepEqual(st.hostInserted, want) {
		t.Errorf("inserted %+v, want %+v", st.hostInserted, want)
	}
}

func TestQueryHost(t *testing.T) {
	now := t0.Add(5 * time.Second)
	current := t0.Add(20*time.Second - time.Hour)
	st := &fakeStore{hostBuckets: []store.HostBucket{
		{Index: 0, CPU: 99},
		{Index: 1, CPU: 1, DiskUsed: 3},
		{Index: Buckets, CPU: 7},
	}}
	h := &fakeHost{stats: host.Stats{CPUs: 4, MemoryTotal: 1 << 30, DiskTotal: 1 << 40}}
	c := New(Config{Docker: &fakeDocker{}, Host: h, Store: st, Now: func() time.Time { return now }, Log: slog.New(slog.DiscardHandler)})

	s, err := c.QueryHost(context.Background(), Range1h)
	if err != nil {
		t.Fatal(err)
	}
	if !st.hostFrom.Equal(current.Add(-20 * time.Second)) {
		t.Errorf("HostBuckets from %v", st.hostFrom)
	}
	if !s.Start.Equal(current) || s.CPUs != 4 || s.MemoryTotal != 1<<30 || s.DiskTotal != 1<<40 {
		t.Errorf("QueryHost() = start %v, %d cpus, %d memory, %d disk", s.Start, s.CPUs, s.MemoryTotal, s.DiskTotal)
	}
	if len(s.DiskUsed) != Buckets || s.CPU[0] == nil || *s.CPU[0] != 1 || *s.DiskUsed[0] != 3 ||
		s.CPU[Buckets-1] == nil || *s.CPU[Buckets-1] != 7 || s.CPU[1] != nil {
		t.Errorf("QueryHost() series: cpu[0] %v, cpu[1] %v, cpu[last] %v", s.CPU[0], s.CPU[1], s.CPU[Buckets-1])
	}
	if _, err := c.QueryHost(context.Background(), "2h"); !errors.Is(err, ErrInvalidRange) {
		t.Errorf("QueryHost(2h) error = %v, want ErrInvalidRange", err)
	}
}
