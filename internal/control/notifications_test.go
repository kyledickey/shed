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
	user := audit.Actor{Kind: audit.User, Name: "octocat"}
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

func TestNotificationRules(t *testing.T) {
	f := newFixture(t)
	user := audit.Actor{Kind: audit.User, Name: "octocat"}
	ctx := audit.WithActor(context.Background(), user)
	find := func(id string) NotificationRule {
		t.Helper()
		rs, err := f.plane.NotificationRules(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rs {
			if r.ID == id {
				return r
			}
		}
		t.Fatalf("no rule %s", id)
		return NotificationRule{}
	}

	if r := find("service-crash-loop"); !r.Enabled || !r.Available || *r.Threshold != 3 || r.Unit != "crashes" {
		t.Errorf("default crash loop = %+v", r)
	}

	off, five, bad, secs := false, 5.0, 2.5, 10
	if _, err := f.plane.UpdateNotificationRule(ctx, "service-crash-loop", NotificationRulePatch{Threshold: &five}); err != nil {
		t.Fatal(err)
	}
	// A later patch keeps the earlier threshold.
	if _, err := f.plane.UpdateNotificationRule(ctx, "service-crash-loop", NotificationRulePatch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if r := find("service-crash-loop"); r.Enabled || *r.Threshold != 5 {
		t.Errorf("crash loop = %+v, want disabled with threshold 5", r)
	}
	f.audit.take()

	for _, tt := range []struct {
		id    string
		patch NotificationRulePatch
		want  error
	}{
		{"nope", NotificationRulePatch{Enabled: &off}, ErrNotFound},
		{"service-crash-loop", NotificationRulePatch{Threshold: &bad}, ErrInvalid},
		{"deploy-failed", NotificationRulePatch{Threshold: &five}, ErrInvalid},
		{"service-crash-loop", NotificationRulePatch{ForSeconds: &secs}, ErrInvalid},
	} {
		_, err := f.plane.UpdateNotificationRule(ctx, tt.id, tt.patch)
		if !errors.Is(err, tt.want) {
			t.Errorf("Update(%s, %+v) = %v, want %v", tt.id, tt.patch, err, tt.want)
		}
	}
	if r := find("deploy-failed"); !r.Enabled {
		t.Errorf("a rejected patch changed deploy-failed: %+v", r)
	}
	for _, e := range f.audit.take() {
		if e.Action != "notification.rule.update" || e.Outcome == audit.OK {
			t.Errorf("audit = %s %s, want failed notification.rule.update", e.Action, e.Outcome)
		}
	}
}

func TestMetricNotificationRules(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	find := func(id string) NotificationRule {
		t.Helper()
		rs, err := f.plane.NotificationRules(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rs {
			if r.ID == id {
				return r
			}
		}
		t.Fatalf("no rule %s", id)
		return NotificationRule{}
	}

	// Telemetry is off: metric rules are unavailable, event rules are not.
	f.tel.s.Enabled = false
	disk := find("shed-host-disk")
	if disk.Available || !disk.NeedsTelemetry || !disk.Enabled || *disk.Threshold != 85 || disk.Unit != "%" || disk.ForSeconds != nil {
		t.Errorf("disk = %+v", disk)
	}
	if r := find("shed-telemetry-drops"); r.Threshold != nil || *r.ForSeconds != 300 {
		t.Errorf("drops = %+v", r)
	}
	if r := find("deploy-failed"); !r.Available || r.NeedsTelemetry {
		t.Errorf("deploy-failed = %+v", r)
	}

	ten, over, secs, tooLong := 10.0, 100.5, 120, 86401
	// An event rule doesn't touch Grafana.
	if _, err := f.plane.UpdateNotificationRule(ctx, "service-crash-loop", NotificationRulePatch{Threshold: &ten}); err != nil {
		t.Fatal(err)
	}
	if f.tel.applied != 0 {
		t.Errorf("event rule applied %d times", f.tel.applied)
	}

	f.tel.s.Enabled = true
	r, err := f.plane.UpdateNotificationRule(ctx, "shed-host-memory", NotificationRulePatch{Threshold: &ten, ForSeconds: &secs})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Available || *r.Threshold != 10 || *r.ForSeconds != 120 || f.tel.applied != 1 {
		t.Errorf("memory = %+v, applied %d", r, f.tel.applied)
	}

	// A failed apply reports the error and keeps the override.
	f.tel.applyErr = errors.New("reload: 503")
	_, err = f.plane.UpdateNotificationRule(ctx, "shed-http-latency", NotificationRulePatch{Threshold: &ten})
	if !errors.Is(err, ErrUpstream) || !strings.Contains(err.Error(), "reload: 503") {
		t.Errorf("failed apply = %v, want ErrUpstream with the reason", err)
	}
	if r := find("shed-http-latency"); *r.Threshold != 10 {
		t.Errorf("latency = %+v, want the override kept", r)
	}
	f.tel.applyErr = nil

	applied := f.tel.applied
	for _, tt := range []struct {
		id    string
		patch NotificationRulePatch
	}{
		{"shed-host-disk", NotificationRulePatch{Threshold: &over}},
		{"shed-host-disk", NotificationRulePatch{ForSeconds: &secs}},
		{"shed-telemetry-drops", NotificationRulePatch{Threshold: &ten}},
		{"shed-host-memory", NotificationRulePatch{ForSeconds: &tooLong}},
	} {
		if _, err := f.plane.UpdateNotificationRule(ctx, tt.id, tt.patch); !errors.Is(err, ErrInvalid) {
			t.Errorf("Update(%s, %+v) = %v, want ErrInvalid", tt.id, tt.patch, err)
		}
	}
	if f.tel.applied != applied {
		t.Errorf("rejected patches applied %d times", f.tel.applied-applied)
	}
}
