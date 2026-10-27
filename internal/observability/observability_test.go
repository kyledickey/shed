package observability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/grafana"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/victoria"
)

// trail records the calls of every fake, in order.
type trail struct {
	mu    sync.Mutex
	calls []string
}

func (t *trail) add(format string, args ...any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls = append(t.calls, fmt.Sprintf(format, args...))
}

func (t *trail) take() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := t.calls
	t.calls = nil
	return out
}

type fakeStore struct {
	mu             sync.Mutex
	rows           map[string]string
	cursorsDeleted bool
	// dashboardsDeleted is set by DeleteGrafanaDashboards.
	dashboardsDeleted bool
}

func (s *fakeStore) DeleteGrafanaDashboards(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dashboardsDeleted = true
	return nil
}

func (s *fakeStore) Setting(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.rows[key]
	if !ok {
		return "", store.ErrNotFound
	}
	return v, nil
}

func (s *fakeStore) SetSetting(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[key] = value
	return nil
}

func (s *fakeStore) DeleteSetting(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, key)
	return nil
}

func (s *fakeStore) DeleteLogCursors(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cursorsDeleted = true
	return nil
}

func (s *fakeStore) AddSetting(_ context.Context, key, value string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[key]; ok {
		return false, nil
	}
	s.rows[key] = value
	return true, nil
}

// fakeDocker keeps containers and volumes in memory.
type fakeDocker struct {
	t       *trail
	mu      sync.Mutex
	ctrs    map[string]*docker.Container // by ID
	volumes map[string]bool
	images  map[string]bool
	seq     int
}

func newFakeDocker(t *trail) *fakeDocker {
	return &fakeDocker{t: t, ctrs: map[string]*docker.Container{}, volumes: map[string]bool{}, images: map[string]bool{}}
}

func (d *fakeDocker) List(_ context.Context, labels map[string]string) ([]docker.Container, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []docker.Container
	for _, c := range d.ctrs {
		match := true
		for k, v := range labels {
			if got, ok := c.Labels[k]; !ok || v != "" && got != v {
				match = false
			}
		}
		if match {
			out = append(out, *c)
		}
	}
	return out, nil
}

func (d *fakeDocker) ResolveImage(_ context.Context, ref string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.images[ref] {
		return "", errors.New("no such image")
	}
	return "sha256:x", nil
}

func (d *fakeDocker) PullImage(_ context.Context, ref string, _ io.Writer) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.images[ref] = true
	return nil
}

func (d *fakeDocker) EnsureVolume(_ context.Context, name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.volumes[name] = true
	return nil
}

func (d *fakeDocker) RemoveVolume(_ context.Context, name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.t.add("remove volume %s", name)
	delete(d.volumes, name)
	return nil
}

func (d *fakeDocker) Run(_ context.Context, spec docker.RunSpec) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seq++
	id := fmt.Sprint("c", d.seq)
	d.ctrs[id] = &docker.Container{
		ID: id, Name: spec.Name, State: "running", Running: true, Labels: spec.Labels,
		IPs: map[string]string{spec.Network: fmt.Sprintf("10.9.0.%d", d.seq)},
	}
	d.t.add("run %s", spec.Labels[LabelComponent])
	return id, nil
}

func (d *fakeDocker) Start(_ context.Context, id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	c := d.ctrs[id]
	c.State, c.Running = "running", true
	d.t.add("start %s", c.Labels[LabelComponent])
	return nil
}

func (d *fakeDocker) Stop(_ context.Context, id string, _ time.Duration) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	c := d.ctrs[id]
	c.State, c.Running = "exited", false
	d.t.add("stop %s", c.Labels[LabelComponent])
	return nil
}

func (d *fakeDocker) Restart(_ context.Context, id string, _ time.Duration) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.t.add("restart %s", d.ctrs[id].Labels[LabelComponent])
	return nil
}

func (d *fakeDocker) Remove(_ context.Context, id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.t.add("remove %s", d.ctrs[id].Labels[LabelComponent])
	delete(d.ctrs, id)
	return nil
}

func (d *fakeDocker) Exec(context.Context, string, []string, io.Reader, io.Writer, io.Writer) error {
	return nil
}

