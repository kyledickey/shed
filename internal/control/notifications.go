package control

import (
	"context"
	"strings"

	"github.com/kyledickey/shed/internal/audit"
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
