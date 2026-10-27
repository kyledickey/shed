package observability

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/telemetry"
)

// Bounds of what the eventFollower remembers and sends.
const (
	// maxCommand is the longest command line recorded in an audit event.
	maxCommand = 2048
	// maxTracked bounds the containers whose starts and health checks are
	// remembered; the maps are cleared when they fill.
	maxTracked = 4096
	// maxPendingExecs bounds the exec_create events waiting to be claimed.
	maxPendingExecs = 256
	// stableAfter is how long an event stream must last before a failure
	// restarts the backoff.
	stableAfter = time.Minute
)

// eventFollower follows the daemon's events for one run of followEvents.
type eventFollower struct {
	*security
	// started holds the containers seen starting or dying, to tell a restart
	// from a first start. Only the event loop uses it.
	started map[string]struct{}
	// signalled and oomed hold the containers that were sent a signal or hit
	// the memory limit since they started. Only the event loop uses them.
	signalled, oomed map[string]struct{}

	mu     sync.Mutex
	health map[string]string // health check command by container ID
	execs  sync.WaitGroup
	slots  chan struct{}
}

func newFollower(s *security) *eventFollower {
	return &eventFollower{
		security:  s,
		started:   make(map[string]struct{}),
		signalled: make(map[string]struct{}),
		oomed:     make(map[string]struct{}),
		health:    make(map[string]string),
		slots:     make(chan struct{}, maxPendingExecs),
	}
}

// followEvents audits and records the daemon's events until ctx is done,
// reconnecting with backoff whenever the stream breaks, and resuming after
// the last event it handled. It runs whether telemetry is on or off: the
// audit events are always written, the log records and metrics only while
// it is on.
func (s *security) followEvents(ctx context.Context) {
	f := newFollower(s)
	defer f.execs.Wait()
	since := s.now()
	backoff := s.minBackoff
	for ctx.Err() == nil {
		began := time.Now()
		err := s.Docker.Events(ctx, since, func(e docker.Event) {
			since = e.Time.Add(time.Nanosecond)
			f.handle(ctx, e)
		})
		if ctx.Err() != nil {
			return
		}
		if time.Since(began) > stableAfter {
			backoff = s.minBackoff
		}
		s.log.Warn("follow docker events", "err", err, "retry_in", backoff)
		if !sleep(ctx, backoff) {
			return
		}
		backoff = min(backoff*2, s.maxBackoff)
	}
}

// handle acts on e.
func (f *eventFollower) handle(ctx context.Context, e docker.Event) {
	if e.Type == "image" {
		if f.on.Load() {
			f.ship(ctx, record(e.Time, shedService, "docker", "info", "image pull "+e.ID))
		}
		return
	}
	if !watched(e.Attributes) {
		return
	}
	if e.Action == "exec_create" {
		f.execs.Add(1)
		select {
		case f.slots <- struct{}{}:
		case <-ctx.Done():
			f.execs.Done()
			return
		}
		go func() {
			defer f.execs.Done()
			defer func() { <-f.slots }()
			f.audited(ctx, e)
		}()
		return
	}

	svc := f.service(ctx, e.Attributes)
	if e.Action == "oom" {
		f.Audit.Record(audit.WithActor(ctx, audit.Actor{Kind: audit.Docker}), audit.Event{
			Action: "container.oom", Outcome: audit.Failed,
			ProjectID: svc.ProjectID, ServiceID: serviceID(svc), Target: e.ID,
			Detail: map[string]any{"container": e.Name, "service": svc.Name},
		})
	}
	f.track(e, svc)
	restart := false
	if e.Action == "start" || e.Action == "die" {
		_, restart = f.started[e.ID]
		restart = restart && e.Action == "start"
		if len(f.started) >= maxTracked {
			clear(f.started)
		}
		f.started[e.ID] = struct{}{}
	}
	if !f.on.Load() {
		return
	}
	attrs := metric.WithAttributes(svc.Labels()...)
	switch {
	case restart:
		f.restarts.Add(ctx, 1, attrs)
	case e.Action == "oom":
		f.ooms.Add(ctx, 1, attrs)
	}
	level, msg := "info", "container "+e.Action
	switch e.Action {
	case "die":
		msg = fmt.Sprintf("container die (exit %d)", e.ExitCode)
		if e.ExitCode != 0 {
			level = "warn"
		}
	case "oom":
		level, msg = "error", "container oom"
	case "kill":
		level = "warn"
		if sig := e.Attributes["signal"]; sig != "" {
			msg += " (signal " + sig + ")"
		}
	}
	r := record(e.Time, svc, "docker", level, msg)
	r["event"], r["container"], r["container.id"] = e.Action, e.Name, e.ID
	r["image"] = e.Attributes["image"]
	if d := e.Attributes[labelDeployment]; d != "" {
		r["shed.deployment.id"] = d
	}
	if e.Action == "die" {
		r["exit_code"] = strconv.Itoa(e.ExitCode)
	}
	f.ship(ctx, r)
}