func (d *fakeDocker) Stats(context.Context, string) (docker.Stats, error) {
	return docker.Stats{Read: time.Now(), MemoryUsage: 1 << 20}, nil
}

// byComponent returns the container of c, or nil.
func (d *fakeDocker) byComponent(c Component) *docker.Container {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, ctr := range d.ctrs {
		if ctr.Labels[LabelComponent] == string(c) {
			return ctr
		}
	}
	return nil
}

type fakeVictoria struct {
	t    *trail
	name Component
}

func (v fakeVictoria) Health(context.Context) error {
	v.t.add("health %s", v.name)
	return nil
}

func (v fakeVictoria) DataSize(context.Context) (int64, error) { return 1234, nil }

func (v fakeVictoria) Query(_ context.Context, q string, _ time.Time) (*victoria.Result, error) {
	if q == ingestQuery {
		return &victoria.Result{Series: []victoria.Series{
			{Labels: map[string]string{"signal": "traces"}, Values: []victoria.Sample{{Value: 3}}},
		}}, nil
	}
	return &victoria.Result{}, nil
}

type fakeGrafana struct {
	t   *trail
	dir string
}

func (g fakeGrafana) Health(context.Context) (grafana.Health, error) {
	g.t.add("health grafana")
	return grafana.Health{Database: "ok"}, nil
}

func (g fakeGrafana) EnsureFolder(_ context.Context, f grafana.Folder) error {
	g.t.add("folder %s", f.UID)
	return nil
}

func (g fakeGrafana) ReloadAlertRules(context.Context) error {
	if _, err := os.Stat(filepath.Join(g.dir, provisioning, "alerting", "shed.yaml")); err != nil {
		return fmt.Errorf("reload before the rules file exists: %w", err)
	}
	g.t.add("reload alerting")
	return nil
}

func (g fakeGrafana) FiringAlerts(context.Context) ([]grafana.Alert, error) {
	return []grafana.Alert{{Labels: map[string]string{"alertname": "Service restarting", "shed_service_id": "svc1"}}}, nil
}

type harness struct {
	t      *trail
	docker *fakeDocker
	store  *fakeStore
	m      *Manager
	dir    string
}

func newHarness(t *testing.T, opts ...func(*Config)) *harness {
	t.Helper()
	tr := &trail{}
	h := &harness{t: tr, docker: newFakeDocker(tr), store: &fakeStore{rows: map[string]string{}}, dir: filepath.Join(t.TempDir(), "telemetry")}
	cfg := Config{
		Store:   h.store,
		Docker:  h.docker,
		Metrics: fakeVictoria{tr, Metrics},
		Logs:    fakeVictoria{tr, Logs},
		Traces:  fakeVictoria{tr, Traces},
		Grafana: func(pw string) GrafanaAPI {
			if pw == "" {
				t.Error("Grafana client without the admin password")
			}
			return fakeGrafana{tr, h.dir}
		},
		Addrs:        Addrs{Metrics: "127.0.0.1:8428", Logs: "127.0.0.1:9428", Traces: "127.0.0.1:10428", Grafana: "127.0.0.1:3001"},
		Dir:          h.dir,
		GrafanaURL:   "https://shed.example.com/grafana/",
		OTLPEndpoint: "http://localhost:4318",
		Log:          slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	h.m = New(cfg)
	h.m.poll = time.Millisecond
	return h
}

// set stores settings s and reconciles.
func (h *harness) set(t *testing.T, s Settings) {
	t.Helper()
	if _, err := h.m.SetSettings(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if err := h.m.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func enabled() Settings {
	s := DefaultSettings()
	s.Enabled = true
	return s
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Settings)
		want   string
	}{
		{"defaults", func(*Settings) {}, ""},
		{"enabled", func(s *Settings) { s.Enabled = true }, ""},
		{"longest retention", func(s *Settings) { s.LogsRetentionDays = MaxRetentionDays }, ""},
		{"no metrics retention", func(s *Settings) { s.MetricsRetentionDays = 0 }, "metrics retention"},
		{"logs retention too long", func(s *Settings) { s.LogsRetentionDays = MaxRetentionDays + 1 }, "logs retention"},
		{"negative traces retention", func(s *Settings) { s.TracesRetentionDays = -1 }, "traces retention"},
		{"no disk", func(s *Settings) { s.DiskLimitGB = 0 }, "disk limit"},
		{"no spans", func(s *Settings) { s.DefaultCaps.SpansPerSecond = 0 }, "cap"},
		{"no logs", func(s *Settings) { s.DefaultCaps.LogsPerSecond = 0 }, "cap"},
		{"no series", func(s *Settings) { s.DefaultCaps.Series = 0 }, "cap"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := DefaultSettings()
			tt.change(&s)
			err := s.Validate()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() = %v", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() = %v, want ErrInvalid about %q", err, tt.want)
			}
		})
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if got, _ := h.m.Settings(ctx); got != DefaultSettings() {
		t.Fatalf("Settings() = %+v before any save, want the defaults", got)
	}
	s := enabled()
	s.LogsRetentionDays = 7
	if _, err := h.m.SetSettings(ctx, s); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.m.Settings(ctx); got != s {
		t.Fatalf("Settings() = %+v, want %+v", got, s)
	}
	bad := s
	bad.DiskLimitGB = 0
	if _, err := h.m.SetSettings(ctx, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("SetSettings(invalid) = %v", err)
	}
	if got, _ := h.m.Settings(ctx); got != s {
		t.Fatalf("invalid settings were stored: %+v", got)
	}
}

