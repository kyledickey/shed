package observability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"time"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/grafana"
)

// Errors callers branch on. Their text is fit to show.
var (
	// ErrEnabled reports an operation that needs telemetry off.
	ErrEnabled = errors.New("turn telemetry off first")
	// ErrDisabled reports an operation that needs telemetry on.
	ErrDisabled = errors.New("telemetry is off")
	// ErrUnknownComponent reports a component name that is not one of
	// metrics, logs, traces, or grafana.
	ErrUnknownComponent = errors.New("no such telemetry component")
	// ErrNoContainer reports a component whose container does not exist
	// yet.
	ErrNoContainer = errors.New("the component's container does not exist yet")
)

// How long a component may take to answer its health check after it
// starts. Grafana installs its data source plugins the first time.
const (
	storeReady   = 2 * time.Minute
	grafanaReady = 5 * time.Minute
	stopTimeout  = 30 * time.Second
)

// Reconcile makes the stack match the stored settings. While enabled, it
// creates or recreates each container whose spec changed, starts stopped
// ones, waits for the stores, provisions Grafana's data sources, starts
// Grafana, creates its shed folder, provisions the alert rules and webhook, and
// starts the workers. While disabled, it stops the workers and the containers and
// keeps their data. Running it again changes nothing.
func (m *Manager) Reconcile(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.Settings(ctx)
	if err != nil {
		return err
	}
	if !s.Enabled {
		return m.disable(ctx)
	}
	return m.enable(ctx, s)
}

func (m *Manager) enable(ctx context.Context, s Settings) error {
	pw, err := m.AdminPassword(ctx)
	if err != nil {
		return err
	}
	in := specInput{
		settings:      s,
		addrs:         m.cfg.Addrs,
		dir:           m.cfg.Dir,
		minFree:       m.cfg.MinFreeBytes,
		grafanaURL:    m.cfg.GrafanaURL,
		adminPassword: pw,
	}
	running, err := m.containers(ctx)
	if err != nil {
		return err
	}
	if err := m.step(Metrics, m.writeScrape()); err != nil {
		return err
	}
	for _, c := range stores {
		if err := m.step(c, m.ensure(ctx, c, in, running[c])); err != nil {
			return err
		}
	}
	for _, c := range stores {
		if err := m.step(c, m.waitReady(ctx, storeReady, m.victoria(c).Health)); err != nil {
			return err
		}
	}
	gf := m.cfg.Grafana(pw)
	err = m.step(Grafana, m.provision(func(dir string) error {
		return grafana.WriteDatasources(dir, m.cfg.Addrs.stores())
	}))
	if err != nil {
		return err
	}
	if err := m.step(Grafana, m.ensure(ctx, Grafana, in, running[Grafana])); err != nil {
		return err
	}
	ready := func(ctx context.Context) error { _, err := gf.Health(ctx); return err }
	if err := m.step(Grafana, m.waitReady(ctx, grafanaReady, ready)); err != nil {
		return err
	}
	if err := m.step(Grafana, m.provisionAlerting(ctx, gf)); err != nil {
		return err
	}
	m.activate()
	return nil
}

// provisionAlerting makes Grafana's alert rules and webhook match the stored
// rule overrides and the webhook token.
func (m *Manager) provisionAlerting(ctx context.Context, gf GrafanaAPI) error {
	m.alertingMu.Lock()
	defer m.alertingMu.Unlock()
	// Grafana files the alert rules under the folder titled shed and creates
	// it with a random UID if it is missing, so the folder comes first.
	if err := gf.EnsureFolder(ctx, grafana.Folder{UID: grafana.ShedFolder, Title: "shed"}); err != nil {
		return err
	}
	rows, err := m.cfg.Store.NotificationRules(ctx)
	if err != nil {
		return err
	}
	overrides := make(map[string]grafana.AlertOverride, len(rows))
	for _, r := range rows {
		overrides[r.ID] = grafana.AlertOverride{Paused: !r.Enabled, Threshold: r.Threshold, ForSeconds: r.ForSeconds}
	}
	token, err := m.AlertWebhookToken(ctx)
	if err != nil {
		return err
	}
	// shortcut: Grafana reads the token as plaintext from notify.yaml, guarded by
	// the data directory's 0750; use a file secret if the host gets other users.
	err = m.provision(func(dir string) error {
		if err := grafana.WriteAlertRules(dir, overrides); err != nil {
			return err
		}
		return grafana.WriteWebhook(dir, m.cfg.AlertWebhookURL, token)
	})
	if err != nil {
		return err
	}
	return gf.ReloadAlertRules(ctx)
}

