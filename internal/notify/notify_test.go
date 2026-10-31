package notify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// fakeStore is an in-memory outbox.
type fakeStore struct {
	channels   []Channel
	deliveries []*fakeDelivery
	rules      []RuleOverride
	projects   map[string]string
	services   map[string]string // project ID by service ID
	err        error

	servicesErr error
}

func (s *fakeStore) Rules(context.Context) ([]RuleOverride, error) { return s.rules, nil }

func (s *fakeStore) ProjectName(_ context.Context, id string) (string, error) {
	return s.projects[id], nil
}

func (s *fakeStore) ServiceProject(_ context.Context, id string) (string, error) {
	return s.services[id], s.servicesErr
}

type fakeDelivery struct {
	Delivery
	status    string // pending, sent, or failed
	lastError string
}

func (s *fakeStore) Channels(context.Context) ([]Channel, error) { return s.channels, s.err }

func (s *fakeStore) AddDelivery(_ context.Context, d Delivery) error {
	for _, e := range s.deliveries {
		if e.ChannelID == d.ChannelID && e.Key == d.Key {
			return nil
		}
	}
	d.ID = fmt.Sprint(len(s.deliveries) + 1)
	s.deliveries = append(s.deliveries, &fakeDelivery{Delivery: d, status: "pending"})
	return nil
}

func (s *fakeStore) DueDeliveries(_ context.Context, t time.Time, limit int) ([]Delivery, error) {
	var out []Delivery
	for _, d := range s.deliveries {
		if d.status == "pending" && !d.NextAttemptAt.After(t) && len(out) < limit {
			out = append(out, d.Delivery)
		}
	}
	return out, nil
}

func (s *fakeStore) find(id string) *fakeDelivery {
	for _, d := range s.deliveries {
		if d.ID == id {
			return d
		}
	}
	panic("no delivery " + id)
}

func (s *fakeStore) DeliverySent(_ context.Context, id string, _ time.Time) error {
	s.find(id).status = "sent"
	return nil
}

func (s *fakeStore) DeliveryRetry(_ context.Context, id string, next time.Time, msg string) error {
	d := s.find(id)
	d.Attempts++
	d.NextAttemptAt, d.lastError = next, msg
	return nil
}

func (s *fakeStore) DeliveryFailed(_ context.Context, id, msg string) error {
	d := s.find(id)
	d.Attempts++
	d.status, d.lastError = "failed", msg
	return nil
}

func (s *fakeStore) PruneDeliveries(context.Context, time.Time) error { return nil }

// fakeProvider records sends per target and fails with errs[target].
type fakeProvider struct {
	got  map[string][]Message
	errs map[string]error
}

func (f *fakeProvider) provider(name string) Provider {
	return Provider{Name: name, Open: func(target string) (Sender, error) {
		if strings.HasPrefix(target, "bad") {
			return nil, errors.New("target is malformed")
		}
		return senderFunc(func(_ context.Context, m Message) error {
			if err := f.errs[target]; err != nil {
				return err
			}
			if f.got == nil {
				f.got = map[string][]Message{}
			}
			f.got[target] = append(f.got[target], m)
			return nil
		}), nil
	}}
}

type senderFunc func(context.Context, Message) error

func (f senderFunc) Send(ctx context.Context, m Message) error { return f(ctx, m) }

func newNotifier(st *fakeStore, f *fakeProvider) (*Notifier, *time.Time) {
	n := New(st, "", slog.New(slog.NewTextHandler(io.Discard, nil)), f.provider("fake"))
	clock := t0
	n.now = func() time.Time { return clock }
	return n, &clock
}