func TestEnableSequence(t *testing.T) {
	h := newHarness(t)
	h.set(t, enabled())
	want := []string{
		"run metrics", "run logs", "run traces",
		"health metrics", "health logs", "health traces",
		"run grafana", "health grafana",
		"folder shed", "reload alerting",
	}
	if got := h.t.take(); !slices.Equal(got, want) {
		t.Fatalf("enable did\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, f := range []string{"scrape/scrape.yaml", provisioning + "/datasources/shed.yaml", provisioning + "/alerting/shed.yaml"} {
		info, err := os.Stat(filepath.Join(h.dir, f))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o004 == 0 {
			t.Errorf("%s is %v, not readable by Grafana's user", f, info.Mode())
		}
	}
	pw, err := h.store.Setting(context.Background(), passwordKey)
	if err != nil || pw == "" {
		t.Fatalf("admin password %q, %v", pw, err)
	}
	h.set(t, enabled())
	if again, _ := h.store.Setting(context.Background(), passwordKey); again != pw {
		t.Error("admin password changed on the second reconciliation")
	}
}

func TestReconcile(t *testing.T) {
	h := newHarness(t)
	h.set(t, enabled())
	h.t.take()

	// Containers that match their spec are left alone.
	h.set(t, enabled())
	for _, call := range h.t.take() {
		if !strings.HasPrefix(call, "health") && !strings.HasPrefix(call, "folder") && call != "reload alerting" {
			t.Errorf("reconciling an unchanged stack did %q", call)
		}
	}

	// Changing a retention recreates only that store, on the same volume.
	s := enabled()
	s.LogsRetentionDays = 30
	h.set(t, s)
	var changed []string
	for _, call := range h.t.take() {
		if strings.HasPrefix(call, "run") || strings.HasPrefix(call, "remove") {
			changed = append(changed, call)
		}
	}
	if want := []string{"remove logs", "run logs"}; !slices.Equal(changed, want) {
		t.Errorf("changing logs retention did %v, want %v", changed, want)
	}
	if !h.docker.volumes[volumeName(Logs)] {
		t.Error("logs volume is gone")
	}

	// Disabling stops the containers, Grafana first, and keeps them.
	off := s
	off.Enabled = false
	h.set(t, off)
	if got, want := h.t.take(), []string{"stop grafana", "stop traces", "stop logs", "stop metrics"}; !slices.Equal(got, want) {
		t.Errorf("disable did %v, want %v", got, want)
	}
	for _, c := range order {
		if ctr := h.docker.byComponent(c); ctr == nil || ctr.Running {
			t.Errorf("%s after disable: %+v, want a stopped container", c, ctr)
		}
	}
	h.set(t, off)
	if got := h.t.take(); len(got) > 0 {
		t.Errorf("disabling again did %v", got)
	}

	// Enabling again starts the same containers.
	h.set(t, s)
	var starts []string
	for _, call := range h.t.take() {
		if !strings.HasPrefix(call, "health") && !strings.HasPrefix(call, "folder") && call != "reload alerting" {
			starts = append(starts, call)
		}
	}
	if want := []string{"start metrics", "start logs", "start traces", "start grafana"}; !slices.Equal(starts, want) {
		t.Errorf("enabling again did %v, want %v", starts, want)
	}
}

func TestDeleteData(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.set(t, enabled())
	if err := h.m.DeleteData(ctx); !errors.Is(err, ErrEnabled) {
		t.Fatalf("DeleteData while enabled = %v, want ErrEnabled", err)
	}
	if h.docker.byComponent(Metrics) == nil {
		t.Fatal("DeleteData while enabled removed a container")
	}
	h.set(t, DefaultSettings())
	h.t.take()
	h.store.rows[auditCursorKey] = "42"
	if err := h.m.DeleteData(ctx); err != nil {
		t.Fatal(err)
	}
	for _, c := range order {
		if h.docker.byComponent(c) != nil || h.docker.volumes[volumeName(c)] {
			t.Errorf("%s survived DeleteData", c)
		}
	}
	if _, err := os.Stat(h.dir); !os.IsNotExist(err) {
		t.Errorf("telemetry directory: %v, want it gone", err)
	}
	if !h.store.cursorsDeleted {
		t.Error("log cursors survived DeleteData")
	}
	if _, ok := h.store.rows[auditCursorKey]; ok {
		t.Error("audit mirror cursor survived DeleteData")
	}
	if !h.store.dashboardsDeleted {
		t.Error("DeleteData kept the dashboard records")
	}
	st, err := h.m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Components) != 0 {
		t.Errorf("components after DeleteData: %+v", st.Components)
	}
}

