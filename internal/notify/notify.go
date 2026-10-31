// Package notify owns shed's notification types and delivers messages to
// the channels a user configured. Providers are adapters registered by
// name. Messages go through an outbox in the store: Enqueue writes one
// pending delivery per enabled channel and Run delivers them with retries,
// so nothing is lost across a restart. Adding a provider means writing one
// adapter and passing it to New.
package notify

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
)

// Message is what shed tells the user, independent of channel.
type Message struct {
	// Key is an idempotency key, such as "deploy:<id>:failed". It may be
	// empty.
	Key string
	// Title is the push sender title or the email subject.
	Title string
	// Body is the message text. It is required.
	Body string
	// URL is the link to open.
	URL string
	// Project groups messages, usually by shed project name.
	Project string
}

// Channel is one configured destination.
type Channel struct {
	ID       string
	Name     string
	Provider string
	// Target is the provider-specific address, such as a Hark webhook URL.
	// It may be a secret, so it never appears in errors or logs.
	Target  string
	Enabled bool
}

// Sender delivers messages to one channel. Adapters implement it.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// Provider describes one adapter: its name and how to open a Sender for a
// target.
type Provider struct {
	Name string
	// Open validates target and returns a Sender for it without contacting
	// the provider. Its errors must not contain target.
	Open func(target string) (Sender, error)
}

// ErrUnknownProvider is returned for a channel whose provider is not
// registered.
var ErrUnknownProvider = errors.New("notify: unknown provider")

// ErrNotDelivered is returned when a provider accepted a message but no
// device or recipient received it.
var ErrNotDelivered = errors.New("notify: no device received the message")

// ErrPermanent marks a send error that retrying cannot fix. Errors that
// match it with [errors.Is] fail their delivery at once.
var ErrPermanent = errors.New("notify: permanent failure")

// permanent returns err marked as matching ErrPermanent, keeping its text.
func permanent(err error) error { return permanentError{err} }

type permanentError struct{ error }

func (e permanentError) Unwrap() error      { return e.error }
func (permanentError) Is(target error) bool { return target == ErrPermanent }

// Delivery is one message waiting for one channel.
type Delivery struct {
	ID        string
	ChannelID string
	Message
	// Attempts is how many sends have failed so far.
	Attempts      int
	NextAttemptAt time.Time
}

// Store loads channels and keeps the outbox.
type Store interface {
	Channels(ctx context.Context) ([]Channel, error)
	// AddDelivery stores a pending delivery due at d.NextAttemptAt. It does
	// nothing if the channel already has one with the same key.
	AddDelivery(ctx context.Context, d Delivery) error
	// DueDeliveries returns up to limit pending deliveries due at or before
	// t, oldest first.
	DueDeliveries(ctx context.Context, t time.Time, limit int) ([]Delivery, error)
	DeliverySent(ctx context.Context, id string, at time.Time) error
	// DeliveryRetry counts a failed attempt and schedules the next one.
	DeliveryRetry(ctx context.Context, id string, next time.Time, lastError string) error
	// DeliveryFailed counts a failed attempt and gives up on the delivery.
	DeliveryFailed(ctx context.Context, id, lastError string) error
	// PruneDeliveries deletes sent and failed deliveries created before t.
	PruneDeliveries(ctx context.Context, t time.Time) error
}

const (
	pollInterval = 30 * time.Second
	batchSize    = 50
	pruneEvery   = 24 * time.Hour
	keepFinished = 30 * 24 * time.Hour
)

// retryDelays is the wait after each failed attempt. A delivery that fails
// once more after the last delay is given up.
var retryDelays = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}

// Notifier enqueues messages and delivers them. It is safe for concurrent
// use.
type Notifier struct {
	store     Store
	providers map[string]Provider
	log       *slog.Logger
	wake      chan struct{}
	now       func() time.Time
}

// New returns a Notifier over the channels and outbox in store, delivering
// through providers. It panics if two providers share a name, which is a
// wiring bug.
func New(store Store, log *slog.Logger, providers ...Provider) *Notifier {
	m := make(map[string]Provider, len(providers))
	for _, p := range providers {
		if _, dup := m[p.Name]; dup {
			panic("notify: duplicate provider " + p.Name)
		}
		m[p.Name] = p
	}
	return &Notifier{store: store, providers: m, log: log, wake: make(chan struct{}, 1), now: time.Now}
}

// Providers returns the registered providers sorted by name.
func (n *Notifier) Providers() []Provider {
	ps := make([]Provider, 0, len(n.providers))
	for _, p := range n.providers {
		ps = append(ps, p)
	}
	slices.SortFunc(ps, func(a, b Provider) int { return strings.Compare(a.Name, b.Name) })
	return ps
}