func TestEnqueue(t *testing.T) {
	st := &fakeStore{channels: []Channel{
		{ID: "a", Name: "a", Provider: "fake", Target: "ta", Enabled: true},
		{ID: "b", Name: "b", Provider: "fake", Target: "tb"},
		{ID: "c", Name: "c", Provider: "fake", Target: "tc", Enabled: true},
	}}
	n, _ := newNotifier(st, &fakeProvider{})
	ctx := context.Background()

	if err := n.Enqueue(ctx, Message{Key: "k", Body: "hi"}); err != nil {
		t.Fatal(err)
	}
	if err := n.Enqueue(ctx, Message{Key: "k", Body: "hi"}); err != nil {
		t.Fatal(err)
	}
	if len(st.deliveries) != 2 || st.deliveries[0].ChannelID != "a" || st.deliveries[1].ChannelID != "c" {
		t.Fatalf("deliveries = %+v, want one each for a and c", st.deliveries)
	}

	// Messages without a key never collide.
	st.deliveries = nil
	for range 2 {
		if err := n.Enqueue(ctx, Message{Body: "hi"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(st.deliveries) != 4 {
		t.Errorf("%d deliveries for two keyless messages, want 4", len(st.deliveries))
	}

	for name, tt := range map[string]struct {
		m      Message
		err    error
		errHas string
	}{
		"empty body":  {m: Message{Body: " \n"}, errHas: "body is empty"},
		"store error": {m: Message{Body: "x"}, err: errors.New("db locked"), errHas: "db locked"},
	} {
		st.err = tt.err
		if err := n.Enqueue(ctx, tt.m); err == nil || !strings.Contains(err.Error(), tt.errHas) {
			t.Errorf("%s: Enqueue error = %v, want %q", name, err, tt.errHas)
		}
	}
}

func TestDeliver(t *testing.T) {
	errTemp := errors.New("boom")
	errPerm := permanent(errors.New("nope"))
	tests := []struct {
		name       string
		channel    Channel
		sendErr    error
		failures   int // sends that fail before the one under test
		wantStatus string
		wantTries  int
		wantNext   time.Duration // after the last failure, if still pending
		wantError  string
	}{
		{name: "sent", channel: Channel{Enabled: true}, wantStatus: "sent"},
		{name: "first retry", channel: Channel{Enabled: true}, sendErr: errTemp, wantStatus: "pending", wantTries: 1, wantNext: time.Minute, wantError: "boom"},
		{name: "last retry", channel: Channel{Enabled: true}, sendErr: errTemp, failures: 4, wantStatus: "pending", wantTries: 5, wantNext: 6 * time.Hour, wantError: "boom"},
		{name: "give up", channel: Channel{Enabled: true}, sendErr: errTemp, failures: 5, wantStatus: "failed", wantTries: 6, wantError: "boom"},
		{name: "permanent", channel: Channel{Enabled: true}, sendErr: errPerm, wantStatus: "failed", wantTries: 1, wantError: "nope"},
		{name: "disabled", channel: Channel{}, wantStatus: "failed", wantTries: 1, wantError: "disabled"},
		{name: "unknown provider", channel: Channel{Enabled: true, Provider: "pager"}, wantStatus: "failed", wantTries: 1, wantError: "unknown provider"},
		{name: "bad target", channel: Channel{Enabled: true, Target: "bad-s3cret"}, wantStatus: "pending", wantTries: 1, wantNext: time.Minute, wantError: "malformed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.channel.ID = "c"
			if tt.channel.Provider == "" {
				tt.channel.Provider = "fake"
			}
			if tt.channel.Target == "" {
				tt.channel.Target = "secret-target"
			}
			// The channel may be disabled after the message was queued.
			enabled := tt.channel.Enabled
			tt.channel.Enabled = true
			st := &fakeStore{channels: []Channel{tt.channel}}
			f := &fakeProvider{errs: map[string]error{"secret-target": tt.sendErr}}
			n, clock := newNotifier(st, f)
			ctx := context.Background()
			if err := n.Enqueue(ctx, Message{Key: "k", Body: "hi"}); err != nil {
				t.Fatal(err)
			}
			st.channels[0].Enabled = enabled
			// Earlier failures walk the delivery through the schedule.
			for range tt.failures {
				*clock = clock.Add(7 * time.Hour)
				if err := n.deliverDue(ctx); err != nil {
					t.Fatal(err)
				}
			}
			*clock = clock.Add(7 * time.Hour)
			if err := n.deliverDue(ctx); err != nil {
				t.Fatal(err)
			}
			d := st.deliveries[0]
			if d.status != tt.wantStatus || d.Attempts != tt.wantTries {
				t.Errorf("status %s after %d attempts, want %s after %d", d.status, d.Attempts, tt.wantStatus, tt.wantTries)
			}
			if tt.wantStatus == "pending" && d.NextAttemptAt != clock.Add(tt.wantNext) {
				t.Errorf("next attempt in %v, want %v", d.NextAttemptAt.Sub(*clock), tt.wantNext)
			}
			if !strings.Contains(d.lastError, tt.wantError) {
				t.Errorf("last error %q, want %q", d.lastError, tt.wantError)
			}
			if strings.Contains(d.lastError, "secret-target") {
				t.Errorf("last error %q leaks the target", d.lastError)
			}
		})
	}
}

func TestRetryNotDueYet(t *testing.T) {
	st := &fakeStore{channels: []Channel{{ID: "c", Provider: "fake", Target: "t", Enabled: true}}}
	f := &fakeProvider{errs: map[string]error{"t": errors.New("boom")}}
	n, clock := newNotifier(st, f)
	ctx := context.Background()
	n.Enqueue(ctx, Message{Key: "k", Body: "hi"})
	n.deliverDue(ctx)
	*clock = clock.Add(30 * time.Second)
	n.deliverDue(ctx)
	if st.deliveries[0].Attempts != 1 {
		t.Errorf("attempts = %d, want 1: retried before its time", st.deliveries[0].Attempts)
	}
}

func TestRestartPicksUpPending(t *testing.T) {
	st := &fakeStore{channels: []Channel{{ID: "c", Provider: "fake", Target: "t", Enabled: true}}}
	f := &fakeProvider{}
	first, _ := newNotifier(st, f)
	ctx := context.Background()
	if err := first.Enqueue(ctx, Message{Key: "k", Body: "hi"}); err != nil {
		t.Fatal(err)
	}

	// A new Notifier over the same store, as after a restart.
	sent := make(chan struct{})
	second := New(st, "", slog.New(slog.NewTextHandler(io.Discard, nil)), Provider{Name: "fake",
		Open: func(string) (Sender, error) {
			return senderFunc(func(context.Context, Message) error { close(sent); return nil }), nil
		}})
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error)
	go func() { done <- second.Run(runCtx) }()
	select {
	case <-sent:
	case <-time.After(5 * time.Second):
		t.Fatal("pending delivery was not sent")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("Run = %v, want context.Canceled", err)
	}
	if st.deliveries[0].status != "sent" {
		t.Errorf("status = %s, want sent", st.deliveries[0].status)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		c       Channel
		wantErr bool
		errIs   error
	}{
		{name: "good", c: Channel{Name: "phone", Provider: "fake", Target: "ok"}},
		{name: "unknown provider", c: Channel{Name: "phone", Provider: "pager", Target: "ok"}, wantErr: true, errIs: ErrUnknownProvider},
		{name: "empty name", c: Channel{Name: " ", Provider: "fake", Target: "ok"}, wantErr: true},
		{name: "bad target", c: Channel{Name: "phone", Provider: "fake", Target: "bad-s3cret"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, _ := newNotifier(&fakeStore{}, &fakeProvider{})
			err := n.Validate(tt.c)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errIs != nil && !errors.Is(err, tt.errIs) {
				t.Errorf("error %v is not %v", err, tt.errIs)
			}
			if err != nil && strings.Contains(err.Error(), tt.c.Target) {
				t.Errorf("error %q leaks the target", err)
			}
		})
	}
}

func TestTest(t *testing.T) {
	f := &fakeProvider{}
	n, _ := newNotifier(&fakeStore{}, f)
	// Disabled and not in the store: Test sends straight to the provider.
	c := Channel{Name: "phone", Provider: "fake", Target: "ta"}
	if err := n.Test(context.Background(), c); err != nil {
		t.Fatalf("Test: %v", err)
	}
	want := Message{Title: "shed", Body: "Test notification from shed."}
	if got := f.got["ta"]; len(got) != 1 || got[0] != want {
		t.Errorf("sent %v, want [%v]", got, want)
	}
}

func TestProvidersSorted(t *testing.T) {
	f := &fakeProvider{}
	n := New(&fakeStore{}, "", nil, f.provider("zeta"), f.provider("alpha"), Hark(nil))
	var names []string
	for _, p := range n.Providers() {
		names = append(names, p.Name)
	}
	if got := strings.Join(names, ","); got != "alpha,hark,zeta" {
		t.Errorf("Providers = %s, want alpha,hark,zeta", got)
	}
}

func TestNewDuplicatePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("New did not panic on duplicate provider names")
		}
	}()
	New(&fakeStore{}, "", nil, Hark(nil), Hark(nil))
}