func TestRestart(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.m.Restart(ctx, Grafana); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Restart while disabled = %v, want ErrDisabled", err)
	}
	h.set(t, enabled())
	h.t.take()
	if err := h.m.Restart(ctx, "postgres"); !errors.Is(err, ErrUnknownComponent) {
		t.Fatalf("Restart(postgres) = %v", err)
	}
	if err := h.m.Restart(ctx, Grafana); err != nil {
		t.Fatal(err)
	}
	if got := h.t.take(); !slices.Equal(got, []string{"restart grafana"}) {
		t.Errorf("Restart did %v", got)
	}
}

func TestWorkers(t *testing.T) {
	h := newHarness(t)
	var mu sync.Mutex
	var events []string
	note := func(e string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	}
	started := make(chan struct{}, 1)
	h.m.AddWorker(func(ctx context.Context) {
		note("worker start")
		started <- struct{}{}
		<-ctx.Done()
		note("worker stop")
	})
	h.m.OnEnabled(func(on bool) { note(fmt.Sprint("enabled ", on)) })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.m.Run(ctx)
		close(done)
	}()
	if _, err := h.m.SetSettings(ctx, enabled()); err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := h.m.SetSettings(ctx, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return slices.Contains(events, "worker stop")
	})
	if _, err := h.m.SetSettings(ctx, enabled()); err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()
	<-done

	want := []string{"worker start", "enabled true", "enabled false", "worker stop", "worker start", "enabled true", "worker stop"}
	mu.Lock()
	defer mu.Unlock()
	// The hook and the worker's first line race; compare them as pairs.
	norm := func(es []string) []string {
		out := slices.Clone(es)
		for i := 0; i+1 < len(out); i++ {
			if out[i] == "enabled true" && out[i+1] == "worker start" {
				out[i], out[i+1] = out[i+1], out[i]
			}
		}
		return out
	}
	if got := norm(events); !slices.Equal(got, want) {
		t.Errorf("events %v, want %v", got, want)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestStatus(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	st, err := h.m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Components) != 0 || st.Settings.Enabled {
		t.Fatalf("Status before enabling: %+v", st)
	}
	h.set(t, enabled())
	if _, err := h.m.Status(ctx); err != nil {
		t.Fatal(err)
	}
	st, err = h.m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Components) != 4 {
		t.Fatalf("components %+v", st.Components)
	}
	for _, c := range st.Components {
		if c.State != Running || c.Version == "" || c.MemoryBytes == nil || c.MemoryLimit == 0 {
			t.Errorf("component %+v", c)
		}
		if (c.DiskBytes == nil) != (c.Name == Grafana) {
			t.Errorf("%s disk %v", c.Name, c.DiskBytes)
		}
	}
	if st.Ingest.Spans != 3 || len(st.Alerts) != 1 || st.Alerts[0].ServiceID != "svc1" {
		t.Errorf("ingest %+v, alerts %+v", st.Ingest, st.Alerts)
	}
	b, _ := json.Marshal(st.Settings)
	if !strings.Contains(string(b), `"metricsRetentionDays":30`) {
		t.Errorf("settings JSON %s", b)
	}
}

