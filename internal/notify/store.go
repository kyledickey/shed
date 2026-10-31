package notify

import (
	"context"
	"errors"
	"time"

	"github.com/kyledickey/shed/internal/store"
)

// Database keeps notification data and project metadata. *store.Store implements it.
type Database interface {
	NotificationChannels(ctx context.Context) ([]store.NotificationChannel, error)
	EnqueueNotification(ctx context.Context, d store.NotificationDelivery) error
	DueNotifications(ctx context.Context, t time.Time, limit int) ([]store.NotificationDelivery, error)
	NotificationSent(ctx context.Context, id string, at time.Time) error
	NotificationRetry(ctx context.Context, id string, next time.Time, lastError string) error
	NotificationFailed(ctx context.Context, id, lastError string) error
	NotificationRules(ctx context.Context) ([]store.NotificationRule, error)
	Project(ctx context.Context, id string) (store.Project, error)
	Service(ctx context.Context, id string) (store.Service, error)
	PruneNotifications(ctx context.Context, t time.Time) error
}

// NewStore adapts db to the notification outbox and rule store.
func NewStore(db Database) Store { return notificationStore{db} }

type notificationStore struct{ Database }

func (s notificationStore) Channels(ctx context.Context) ([]Channel, error) {
	cs, err := s.NotificationChannels(ctx)
	out := make([]Channel, len(cs))
	for i, c := range cs {
		out[i] = Channel{ID: c.ID, Name: c.Name, Provider: c.Provider, Target: c.Target, Enabled: c.Enabled}
	}
	return out, err
}

func (s notificationStore) AddDelivery(ctx context.Context, d Delivery) error {
	return s.EnqueueNotification(ctx, store.NotificationDelivery{
		ChannelID: d.ChannelID, Key: d.Key, Title: d.Title, Body: d.Body, URL: d.URL, Project: d.Project,
		NextAttemptAt: d.NextAttemptAt,
	})
}

func (s notificationStore) DueDeliveries(ctx context.Context, t time.Time, limit int) ([]Delivery, error) {
	ds, err := s.DueNotifications(ctx, t, limit)
	out := make([]Delivery, len(ds))
	for i, d := range ds {
		out[i] = Delivery{
			ID: d.ID, ChannelID: d.ChannelID, Attempts: d.Attempts, NextAttemptAt: d.NextAttemptAt,
			Message: Message{Key: d.Key, Title: d.Title, Body: d.Body, URL: d.URL, Project: d.Project},
		}
	}
	return out, err
}

func (s notificationStore) DeliverySent(ctx context.Context, id string, at time.Time) error {
	return s.NotificationSent(ctx, id, at)
}

func (s notificationStore) DeliveryRetry(ctx context.Context, id string, next time.Time, lastError string) error {
	return s.NotificationRetry(ctx, id, next, lastError)
}

func (s notificationStore) DeliveryFailed(ctx context.Context, id, lastError string) error {
	return s.NotificationFailed(ctx, id, lastError)
}

func (s notificationStore) Rules(ctx context.Context) ([]RuleOverride, error) {
	rs, err := s.NotificationRules(ctx)
	out := make([]RuleOverride, len(rs))
	for i, r := range rs {
		out[i] = RuleOverride(r)
	}
	return out, err
}

func (s notificationStore) ProjectName(ctx context.Context, id string) (string, error) {
	p, err := s.Project(ctx, id)
	return p.Name, err
}

func (s notificationStore) ServiceProject(ctx context.Context, serviceID string) (string, error) {
	svc, err := s.Service(ctx, serviceID)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	return svc.ProjectID, err
}

func (s notificationStore) PruneDeliveries(ctx context.Context, t time.Time) error {
	return s.PruneNotifications(ctx, t)
}
