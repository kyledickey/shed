// Package audit keeps shed's audit log: one event per mutation, sign-in,
// denial, and security-relevant read, saying who acted, from where, on what,
// and whether it worked.
//
// The actor travels in the request context: the HTTP layer and auth set it
// with [WithActor], and [Log.Record] reads it, so the code that records an
// event does not need to know who called it. A context without an actor is
// shed itself ([System]).
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kyledickey/shed/internal/store"
)

// Kind is who acted.
type Kind string

// The kinds of actor.
const (
	User      Kind = "user"      // a signed-in person, by GitHub login
	Agent     Kind = "agent"     // an OAuth client acting for a user
	GitHub    Kind = "github"    // a webhook delivery
	System    Kind = "system"    // shed itself: boot, schedules, retries
	Docker    Kind = "docker"    // a container event
	Anonymous Kind = "anonymous" // an unauthenticated request
)

// Actor is who acted and from where.
type Actor struct {
	Kind Kind
	// Name is the GitHub login of a user or agent.
	Name string
	// Client is the OAuth client of an agent: its ID and name.
	ClientID, ClientName string
	// IP is the client address of a request.
	IP string
}

type actorKey struct{}

// WithActor returns ctx carrying a.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorFrom returns the actor of ctx, or [System] if it has none.
func ActorFrom(ctx context.Context) Actor {
	if a, ok := ctx.Value(actorKey{}).(Actor); ok {
		return a
	}
	return Actor{Kind: System}
}

// ClientIP returns the address r came from. Behind the embedded proxy the
// connection is from loopback and the proxy sets X-Forwarded-For,
// overwriting what the client sent (or, behind Cloudflare, from
// CF-Connecting-IP), so its last entry is trusted then.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
			last := xff[len(xff)-1]
			last = strings.TrimSpace(last[strings.LastIndexByte(last, ',')+1:])
			if fwd := net.ParseIP(last); fwd != nil {
				ip = fwd
			}
		}
	}
	if ip == nil {
		return host
	}
	return ip.String()
}

// Outcome is how an action ended.
type Outcome string

// The outcomes of an action.
const (
	OK     Outcome = "ok"
	Failed Outcome = "failed"
	Denied Outcome = "denied"
)

// Result returns [Failed] if err is not nil, else [OK].
func Result(err error) Outcome {
	if err != nil {
		return Failed
	}
	return OK
}

// Event is something to record. [Log.Record] adds the time and the actor.
type Event struct {
	// Action is dotted, such as service.variables.update.
	Action  string
	Outcome Outcome
	// ProjectID and ServiceID place the event; Target is the ID of what was
	// acted on.
	ProjectID string
	ServiceID string
	Target    string
	// Detail holds names at the time of the event and changed field names
	// or keys. It must never hold a secret value.
	Detail map[string]any
}

// Store keeps the audit log and its settings. *store.Store implements it.
type Store interface {
	InsertAuditEvent(ctx context.Context, e store.AuditEvent) (store.AuditEvent, error)
	CountAuditEvent(ctx context.Context, e store.AuditEvent, since time.Time) (bool, error)
	AuditEvents(ctx context.Context, f store.AuditFilter) ([]store.AuditEvent, error)
	DeleteAuditEventsBefore(ctx context.Context, t time.Time) error
	Setting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
}

var _ Store = (*store.Store)(nil)

// Log records and lists audit events. It is safe for concurrent use.
type Log struct {
	store Store
	log   *slog.Logger
	now   func() time.Time

	countMu sync.Mutex // serializes coalescing, so that a burst counts into one row
}

// New returns a Log that keeps events in st.
func New(st Store, log *slog.Logger) *Log {
	return &Log{store: st, log: log, now: time.Now}
}

