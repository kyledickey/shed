// Package notify owns shed's notification types and delivers messages to
// the endpoints a user configured. Providers are adapters registered by
// name; each one delivers on a single Kind, such as push or email. Adding a
// provider means writing one adapter and passing it to New.
package notify

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Kind is the channel a provider delivers on.
type Kind string

// Kinds of delivery channel.
const (
	KindPush  Kind = "push"
	KindEmail Kind = "email"
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

// Endpoint is one configured destination.
type Endpoint struct {
	ID       string
	Name     string
	Provider string
	// Target is the provider-specific address, such as a Hark webhook URL.
	// It may be a secret, so it never appears in errors or logs.
	Target  string
	Enabled bool
}

// Sender delivers messages to one endpoint. Adapters implement it.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// Provider describes one adapter: its name, the kind it delivers, and how to
// open a Sender for a target.
type Provider struct {
	Name string
	Kind Kind
	// Open validates target and returns a Sender for it without contacting
	// the provider. Its errors must not contain target.
	Open func(target string) (Sender, error)
}

// ErrUnknownProvider is returned for an endpoint whose provider is not
// registered.
var ErrUnknownProvider = errors.New("notify: unknown provider")

// ErrNotDelivered is returned when a provider accepted a message but no
// device or recipient received it.
var ErrNotDelivered = errors.New("notify: no device received the message")

// Store loads the configured endpoints.
type Store interface {
	Endpoints(ctx context.Context) ([]Endpoint, error)
}

// Notifier sends messages to every enabled endpoint. It is safe for
// concurrent use.
type Notifier struct {
	store     Store
	providers map[string]Provider
}

// New returns a Notifier over the endpoints in store, delivering through
// providers. It panics if two providers share a name, which is a wiring bug.
func New(store Store, providers ...Provider) *Notifier {
	m := make(map[string]Provider, len(providers))
	for _, p := range providers {
		if _, dup := m[p.Name]; dup {
			panic("notify: duplicate provider " + p.Name)
		}
		m[p.Name] = p
	}
	return &Notifier{store: store, providers: m}
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

// Kind returns the kind of channel e delivers on.
func (n *Notifier) Kind(e Endpoint) (Kind, error) {
	p, err := n.provider(e)
	if err != nil {
		return "", err
	}
	return p.Kind, nil
}

// Validate reports whether e has a name, a registered provider, and a target
// that provider accepts. It does not contact the provider.
func (n *Notifier) Validate(e Endpoint) error {
	_, err := n.validate(e)
	return err
}

// Test sends a fixed test message to e, even if e is disabled.
func (n *Notifier) Test(ctx context.Context, e Endpoint) error {
	s, err := n.validate(e)
	if err != nil {
		return err
	}
	return s.Send(ctx, Message{Title: "shed", Body: "Test notification from shed."})
}

// Send delivers m to every enabled endpoint concurrently. A failing endpoint
// does not stop the others; Send returns their errors joined.
func (n *Notifier) Send(ctx context.Context, m Message) error {
	if strings.TrimSpace(m.Body) == "" {
		return errors.New("notify: message body is empty")
	}
	endpoints, err := n.store.Endpoints(ctx)
	if err != nil {
		return fmt.Errorf("notify: load endpoints: %w", err)
	}
	errs := make([]error, len(endpoints))
	var wg sync.WaitGroup
	for i, e := range endpoints {
		if !e.Enabled {
			continue
		}
		wg.Go(func() {
			if err := n.send(ctx, e, m); err != nil {
				errs[i] = fmt.Errorf("notify: endpoint %q (%s): %w", e.Name, e.ID, err)
			}
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

func (n *Notifier) send(ctx context.Context, e Endpoint, m Message) error {
	s, err := n.open(e)
	if err != nil {
		return err
	}
	return s.Send(ctx, m)
}

func (n *Notifier) validate(e Endpoint) (Sender, error) {
	if strings.TrimSpace(e.Name) == "" {
		return nil, errors.New("notify: endpoint name is empty")
	}
	return n.open(e)
}

func (n *Notifier) open(e Endpoint) (Sender, error) {
	p, err := n.provider(e)
	if err != nil {
		return nil, err
	}
	s, err := p.Open(e.Target)
	if err != nil {
		return nil, fmt.Errorf("notify: %s target: %w", p.Name, err)
	}
	return s, nil
}

func (n *Notifier) provider(e Endpoint) (Provider, error) {
	p, ok := n.providers[e.Provider]
	if !ok {
		return Provider{}, fmt.Errorf("%w %q", ErrUnknownProvider, e.Provider)
	}
	return p, nil
}