func TestState(t *testing.T) {
	running := &docker.Container{State: "running"}
	exited := &docker.Container{State: "exited"}
	tests := []struct {
		name    string
		enabled bool
		cur     *docker.Container
		healthy bool
		failure string
		want    State
	}{
		{"healthy", true, running, true, "", Running},
		{"not answering yet", true, running, false, "", Starting},
		{"not created yet", true, nil, false, "", Starting},
		{"stopped while enabled", true, exited, false, "", Starting},
		{"stopped while disabled", false, exited, false, "", Stopped},
		{"crash looping", true, &docker.Container{State: "restarting"}, false, "", Failed},
		{"reconcile failed", true, running, true, "pull failed", Failed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := state(tt.enabled, tt.cur, tt.healthy, tt.failure); got != tt.want {
				t.Errorf("state() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestSpec(t *testing.T) {
	in := specInput{
		settings:      DefaultSettings(),
		addrs:         Addrs{Metrics: "127.0.0.1:8428", Logs: "127.0.0.1:9428", Traces: "127.0.0.1:10428", Grafana: "127.0.0.1:3001"},
		dir:           "/var/lib/shed/telemetry",
		minFree:       2048 << 20,
		grafanaURL:    "https://shed.example.com/grafana/",
		adminPassword: "pw",
	}
	logs, err := spec(Logs, in)
	if err != nil {
		t.Fatal(err)
	}
	if logs.Network != docker.HostNetwork || logs.Name != "shed-telemetry-logs" || logs.Memory != 512<<20 ||
		!slices.Contains(logs.Cmd, "-retentionPeriod=14d") ||
		!slices.Contains(logs.Cmd, fmt.Sprintf("-retention.maxDiskSpaceUsageBytes=%d", int64(20<<30)*6/10)) ||
		!slices.Contains(logs.Cmd, "-httpListenAddr=127.0.0.1:9428") {
		t.Errorf("logs spec %+v", logs)
	}
	metrics, _ := spec(Metrics, in)
	if !slices.Contains(metrics.Cmd, fmt.Sprintf("-storage.minFreeDiskSpaceBytes=%d", 2048<<20)) ||
		len(metrics.Binds) != 1 || !metrics.Binds[0].ReadOnly || metrics.Binds[0].Source != "/var/lib/shed/telemetry/scrape" {
		t.Errorf("metrics spec %+v", metrics)
	}
	gf, _ := spec(Grafana, in)
	if len(gf.Binds) != 1 || gf.Binds[0].Target != grafana.ProvisioningPath || !gf.Binds[0].ReadOnly ||
		!slices.Contains(gf.Env, "GF_SECURITY_ADMIN_PASSWORD=pw") {
		t.Errorf("grafana spec %+v", gf)
	}

	// The hash follows everything the container depends on.
	moved := in
	moved.addrs.Logs = "127.0.0.1:9429"
	gf2, _ := spec(Grafana, moved)
	if gf2.Labels[labelSpec] == gf.Labels[labelSpec] {
		t.Error("moving a store did not change Grafana's spec")
	}
	longer := in
	longer.settings.MetricsRetentionDays = 60
	if m2, _ := spec(Metrics, longer); m2.Labels[labelSpec] == metrics.Labels[labelSpec] {
		t.Error("changing retention did not change the spec")
	}
	if l2, _ := spec(Logs, longer); l2.Labels[labelSpec] != logs.Labels[labelSpec] {
		t.Error("changing metrics retention changed the logs spec")
	}
}
