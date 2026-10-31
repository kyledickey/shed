package control

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/grafana"
	"github.com/kyledickey/shed/internal/notify"
	"github.com/kyledickey/shed/internal/store"
)

// Notifier checks and tests notification channels. *notify.Notifier
// implements it.
type Notifier interface {
	Providers() []notify.Provider
	Validate(c notify.Channel) error
	Test(ctx context.Context, c notify.Channel) error
}

var _ Notifier = (*notify.Notifier)(nil)

// NotificationChannelInput is a new notification channel.
type NotificationChannelInput struct {
	Name     string
	Provider string
	Target   string
	Enabled  bool
}

// NotificationChannelPatch changes a notification channel. A nil field and
// an empty Target stay as they are.
type NotificationChannelPatch struct {
	Name    *string
	Target  string
	Enabled *bool
}

// NotificationProviders returns the names of the notification providers.
func (p *Plane) NotificationProviders() []string {
	var names []string
	for _, pr := range p.notifier.Providers() {
		names = append(names, pr.Name)
	}
	return names
}

// NotificationChannels returns the notification channels, oldest first.
func (p *Plane) NotificationChannels(ctx context.Context) ([]store.NotificationChannel, error) {
	return p.store.NotificationChannels(ctx)
}

// CreateNotificationChannel validates and stores a notification channel.
func (p *Plane) CreateNotificationChannel(ctx context.Context, in NotificationChannelInput) (store.NotificationChannel, error) {
	c := store.NotificationChannel{Name: strings.TrimSpace(in.Name), Provider: in.Provider, Target: in.Target, Enabled: in.Enabled}
	out, err := p.createNotificationChannel(ctx, c)
	// The target is a secret, so it is never recorded.
	p.record(ctx, err, audit.Event{Action: "notification.channel.create", Target: out.ID,
		Detail: map[string]any{"name": c.Name, "provider": c.Provider}})
	return out, err
}

func (p *Plane) createNotificationChannel(ctx context.Context, c store.NotificationChannel) (store.NotificationChannel, error) {
	if err := p.validateChannel(c); err != nil {
		return store.NotificationChannel{}, err
	}
	return p.store.CreateNotificationChannel(ctx, c)
}

// UpdateNotificationChannel applies patch to a channel and returns it.
func (p *Plane) UpdateNotificationChannel(ctx context.Context, id string, patch NotificationChannelPatch) (store.NotificationChannel, error) {
	c, err := p.updateNotificationChannel(ctx, id, patch)
	p.record(ctx, err, audit.Event{Action: "notification.channel.update", Target: id, Detail: map[string]any{
		"name": c.Name,
		"fields": changed([]field{
			{"name", patch.Name != nil},
			{"target", patch.Target != ""},
			{"enabled", patch.Enabled != nil},
		}),
	}})
	return c, err
}

