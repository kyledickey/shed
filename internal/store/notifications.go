package store

import (
	"context"
	"fmt"
	"time"
)

const channelCols = `id, name, provider, target, enabled, created_at`

func (s *Store) scanChannel(r scanner) (NotificationChannel, error) {
	var c NotificationChannel
	err := r.Scan(&c.ID, &c.Name, &c.Provider, &c.Target, &c.Enabled, (*timestamp)(&c.CreatedAt))
	if err != nil {
		return NotificationChannel{}, err
	}
	c.Target, err = s.crypt.open(c.Target, channelAD(c.ID))
	return c, err
}

// NotificationChannels returns every notification channel, oldest first.
func (s *Store) NotificationChannels(ctx context.Context) ([]NotificationChannel, error) {
	cs, err := queryAll(ctx, s, s.scanChannel,
		`SELECT `+channelCols+` FROM notification_channels ORDER BY created_at, rowid`)
	if err != nil {
		return nil, fmt.Errorf("store: notification channels: %w", err)
	}
	return cs, nil
}

// NotificationChannel returns the channel with the given ID, or ErrNotFound.
func (s *Store) NotificationChannel(ctx context.Context, id string) (NotificationChannel, error) {
	c, err := queryOne(ctx, s, s.scanChannel,
		`SELECT `+channelCols+` FROM notification_channels WHERE id = ?`, id)
	if err != nil {
		return NotificationChannel{}, fmt.Errorf("store: notification channel %s: %w", id, err)
	}
	return c, nil
}

// CreateNotificationChannel stores c under a new ID and returns it.
func (s *Store) CreateNotificationChannel(ctx context.Context, c NotificationChannel) (NotificationChannel, error) {
	c.ID, c.CreatedAt = NewID(), now()
	err := s.exec(ctx, `INSERT INTO notification_channels (`+channelCols+`) VALUES (?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Provider, s.crypt.seal(c.Target, channelAD(c.ID)), c.Enabled, formatTime(c.CreatedAt))
	if err != nil {
		return NotificationChannel{}, fmt.Errorf("store: create notification channel %q: %w", c.Name, err)
	}
	return c, nil
}

// UpdateNotificationChannel overwrites the name, target, and enabled flag of
// channel c.ID. It returns ErrNotFound for an unknown channel.
func (s *Store) UpdateNotificationChannel(ctx context.Context, c NotificationChannel) error {
	err := s.execOne(ctx, `UPDATE notification_channels SET name = ?, target = ?, enabled = ? WHERE id = ?`,
		c.Name, s.crypt.seal(c.Target, channelAD(c.ID)), c.Enabled, c.ID)
	if err != nil {
		return fmt.Errorf("store: update notification channel %s: %w", c.ID, err)
	}
	return nil
}

// DeleteNotificationChannel deletes a channel and its deliveries. Deleting an
// unknown channel is not an error.
func (s *Store) DeleteNotificationChannel(ctx context.Context, id string) error {
	if err := s.exec(ctx, `DELETE FROM notification_channels WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete notification channel %s: %w", id, err)
	}
	return nil
}

const deliveryCols = `d.id, d.channel_id, c.name, d.key, d.title, d.body, d.url, d.project, d.status,
	d.attempts, d.next_attempt_at, d.last_error, d.created_at, d.sent_at`

const deliveryFrom = ` FROM notification_deliveries d JOIN notification_channels c ON c.id = d.channel_id`

func scanDelivery(r scanner) (NotificationDelivery, error) {
	var d NotificationDelivery
	err := r.Scan(&d.ID, &d.ChannelID, &d.ChannelName, &d.Key, &d.Title, &d.Body, &d.URL, &d.Project,
		&d.Status, &d.Attempts, (*timestamp)(&d.NextAttemptAt), &d.LastError,
		(*timestamp)(&d.CreatedAt), nullTimestamp{&d.SentAt})
	return d, err
}

// EnqueueNotification stores d as pending, due at d.NextAttemptAt. It does
// nothing if d.ChannelID already has a delivery with d.Key.
func (s *Store) EnqueueNotification(ctx context.Context, d NotificationDelivery) error {
	err := s.exec(ctx, `INSERT INTO notification_deliveries
		(id, channel_id, key, title, body, url, project, status, next_attempt_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (channel_id, key) DO NOTHING`,
		NewID(), d.ChannelID, d.Key, d.Title, d.Body, d.URL, d.Project, DeliveryPending,
		formatTime(d.NextAttemptAt), formatTime(now()))
	if err != nil {
		return fmt.Errorf("store: enqueue notification for channel %s: %w", d.ChannelID, err)
	}
	return nil
}

// DueNotifications returns up to limit pending deliveries due at or before
// t, oldest first.
func (s *Store) DueNotifications(ctx context.Context, t time.Time, limit int) ([]NotificationDelivery, error) {
	ds, err := queryAll(ctx, s, scanDelivery, `SELECT `+deliveryCols+deliveryFrom+`
		WHERE d.status = ? AND d.next_attempt_at <= ? ORDER BY d.next_attempt_at, d.rowid LIMIT ?`,
		DeliveryPending, formatTime(t), limit)
	if err != nil {
		return nil, fmt.Errorf("store: due notifications: %w", err)
	}
	return ds, nil
}

// NotificationDeliveries returns the newest deliveries first. A limit of
// zero or less returns all of them.
func (s *Store) NotificationDeliveries(ctx context.Context, limit int) ([]NotificationDelivery, error) {
	if limit <= 0 {
		limit = -1 // SQLite: no limit.
	}
	ds, err := queryAll(ctx, s, scanDelivery, `SELECT `+deliveryCols+deliveryFrom+`
		ORDER BY d.created_at DESC, d.rowid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: notification deliveries: %w", err)
	}
	return ds, nil
}

// NotificationSent marks a delivery sent at the given time.
func (s *Store) NotificationSent(ctx context.Context, id string, at time.Time) error {
	err := s.execOne(ctx, `UPDATE notification_deliveries SET status = ?, sent_at = ? WHERE id = ?`,
		DeliverySent, formatTime(at), id)
	if err != nil {
		return fmt.Errorf("store: notification %s sent: %w", id, err)
	}
	return nil
}

// NotificationRetry counts a failed attempt, records its error, and
// schedules the next attempt.
func (s *Store) NotificationRetry(ctx context.Context, id string, next time.Time, lastError string) error {
	err := s.execOne(ctx, `UPDATE notification_deliveries
		SET attempts = attempts + 1, next_attempt_at = ?, last_error = ? WHERE id = ?`,
		formatTime(next), lastError, id)
	if err != nil {
		return fmt.Errorf("store: notification %s retry: %w", id, err)
	}
	return nil
}

// NotificationFailed counts a failed attempt, records its error, and marks
// the delivery failed.
func (s *Store) NotificationFailed(ctx context.Context, id, lastError string) error {
	err := s.execOne(ctx, `UPDATE notification_deliveries
		SET attempts = attempts + 1, status = ?, last_error = ? WHERE id = ?`,
		DeliveryFailed, lastError, id)
	if err != nil {
		return fmt.Errorf("store: notification %s failed: %w", id, err)
	}
	return nil
}

// PruneNotifications deletes the sent and failed deliveries created before t.
func (s *Store) PruneNotifications(ctx context.Context, t time.Time) error {
	err := s.exec(ctx, `DELETE FROM notification_deliveries WHERE status <> ? AND created_at < ?`,
		DeliveryPending, formatTime(t))
	if err != nil {
		return fmt.Errorf("store: prune notifications: %w", err)
	}
	return nil
}