// ApplyAlertRules makes Grafana evaluate the stored rule overrides. While
// telemetry is off it does nothing: the next Reconcile that enables telemetry
// renders them.
func (m *Manager) ApplyAlertRules(ctx context.Context) error {
	s, err := m.Settings(ctx)
	if err != nil {
		return err
	}
	if !s.Enabled {
		return nil
	}
	pw, err := m.AdminPassword(ctx)
	if err != nil {
		return err
	}
	if err := m.provisionAlerting(ctx, m.cfg.Grafana(pw)); err != nil {
		return fmt.Errorf("observability: apply alert rules: %w", err)
	}
	return nil
}

// provision writes Grafana provisioning files with write, then makes them
// readable to Grafana.
func (m *Manager) provision(write func(dir string) error) error {
	dir := filepath.Join(m.cfg.Dir, provisioning)
	if err := write(dir); err != nil {
		return err
	}
	return readable(dir)
}

// step records err as c's state and returns it wrapped.
func (m *Manager) step(c Component, err error) error {
	m.setErr(c, err)
	if err != nil {
		return fmt.Errorf("observability: %s: %w", c, err)
	}
	return nil
}

// victoria returns the client of the store c.
func (m *Manager) victoria(c Component) Victoria {
	switch c {
	case Metrics:
		return m.cfg.Metrics
	case Logs:
		return m.cfg.Logs
	}
	return m.cfg.Traces
}

// ensure makes c's container match its spec: it leaves a matching running
// container alone, starts a matching stopped one, and otherwise removes
// whatever is there and creates the container on the same volume.
func (m *Manager) ensure(ctx context.Context, c Component, in specInput, cur *docker.Container) error {
	want, err := spec(c, in)
	if err != nil {
		return err
	}
	if cur != nil && cur.Labels[labelSpec] == want.Labels[labelSpec] {
		switch cur.State {
		case "running", "restarting":
			return nil
		}
		return m.cfg.Docker.Start(ctx, cur.ID)
	}
	if _, err := m.cfg.Docker.ResolveImage(ctx, want.Image); err != nil {
		m.cfg.Log.Info("pull telemetry image", "image", want.Image)
		if err := m.cfg.Docker.PullImage(ctx, want.Image, io.Discard); err != nil {
			return err
		}
	}
	if cur != nil {
		m.cfg.Log.Info("recreate telemetry container", "component", c)
		if err := m.cfg.Docker.Remove(ctx, cur.ID); err != nil {
			return err
		}
	}
	if err := m.cfg.Docker.EnsureVolume(ctx, volumeName(c)); err != nil {
		return err
	}
	_, err = m.cfg.Docker.Run(ctx, want)
	return err
}

// waitReady polls ready until it succeeds, ctx is done, or timeout passes.
func (m *Manager) waitReady(ctx context.Context, timeout time.Duration, ready func(context.Context) error) error {
	deadline := time.Now().Add(timeout)
	for {
		probe, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := ready(probe)
		cancel()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("not ready after %s: %w", timeout, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.poll):
		}
	}
}

// disable stops the workers, removes the database exporters, and stops the
// containers, Grafana first.
func (m *Manager) disable(ctx context.Context) error {
	m.deactivate()
	running, err := m.containers(ctx)
	if err != nil {
		return err
	}
	var errs []error
	if m.dbs != nil {
		errs = append(errs, m.dbs.removeAll(ctx))
		m.scrapeMu.Lock()
		m.targets = nil
		m.scrapeMu.Unlock()
	}
	for _, c := range slices.Backward(order) {
		cur := running[c]
		if cur == nil || cur.State == "exited" || cur.State == "created" {
			m.setErr(c, nil)
			continue
		}
		errs = append(errs, m.step(c, m.cfg.Docker.Stop(ctx, cur.ID, stopTimeout)))
	}
	return errors.Join(errs...)
}