func (p *Plane) updateNotificationChannel(ctx context.Context, id string, patch NotificationChannelPatch) (store.NotificationChannel, error) {
	c, err := p.store.NotificationChannel(ctx, id)
	if err != nil {
		return store.NotificationChannel{}, err
	}
	if patch.Name != nil {
		c.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.Target != "" {
		c.Target = patch.Target
	}
	if patch.Enabled != nil {
		c.Enabled = *patch.Enabled
	}
	if err := p.validateChannel(c); err != nil {
		return c, err
	}
	return c, p.store.UpdateNotificationChannel(ctx, c)
}

// DeleteNotificationChannel deletes a channel and its deliveries.
func (p *Plane) DeleteNotificationChannel(ctx context.Context, id string) error {
	c, err := p.store.NotificationChannel(ctx, id)
	if err == nil {
		err = p.store.DeleteNotificationChannel(ctx, id)
	}
	p.record(ctx, err, audit.Event{Action: "notification.channel.delete", Target: id,
		Detail: map[string]any{"name": c.Name, "provider": c.Provider}})
	return err
}

// TestNotificationChannel sends a test message to a channel right away, even
// if it is disabled. A failed send is an ErrInvalid error with the reason.
func (p *Plane) TestNotificationChannel(ctx context.Context, id string) error {
	c, err := p.store.NotificationChannel(ctx, id)
	if err != nil {
		return err
	}
	err = p.notifier.Test(ctx, toNotifyChannel(c))
	if err != nil {
		err = errorf(ErrInvalid, "%v", err)
	}
	p.record(ctx, err, audit.Event{Action: "notification.channel.test", Target: id,
		Detail: map[string]any{"name": c.Name, "provider": c.Provider}})
	return err
}

// NotificationDeliveries returns the newest notification deliveries first.
func (p *Plane) NotificationDeliveries(ctx context.Context, limit int) ([]store.NotificationDelivery, error) {
	return p.store.NotificationDeliveries(ctx, limit)
}

func (p *Plane) validateChannel(c store.NotificationChannel) error {
	if err := p.notifier.Validate(toNotifyChannel(c)); err != nil {
		return errorf(ErrInvalid, "%v", err)
	}
	return nil
}

func toNotifyChannel(c store.NotificationChannel) notify.Channel {
	return notify.Channel{ID: c.ID, Name: c.Name, Provider: c.Provider, Target: c.Target, Enabled: c.Enabled}
}

// NotificationRule is a notification rule with its overrides applied.
type NotificationRule struct {
	notify.Rule
	// Available is false for a rule that cannot fire now, such as one that
	// needs telemetry while it is off.
	Available bool
}

// NotificationRulePatch changes a notification rule. A nil field stays as it
// is.
type NotificationRulePatch struct {
	Enabled    *bool
	Threshold  *float64
	ForSeconds *int
}

// notificationCatalog returns every rule: the event rules, then the metric
// rules that Grafana evaluates.
func notificationCatalog() []notify.RuleDef {
	defs := notify.EventRules()
	for _, r := range grafana.AlertRules() {
		defs = append(defs, notify.RuleDef{
			ID: r.UID, Title: r.Title, Description: r.Description,
			Threshold: r.Threshold, Unit: r.Unit, Max: r.Max,
			ForSeconds: r.ForSeconds, NeedsTelemetry: true,
		})
	}
	return defs
}

// NotificationRules returns every notification rule with its settings.
func (p *Plane) NotificationRules(ctx context.Context) ([]NotificationRule, error) {
	s, err := p.telemetry.Settings(ctx)
	if err != nil {
		return nil, err
	}
	os, err := p.store.NotificationRules(ctx)
	if err != nil {
		return nil, err
	}
	overrides := make([]notify.RuleOverride, len(os))
	for i, o := range os {
		overrides[i] = notify.RuleOverride(o)
	}
	rs := notify.MergeRules(notificationCatalog(), overrides)
	out := make([]NotificationRule, len(rs))
	for i, r := range rs {
		out[i] = NotificationRule{Rule: r, Available: !r.NeedsTelemetry || s.Enabled}
	}
	return out, nil
}

// UpdateNotificationRule applies patch to a rule and returns it.
func (p *Plane) UpdateNotificationRule(ctx context.Context, id string, patch NotificationRulePatch) (NotificationRule, error) {
	r, err := p.updateNotificationRule(ctx, id, patch)
	p.record(ctx, err, audit.Event{Action: "notification.rule.update", Target: id, Detail: map[string]any{
		"fields": changed([]field{
			{"enabled", patch.Enabled != nil},
			{"threshold", patch.Threshold != nil},
			{"forSeconds", patch.ForSeconds != nil},
		}),
	}})
	return r, err
}

func (p *Plane) updateNotificationRule(ctx context.Context, id string, patch NotificationRulePatch) (NotificationRule, error) {
	defs := notificationCatalog()
	if err := notify.ValidateRule(defs, id, patch.Threshold, patch.ForSeconds); err != nil {
		if errors.Is(err, notify.ErrUnknownRule) {
			return NotificationRule{}, errorf(ErrNotFound, "%v", err)
		}
		return NotificationRule{}, errorf(ErrInvalid, "%v", err)
	}
	os, err := p.store.NotificationRules(ctx)
	if err != nil {
		return NotificationRule{}, err
	}
	// Start from the stored override, so a patch keeps the other overrides.
	o := store.NotificationRule{ID: id, Enabled: true}
	if i := slices.IndexFunc(os, func(o store.NotificationRule) bool { return o.ID == id }); i >= 0 {
		o = os[i]
	}
	if patch.Enabled != nil {
		o.Enabled = *patch.Enabled
	}
	if patch.Threshold != nil {
		o.Threshold = patch.Threshold
	}
	if patch.ForSeconds != nil {
		o.ForSeconds = patch.ForSeconds
	}
	if err := p.store.SetNotificationRule(ctx, o); err != nil {
		return NotificationRule{}, err
	}
	if defs[slices.IndexFunc(defs, func(d notify.RuleDef) bool { return d.ID == id })].NeedsTelemetry {
		// The override stays on failure: the next reconcile applies it.
		if err := p.telemetry.ApplyAlertRules(ctx); err != nil {
			return NotificationRule{}, errorf(ErrUpstream, "the rule is saved, but Grafana did not apply it yet: %v", err)
		}
	}
	rs, err := p.NotificationRules(ctx)
	if err != nil {
		return NotificationRule{}, err
	}
	return rs[slices.IndexFunc(rs, func(r NotificationRule) bool { return r.ID == id })], nil
}