// Record appends e, done by the actor of ctx, to the log. Denials by
// anonymous actors are coalesced into one row per action and IP per minute,
// counted in detail.count. A failure to record is logged, not returned: the
// action has already happened.
func (l *Log) Record(ctx context.Context, e Event) {
	actor := ActorFrom(ctx)
	detail := make(map[string]any, len(e.Detail)+2)
	maps.Copy(detail, e.Detail)
	if actor.Kind == Agent {
		detail["clientId"], detail["client"] = actor.ClientID, actor.ClientName
	}
	coalesce := actor.Kind == Anonymous && e.Outcome == Denied
	if coalesce {
		detail["count"] = 1
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		l.log.Error("record audit event", "action", e.Action, "err", err)
		return
	}
	row := store.AuditEvent{
		Time:      l.now(),
		ActorKind: string(actor.Kind),
		Actor:     actor.Name,
		ActorIP:   actor.IP,
		Action:    e.Action,
		Outcome:   string(e.Outcome),
		ProjectID: e.ProjectID,
		ServiceID: e.ServiceID,
		Target:    e.Target,
		Detail:    string(raw),
	}
	// Record even when the request that did it was canceled.
	ctx = context.WithoutCancel(ctx)
	if coalesce {
		l.countMu.Lock()
		defer l.countMu.Unlock()
		counted, err := l.store.CountAuditEvent(ctx, row, row.Time.Truncate(time.Minute))
		if err != nil {
			l.log.Error("record audit event", "action", e.Action, "err", err)
			return
		}
		if counted {
			return
		}
	}
	if _, err := l.store.InsertAuditEvent(ctx, row); err != nil {
		l.log.Error("record audit event", "action", e.Action, "err", err)
	}
}

// Bounds of a page of events.
const (
	DefaultLimit = 100
	MaxLimit     = 500
)

// Page is events, newest first, and the ID to pass as the next page's
// Before, or 0 if there are no more.
type Page struct {
	Events []store.AuditEvent
	Next   int64
}

// Events returns the events f selects, newest first. A limit outside 1 to
// [MaxLimit] means [DefaultLimit] or MaxLimit.
func (l *Log) Events(ctx context.Context, f store.AuditFilter) (Page, error) {
	switch {
	case f.Limit <= 0:
		f.Limit = DefaultLimit
	case f.Limit > MaxLimit:
		f.Limit = MaxLimit
	}
	limit := f.Limit
	f.Limit++
	events, err := l.store.AuditEvents(ctx, f)
	if err != nil {
		return Page{}, err
	}
	page := Page{Events: events}
	if len(events) > limit {
		page.Events = events[:limit]
		page.Next = page.Events[limit-1].ID
	}
	if page.Events == nil {
		page.Events = []store.AuditEvent{}
	}
	return page, nil
}

// settingsKey is the settings row holding [Settings] as JSON.
const settingsKey = "audit"

// Retention bounds, in days.
const (
	DefaultRetentionDays = 365
	MaxRetentionDays     = 3650
)

// ErrRetention reports a retention out of bounds. Its text is fit to show.
var ErrRetention = fmt.Errorf("retention must be 1 to %d days", MaxRetentionDays)

// Settings configure the audit log.
type Settings struct {
	// RetentionDays is how long events are kept.
	RetentionDays int `json:"retentionDays"`
}

// Settings returns the stored settings, or the defaults.
func (l *Log) Settings(ctx context.Context) (Settings, error) {
	s := Settings{RetentionDays: DefaultRetentionDays}
	raw, err := l.store.Setting(ctx, settingsKey)
	if errors.Is(err, store.ErrNotFound) {
		return s, nil
	}
	if err != nil {
		return Settings{}, err
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return Settings{}, fmt.Errorf("audit: decode settings: %w", err)
	}
	return s, nil
}

// SetSettings validates and stores s.
func (l *Log) SetSettings(ctx context.Context, s Settings) (Settings, error) {
	if s.RetentionDays < 1 || s.RetentionDays > MaxRetentionDays {
		return Settings{}, ErrRetention
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return Settings{}, err
	}
	if err := l.store.SetSetting(ctx, settingsKey, string(raw)); err != nil {
		return Settings{}, err
	}
	return s, nil
}

// pruneEvery is how often events older than the retention are deleted.
const pruneEvery = time.Hour

// Run deletes events older than the retention now and then hourly, until ctx
// is done.
func (l *Log) Run(ctx context.Context) {
	t := time.NewTicker(pruneEvery)
	defer t.Stop()
	for {
		if err := l.Prune(ctx); err != nil && ctx.Err() == nil {
			l.log.Warn("prune audit events", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Prune deletes events older than the retention.
func (l *Log) Prune(ctx context.Context) error {
	s, err := l.Settings(ctx)
	if err != nil {
		return err
	}
	return l.store.DeleteAuditEventsBefore(ctx, l.now().AddDate(0, 0, -s.RetentionDays))
}