// containers returns the components' containers that exist, by component.
func (m *Manager) containers(ctx context.Context) (map[Component]*docker.Container, error) {
	list, err := m.cfg.Docker.List(ctx, map[string]string{LabelComponent: ""})
	if err != nil {
		return nil, fmt.Errorf("observability: %w", err)
	}
	out := make(map[Component]*docker.Container, len(list))
	for i := range list {
		c := Component(list[i].Labels[LabelComponent])
		if _, ok := components[c]; ok {
			out[c] = &list[i]
		}
	}
	return out, nil
}

// DeleteData stops the workers, then removes the containers, their
// volumes, the telemetry directory, and the records of shed's dashboards.
// It fails with ErrEnabled unless telemetry is off. The settings cannot be
// saved until it returns.
func (m *Manager) DeleteData(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settingsMu.Lock()
	defer m.settingsMu.Unlock()
	s, err := m.Settings(ctx)
	if err != nil {
		return err
	}
	if s.Enabled {
		return ErrEnabled
	}
	// Telemetry may have been turned off without being reconciled yet.
	m.deactivate()
	running, err := m.containers(ctx)
	if err != nil {
		return err
	}
	if m.dbs != nil {
		if err := m.dbs.removeAll(ctx); err != nil {
			return fmt.Errorf("observability: %w", err)
		}
	}
	for _, c := range order {
		if cur := running[c]; cur != nil {
			if err := m.cfg.Docker.Remove(ctx, cur.ID); err != nil {
				return fmt.Errorf("observability: %w", err)
			}
		}
		if err := m.cfg.Docker.RemoveVolume(ctx, volumeName(c)); err != nil {
			return fmt.Errorf("observability: %w", err)
		}
		m.setErr(c, nil)
	}
	if err := os.RemoveAll(m.cfg.Dir); err != nil {
		return fmt.Errorf("observability: delete %s: %w", m.cfg.Dir, err)
	}
	if err := m.cfg.Store.DeleteGrafanaDashboards(ctx); err != nil {
		return err
	}
	// The shipped logs are gone: container output is shipped again from
	// what Docker still has, and the audit mirror starts over.
	if err := m.cfg.Store.DeleteLogCursors(ctx); err != nil {
		return err
	}
	return m.cfg.Store.DeleteSetting(ctx, auditCursorKey)
}

// Restart restarts the container of c. It fails with ErrDisabled while
// telemetry is off.
func (m *Manager) Restart(ctx context.Context, c Component) error {
	if _, ok := components[c]; !ok {
		return ErrUnknownComponent
	}
	s, err := m.Settings(ctx)
	if err != nil {
		return err
	}
	if !s.Enabled {
		return ErrDisabled
	}
	running, err := m.containers(ctx)
	if err != nil {
		return err
	}
	cur := running[c]
	if cur == nil {
		return ErrNoContainer
	}
	if err := m.cfg.Docker.Restart(ctx, cur.ID, stopTimeout); err != nil {
		return fmt.Errorf("observability: %w", err)
	}
	return nil
}

// writeScrape writes scrape.yaml with the current exporters.
func (m *Manager) writeScrape() error {
	m.scrapeMu.Lock()
	defer m.scrapeMu.Unlock()
	return writeScrapeConfig(m.cfg.Dir, m.cfg.Addrs, m.targets)
}

// setTargets makes targets the exporters scrape.yaml names, rewriting it
// if they changed. VictoriaMetrics notices within its config check interval.
func (m *Manager) setTargets(targets []scrapeTarget) error {
	m.scrapeMu.Lock()
	defer m.scrapeMu.Unlock()
	if reflect.DeepEqual(targets, m.targets) {
		return nil
	}
	if err := writeScrapeConfig(m.cfg.Dir, m.cfg.Addrs, targets); err != nil {
		return err
	}
	m.targets = targets
	return nil
}
