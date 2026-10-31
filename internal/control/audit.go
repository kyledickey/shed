package control

import (
	"context"
	"maps"

	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/store"
)

// Audit records and lists audit events. *audit.Log implements it.
type Audit interface {
	Record(ctx context.Context, e audit.Event)
	Events(ctx context.Context, f store.AuditFilter) (audit.Page, error)
	Settings(ctx context.Context) (audit.Settings, error)
	SetSettings(ctx context.Context, s audit.Settings) (audit.Settings, error)
}

var _ Audit = (*audit.Log)(nil)

// Record records e, done by the actor of ctx. Interfaces record through it
// what happens outside Plane, such as denied requests and MCP tool calls.
func (p *Plane) Record(ctx context.Context, e audit.Event) {
	p.audit.Record(ctx, e)
}

// AuditEvents returns the audit events f selects, newest first, a page at a
// time.
func (p *Plane) AuditEvents(ctx context.Context, f store.AuditFilter) (audit.Page, error) {
	switch audit.Outcome(f.Outcome) {
	case "", audit.OK, audit.Failed, audit.Denied:
	default:
		return audit.Page{}, errorf(ErrInvalid, "outcome must be ok, failed, or denied")
	}
	return p.audit.Events(ctx, f)
}

// AuditSettings returns the audit log's settings.
func (p *Plane) AuditSettings(ctx context.Context) (audit.Settings, error) {
	return p.audit.Settings(ctx)
}

// SetAuditSettings saves the audit log's settings.
func (p *Plane) SetAuditSettings(ctx context.Context, s audit.Settings) (audit.Settings, error) {
	out, err := p.audit.SetSettings(ctx, s)
	p.record(ctx, err, audit.Event{Action: "audit.settings.update", Detail: map[string]any{"retentionDays": s.RetentionDays}})
	return out, err
}

// record records e with the outcome of err.
func (p *Plane) record(ctx context.Context, err error, e audit.Event) {
	e.Outcome = audit.Result(err)
	p.audit.Record(ctx, e)
}

// serviceEvent returns an event about svc, with its name and detail.
func serviceEvent(action string, svc store.Service, detail map[string]any) audit.Event {
	d := make(map[string]any, len(detail)+1)
	if svc.Name != "" {
		d["service"] = svc.Name
	}
	maps.Copy(d, detail)
	return audit.Event{Action: action, ProjectID: svc.ProjectID, ServiceID: svc.ID, Target: svc.ID, Detail: d}
}

// recordService records action on service id with the outcome of err,
// naming the service as it is stored now.
func (p *Plane) recordService(ctx context.Context, err error, action, id string, detail map[string]any) {
	svc, lerr := p.store.Service(ctx, id)
	if lerr != nil {
		svc = store.Service{ID: id}
	}
	p.record(ctx, err, serviceEvent(action, svc, detail))
}

// field is a setting and whether an update changed it.
type field struct {
	name    string
	changed bool
}

// changed returns the names of the changed fields.
func changed(fields []field) []string {
	names := []string{}
	for _, f := range fields {
		if f.changed {
			names = append(names, f.name)
		}
	}
	return names
}
