package notify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kyledickey/shed/internal/telemetry"
)

const (
	eventTimeout = 5 * time.Second
	bodyLimit    = 500 // bytes of event text sent as a message body
	crashWindow  = 10 * time.Minute
	crashMute    = 30 * time.Minute
)

// loop is the crash history of one service.
type loop struct {
	crashes    []time.Time
	mutedUntil time.Time
}

// Event notifies about e if a rule covers it and is enabled; other events
// are ignored. It matches the Events callbacks of the deployer and the
// backup manager. It runs whether or not telemetry is on, only touches the
// store, and logs its failures instead of returning them.
func (n *Notifier) Event(e telemetry.Event) {
	switch e.Kind {
	case telemetry.EventDeployFailed:
		n.send(e, RuleDeployFailed, "deploy:"+e.ID+":failed", "Deploy failed: "+e.Service.Name, e.Text)
	case telemetry.EventBackupFailed:
		n.send(e, RuleBackupFailed, "backup:"+e.ID+":failed", "Backup failed: "+serviceOrShed(e),
			"The backup of "+serviceOrShed(e)+" failed. The details are in shed.")
	case telemetry.EventRestoreFailed:
		n.send(e, RuleRestoreFailed, "restore:"+e.ID+":failed", "Restore failed: "+serviceOrShed(e),
			"The restore of "+serviceOrShed(e)+" failed. The details are in shed.")
	case telemetry.EventCrash:
		if e.OOM {
			n.send(e, RuleOOM, fmt.Sprintf("oom:%s:%d", e.Service.ID, e.Start.Unix()), "Out of memory: "+e.Service.Name, e.Text)
		}
		n.crashed(e)
	}
}

func serviceOrShed(e telemetry.Event) string {
	if e.Service.ID == "" {
		return "shed.db"
	}
	return e.Service.Name
}

// rule returns the rule id with overrides applied.
func (n *Notifier) rule(ctx context.Context, id string) (Rule, error) {
	os, err := n.store.Rules(ctx)
	if err != nil {
		return Rule{}, fmt.Errorf("notify: load rules: %w", err)
	}
	for _, r := range MergeRules(os) {
		if r.ID == id {
			return r, nil
		}
	}
	return Rule{}, fmt.Errorf("%w %q", ErrUnknownRule, id)
}

// send enqueues a message about e if the rule id is enabled. Backup and restore
// errors aren't masked and channels are third parties, so body must never hold
// their text.
func (n *Notifier) send(e telemetry.Event, id, key, title, body string) {
	ctx, cancel := context.WithTimeout(context.Background(), eventTimeout)
	defer cancel()
	r, err := n.rule(ctx, id)
	if err != nil || !r.Enabled {
		if err != nil {
			n.log.Error("notify: event", "rule", id, "err", err)
		}
		return
	}
	n.enqueueEvent(ctx, e, key, title, body)
}

func (n *Notifier) enqueueEvent(ctx context.Context, e telemetry.Event, key, title, body string) {
	if strings.TrimSpace(body) == "" {
		body = title
	}
	if len(body) > bodyLimit {
		body = strings.ToValidUTF8(body[:bodyLimit], "") + "…"
	}
	m := Message{Key: key, Title: title, Body: body}
	if id := e.Service.ID; id != "" && n.baseURL != "" {
		m.URL = fmt.Sprintf("%s/projects/%s/services/%s", n.baseURL, e.Service.ProjectID, id)
	}
	if pid := e.Service.ProjectID; pid != "" {
		name, err := n.store.ProjectName(ctx, pid)
		if err != nil {
			n.log.Warn("notify: project name", "project", pid, "err", err)
		}
		m.Project = name
	}
	if err := n.Enqueue(ctx, m); err != nil {
		n.log.Error("notify: enqueue event", "key", key, "err", err)
	}
}

// crashed counts a crash of e's service and notifies when the service
// reaches the crash-loop threshold.
//
// shortcut: counts reset when shed restarts, persist if that hides real crash loops.
func (n *Notifier) crashed(e telemetry.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), eventTimeout)
	defer cancel()
	r, err := n.rule(ctx, RuleCrashLoop)
	if err != nil || !r.Enabled {
		if err != nil {
			n.log.Error("notify: event", "rule", RuleCrashLoop, "err", err)
		}
		return
	}
	now := n.now()
	if !n.countCrash(e.Service.ID, now, int(*r.Threshold)) {
		return
	}
	title := "Crash loop: " + e.Service.Name
	body := fmt.Sprintf("%s crashed %d times in 10 minutes.", e.Service.Name, int(*r.Threshold))
	n.enqueueEvent(ctx, e, fmt.Sprintf("crash-loop:%s:%d", e.Service.ID, now.Unix()), title, body)
}

// countCrash records a crash of service at now and reports whether it
// reached limit crashes within the window while not muted. It then forgets
// the crashes and mutes the service.
func (n *Notifier) countCrash(service string, now time.Time, limit int) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.loops == nil {
		n.loops = map[string]*loop{}
	}
	for id, l := range n.loops {
		l.crashes = trimBefore(l.crashes, now.Add(-crashWindow))
		if len(l.crashes) == 0 && !now.Before(l.mutedUntil) {
			delete(n.loops, id)
		}
	}
	l := n.loops[service]
	if l == nil {
		l = &loop{}
		n.loops[service] = l
	}
	if now.Before(l.mutedUntil) {
		return false
	}
	l.crashes = append(l.crashes, now)
	if len(l.crashes) < limit {
		return false
	}
	l.crashes, l.mutedUntil = nil, now.Add(crashMute)
	return true
}

func trimBefore(ts []time.Time, t time.Time) []time.Time {
	i := 0
	for i < len(ts) && ts[i].Before(t) {
		i++
	}
	return ts[i:]
}