// track follows the signals and OOM kills of containers to tell a crash
// from a stop: the daemon sends a kill event before the die of a container
// that was stopped, restarted, or killed, and none before one that exited on
// its own or was killed by the kernel.
func (f *eventFollower) track(e docker.Event, svc telemetry.Service) {
	if len(f.signalled) >= maxTracked || len(f.oomed) >= maxTracked {
		clear(f.signalled)
		clear(f.oomed)
	}
	switch e.Action {
	case "start":
		delete(f.signalled, e.ID)
		delete(f.oomed, e.ID)
	case "kill":
		f.signalled[e.ID] = struct{}{}
	case "oom":
		f.oomed[e.ID] = struct{}{}
	case "die":
		_, signalled := f.signalled[e.ID]
		_, oom := f.oomed[e.ID]
		delete(f.signalled, e.ID)
		delete(f.oomed, e.ID)
		if f.Crashed != nil && e.Attributes[labelService] != "" && (oom || !signalled && e.ExitCode != 0) {
			f.Crashed(Death{ServiceID: svc.ID, Container: e.Name, ExitCode: e.ExitCode, OOM: oom})
		}
	}
}

// serviceID returns the ID to audit svc under: none for shed itself.
func serviceID(svc telemetry.Service) string {
	if svc == shedService {
		return ""
	}
	return svc.ID
}

// audited records the exec_create event e, unless shed started the exec
// itself or it is a run of the container's health check. The daemon announces
// an exec before the call that created it has returned, so it waits for the
// call to claim its exec first.
func (f *eventFollower) audited(ctx context.Context, e docker.Event) {
	if !sleep(ctx, f.settle) {
		return
	}
	if f.Docker.IsOwnExec(e.ExecID) {
		return
	}
	if h := f.healthCommand(ctx, e.ID); h != "" && e.Command == h {
		return
	}
	svc := f.service(ctx, e.Attributes)
	cmd := e.Command
	if len(cmd) > maxCommand {
		cmd = strings.ToValidUTF8(cmd[:maxCommand], "") + "…"
	}
	f.Audit.Record(audit.WithActor(ctx, audit.Actor{Kind: audit.Docker}), audit.Event{
		Action: "container.exec", Outcome: audit.OK,
		ProjectID: svc.ProjectID, ServiceID: serviceID(svc), Target: e.ID,
		Detail: map[string]any{"command": cmd, "container": e.Name, "service": svc.Name},
	})
}

// healthCommand returns the command of the health check of container id,
// which the daemon runs as an exec on its own, or "" if it has none.
func (f *eventFollower) healthCommand(ctx context.Context, id string) string {
	f.mu.Lock()
	cmd, ok := f.health[id]
	f.mu.Unlock()
	if ok {
		return cmd
	}
	c, err := f.Docker.Inspect(ctx, id)
	if err != nil {
		return ""
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.health) >= maxTracked {
		clear(f.health)
	}
	f.health[id] = c.HealthCommand
	return c.HealthCommand
}
