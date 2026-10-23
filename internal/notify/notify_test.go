package notify

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type fakeStore struct {
	endpoints []Endpoint
	err       error
}

func (s fakeStore) Endpoints(context.Context) ([]Endpoint, error) { return s.endpoints, s.err }

// recorder collects messages per target and fails sends to targets listed
// in fail.
type recorder struct {
	mu   sync.Mutex
	got  map[string][]Message
	fail map[string]bool
}

func (r *recorder) provider(name string, kind Kind) Provider {
	return Provider{
		Name: name,
		Kind: kind,
		Open: func(target string) (Sender, error) {
			if strings.HasPrefix(target, "bad") {
				return nil, errors.New("target is malformed")
			}
			return senderFunc(func(_ context.Context, m Message) error {
				if r.fail[target] {
					return errors.New("boom")
				}
				r.mu.Lock()
				defer r.mu.Unlock()
				if r.got == nil {
					r.got = map[string][]Message{}
				}
				r.got[target] = append(r.got[target], m)
				return nil
			}), nil
		},
	}
}

type senderFunc func(context.Context, Message) error

func (f senderFunc) Send(ctx context.Context, m Message) error { return f(ctx, m) }

func newNotifier(r *recorder, endpoints []Endpoint, storeErr error) *Notifier {
	return New(fakeStore{endpoints, storeErr},
		r.provider("fake", KindPush), r.provider("mail", KindEmail))
}

func TestSend(t *testing.T) {
	msg := Message{Key: "k", Title: "t", Body: "hello"}
	tests := []struct {
		name      string
		endpoints []Endpoint
		fail      map[string]bool
		storeErr  error
		msg       Message
		delivered []string
		errHas    []string
		errIs     error
	}{
		{
			name: "only enabled",
			endpoints: []Endpoint{
				{ID: "1", Name: "a", Provider: "fake", Target: "ta", Enabled: true},
				{ID: "2", Name: "b", Provider: "fake", Target: "tb"},
				{ID: "3", Name: "c", Provider: "mail", Target: "tc", Enabled: true},
			},
			msg:       msg,
			delivered: []string{"ta", "tc"},
		},
		{
			name: "one failure does not block others",
			endpoints: []Endpoint{
				{ID: "1", Name: "a", Provider: "fake", Target: "ta", Enabled: true},
				{ID: "2", Name: "b", Provider: "fake", Target: "tb", Enabled: true},
				{ID: "3", Name: "c", Provider: "fake", Target: "tc", Enabled: true},
			},
			fail:      map[string]bool{"tb": true},
			msg:       msg,
			delivered: []string{"ta", "tc"},
			errHas:    []string{`"b"`, "(2)", "boom"},
		},
		{
			name: "unknown provider",
			endpoints: []Endpoint{
				{ID: "1", Name: "a", Provider: "fake", Target: "ta", Enabled: true},
				{ID: "2", Name: "gone", Provider: "pager", Target: "secret-target", Enabled: true},
			},
			msg:       msg,
			delivered: []string{"ta"},
			errHas:    []string{`"gone"`, "(2)"},
			errIs:     ErrUnknownProvider,
		},
		{
			name:      "empty body",
			endpoints: []Endpoint{{ID: "1", Name: "a", Provider: "fake", Target: "ta", Enabled: true}},
			msg:       Message{Body: "  \n"},
			errHas:    []string{"body is empty"},
		},
		{
			name:     "store error",
			storeErr: errors.New("db locked"),
			msg:      msg,
			errHas:   []string{"db locked"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &recorder{fail: tt.fail}
			err := newNotifier(r, tt.endpoints, tt.storeErr).Send(context.Background(), tt.msg)

			if len(tt.errHas) == 0 && err != nil {
				t.Fatalf("Send: %v", err)
			}
			if len(tt.errHas) > 0 && err == nil {
				t.Fatal("Send succeeded, want error")
			}
			for _, s := range tt.errHas {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("error %q does not contain %q", err, s)
				}
			}
			if tt.errIs != nil && !errors.Is(err, tt.errIs) {
				t.Errorf("error %v is not %v", err, tt.errIs)
			}
			if err != nil && strings.Contains(err.Error(), "secret-target") {
				t.Errorf("error %q leaks the target", err)
			}
			if len(r.got) != len(tt.delivered) {
				t.Errorf("delivered to %v, want %v", r.got, tt.delivered)
			}
			for _, target := range tt.delivered {
				if got := r.got[target]; len(got) != 1 || got[0] != tt.msg {
					t.Errorf("target %s got %v, want [%v]", target, got, tt.msg)
				}
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		e       Endpoint
		wantErr bool
		errIs   error
	}{
		{name: "good", e: Endpoint{Name: "phone", Provider: "fake", Target: "ok"}},
		{name: "unknown provider", e: Endpoint{Name: "phone", Provider: "pager", Target: "ok"}, wantErr: true, errIs: ErrUnknownProvider},
		{name: "empty name", e: Endpoint{Name: " ", Provider: "fake", Target: "ok"}, wantErr: true},
		{name: "bad target", e: Endpoint{Name: "phone", Provider: "fake", Target: "bad-s3cret"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newNotifier(&recorder{}, nil, nil).Validate(tt.e)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errIs != nil && !errors.Is(err, tt.errIs) {
				t.Errorf("error %v is not %v", err, tt.errIs)
			}
			if err != nil && strings.Contains(err.Error(), tt.e.Target) {
				t.Errorf("error %q leaks the target", err)
			}
		})
	}
}

func TestTest(t *testing.T) {
	r := &recorder{}
	n := newNotifier(r, nil, nil)
	e := Endpoint{Name: "phone", Provider: "fake", Target: "ta"}
	if err := n.Test(context.Background(), e); err != nil {
		t.Fatalf("Test: %v", err)
	}
	want := Message{Title: "shed", Body: "Test notification from shed."}
	if got := r.got["ta"]; len(got) != 1 || got[0] != want {
		t.Errorf("sent %v, want [%v]", got, want)
	}
}

func TestKind(t *testing.T) {
	n := newNotifier(&recorder{}, nil, nil)
	for provider, want := range map[string]Kind{"fake": KindPush, "mail": KindEmail} {
		got, err := n.Kind(Endpoint{Provider: provider})
		if err != nil || got != want {
			t.Errorf("Kind(%s) = %q, %v; want %q", provider, got, err, want)
		}
	}
	if _, err := n.Kind(Endpoint{Provider: "pager"}); !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("Kind(pager) error = %v, want ErrUnknownProvider", err)
	}
}

func TestProvidersSorted(t *testing.T) {
	r := &recorder{}
	n := New(fakeStore{},
		r.provider("zeta", KindPush), r.provider("alpha", KindEmail), Hark(nil))
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
	New(fakeStore{}, Hark(nil), Hark(nil))
}
