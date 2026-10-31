package control

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/notify"
)

// fakeNotifyProvider accepts any target except those starting with "bad",
// and fails to send to "down".
func fakeNotifyProvider() notify.Provider {
	return notify.Provider{Name: "fake", Open: func(target string) (notify.Sender, error) {
		if strings.HasPrefix(target, "bad") {
			return nil, errors.New("target is malformed")
		}
		return sender(func(context.Context, notify.Message) error {
			if target == "down" {
				return errors.New("fake: 503 unavailable")
			}
			return nil
		}), nil
	}}
}

type sender func(context.Context, notify.Message) error

func (f sender) Send(ctx context.Context, m notify.Message) error { return f(ctx, m) }

func TestNotificationChannels(t *testing.T) {
	f := newFixture(t)
	user := audit.Actor{Kind: audit.User, Name: "octocat", IP: "203.0.113.9"}
	ctx := audit.WithActor(context.Background(), user)

	if got := f.plane.NotificationProviders(); len(got) != 1 || got[0] != "fake" {
		t.Errorf("providers = %v, want [fake]", got)
	}
	for _, in := range []NotificationChannelInput{
		{Name: "", Provider: "fake", Target: "ok"},
		{Name: "x", Provider: "pager", Target: "ok"},
		{Name: "x", Provider: "fake", Target: "bad-s3cret"},
	} {
		_, err := f.plane.CreateNotificationChannel(ctx, in)
		if e, ok := Explain(err); !ok || e.Kind != ErrInvalid {
			t.Errorf("Create(%+v) = %v, want ErrInvalid", in, err)
		}
	}
	f.audit.take()

	c, err := f.plane.CreateNotificationChannel(ctx, NotificationChannelInput{Name: " phone ", Provider: "fake", Target: "s3cret-target", Enabled: true})
	if err != nil || c.Name != "phone" || !c.Enabled {
		t.Fatalf("Create = %+v, %v", c, err)
	}

	// An empty target keeps the stored one.
	off := false
	if _, err := f.plane.UpdateNotificationChannel(ctx, c.ID, NotificationChannelPatch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.NotificationChannel(ctx, c.ID)
	if got.Target != "s3cret-target" || got.Enabled || got.Name != "phone" {
		t.Errorf("after update: %+v", got)
	}
	if _, err := f.plane.UpdateNotificationChannel(ctx, c.ID, NotificationChannelPatch{Target: "bad-new"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("Update with a bad target = %v, want ErrInvalid", err)
	}

	// Testing works on a disabled channel and reports why it failed.
	if err := f.plane.TestNotificationChannel(ctx, c.ID); err != nil {
		t.Errorf("Test = %v", err)
	}
	if _, err := f.plane.UpdateNotificationChannel(ctx, c.ID, NotificationChannelPatch{Target: "down"}); err != nil {
		t.Fatal(err)
	}
	if err := f.plane.TestNotificationChannel(ctx, c.ID); err == nil || !strings.Contains(err.Error(), "503 unavailable") || !errors.Is(err, ErrInvalid) {
		t.Errorf("Test of a down channel = %v, want ErrInvalid with the reason", err)
	}
	if err := f.plane.TestNotificationChannel(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Test(missing) = %v, want ErrNotFound", err)
	}

	if err := f.plane.DeleteNotificationChannel(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.plane.DeleteNotificationChannel(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}

	var actions []string
	for _, e := range f.audit.take() {
		actions = append(actions, e.Action+" "+string(e.Outcome))
		if d := detailJSON(t, e.Event); strings.Contains(d, "s3cret") || strings.Contains(d, "down") || strings.Contains(d, "bad-new") {
			t.Errorf("%s detail %s holds a target", e.Action, d)
		}
		if e.Actor != user {
			t.Errorf("%s actor = %+v", e.Action, e.Actor)
		}
	}
	want := "notification.channel.create ok,notification.channel.update ok,notification.channel.update failed," +
		"notification.channel.test ok,notification.channel.update ok,notification.channel.test failed," +
		"notification.channel.delete ok,notification.channel.delete failed"
	if got := strings.Join(actions, ","); got != want {
		t.Errorf("audit actions:\n got %s\nwant %s", got, want)
	}
}