// Validate reports whether c has a name, a registered provider, and a target
// that provider accepts. It does not contact the provider.
func (n *Notifier) Validate(c Channel) error {
	_, err := n.validate(c)
	return err
}

// Test sends a fixed test message to c now, bypassing the outbox, even if c
// is disabled.
func (n *Notifier) Test(ctx context.Context, c Channel) error {
	s, err := n.validate(c)
	if err != nil {
		return err
	}
	return s.Send(ctx, Message{Title: "shed", Body: "Test notification from shed."})
}

// Enqueue stores m as a pending delivery for every enabled channel and wakes
// Run. A message whose key a channel already has is ignored for that
// channel; a message without a key is always new.
func (n *Notifier) Enqueue(ctx context.Context, m Message) error {
	if strings.TrimSpace(m.Body) == "" {
		return errors.New("notify: message body is empty")
	}
	channels, err := n.store.Channels(ctx)
	if err != nil {
		return fmt.Errorf("notify: load channels: %w", err)
	}
	if m.Key == "" {
		m.Key = rand.Text()
	}
	for _, c := range channels {
		if !c.Enabled {
			continue
		}
		d := Delivery{ChannelID: c.ID, Message: m, NextAttemptAt: n.now()}
		if err := n.store.AddDelivery(ctx, d); err != nil {
			return fmt.Errorf("notify: enqueue for channel %q (%s): %w", c.Name, c.ID, err)
		}
	}
	select {
	case n.wake <- struct{}{}:
	default:
	}
	return nil
}

// Run delivers due messages until ctx ends, then returns its error. It wakes
// when a message is enqueued and otherwise polls.
func (n *Notifier) Run(ctx context.Context) error {
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	var pruned time.Time
	for {
		if err := n.deliverDue(ctx); err != nil && ctx.Err() == nil {
			n.log.Error("notify: deliver", "err", err)
		}
		if t := n.now(); t.Sub(pruned) >= pruneEvery {
			pruned = t
			if err := n.store.PruneDeliveries(ctx, t.Add(-keepFinished)); err != nil && ctx.Err() == nil {
				n.log.Error("notify: prune deliveries", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-n.wake:
		case <-tick.C:
		}
	}
}

// deliverDue sends every due delivery, one at a time. It stops at the first
// store error.
func (n *Notifier) deliverDue(ctx context.Context) error {
	for {
		due, err := n.store.DueDeliveries(ctx, n.now(), batchSize)
		if err != nil || len(due) == 0 {
			return err
		}
		channels, err := n.store.Channels(ctx)
		if err != nil {
			return err
		}
		for _, d := range due {
			if err := n.deliver(ctx, d, channels); err != nil {
				return err
			}
		}
	}
}

func (n *Notifier) deliver(ctx context.Context, d Delivery, channels []Channel) error {
	i := slices.IndexFunc(channels, func(c Channel) bool { return c.ID == d.ChannelID })
	switch {
	case i < 0:
		return n.store.DeliveryFailed(ctx, d.ID, "channel not found")
	case !channels[i].Enabled:
		return n.store.DeliveryFailed(ctx, d.ID, "channel is disabled")
	}
	s, err := n.open(channels[i])
	if err == nil {
		err = s.Send(ctx, d.Message)
	}
	switch {
	case err == nil:
		return n.store.DeliverySent(ctx, d.ID, n.now())
	case ctx.Err() != nil:
		return ctx.Err() // Shutting down: the delivery stays pending.
	case errors.Is(err, ErrPermanent) || errors.Is(err, ErrUnknownProvider) || d.Attempts >= len(retryDelays):
		return n.store.DeliveryFailed(ctx, d.ID, err.Error())
	}
	return n.store.DeliveryRetry(ctx, d.ID, n.now().Add(retryDelays[d.Attempts]), err.Error())
}

func (n *Notifier) validate(c Channel) (Sender, error) {
	if strings.TrimSpace(c.Name) == "" {
		return nil, errors.New("notify: channel name is empty")
	}
	return n.open(c)
}

func (n *Notifier) open(c Channel) (Sender, error) {
	p, err := n.provider(c)
	if err != nil {
		return nil, err
	}
	s, err := p.Open(c.Target)
	if err != nil {
		return nil, fmt.Errorf("notify: %s target: %w", p.Name, err)
	}
	return s, nil
}

func (n *Notifier) provider(c Channel) (Provider, error) {
	p, ok := n.providers[c.Provider]
	if !ok {
		return Provider{}, fmt.Errorf("%w %q", ErrUnknownProvider, c.Provider)
	}
	return p, nil
}
