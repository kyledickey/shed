package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/notify"
	"github.com/kyledickey/shed/internal/store"
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

func TestNotificationChannelsAPI(t *testing.T) {
	f := newFixture(t)

	providers := f.do("GET", "/api/notifications/providers", "")
	if providers.Code != 200 || strings.TrimSpace(providers.Body.String()) != `["fake"]` {
		t.Errorf("providers = %d %s", providers.Code, providers.Body)
	}

	bad := f.decode(f.do("POST", "/api/notifications/channels", `{"name":"x","provider":"fake","target":"bad-1"}`), http.StatusBadRequest)
	if !strings.Contains(bad["error"].(string), "malformed") {
		t.Errorf("error = %v", bad["error"])
	}
	c := f.decode(f.do("POST", "/api/notifications/channels",
		`{"name":"phone","provider":"fake","target":"s3cret-target","enabled":true}`), http.StatusCreated)
	id := c["id"].(string)
	if c["name"] != "phone" || c["enabled"] != true {
		t.Errorf("channel = %v", c)
	}

	rec := f.do("GET", "/api/notifications/channels", "")
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "s3cret") || !strings.Contains(rec.Body.String(), id) {
		t.Errorf("list = %d %s", rec.Code, rec.Body)
	}

	patched := f.decode(f.do("PATCH", "/api/notifications/channels/"+id, `{"name":"pager","enabled":false}`), http.StatusOK)
	if patched["name"] != "pager" || patched["enabled"] != false {
		t.Errorf("patched = %v", patched)
	}
	if got, _ := f.st.NotificationChannel(context.Background(), id); got.Target != "s3cret-target" {
		t.Errorf("target = %q, want it kept", got.Target)
	}

	if rec := f.do("POST", "/api/notifications/channels/"+id+"/test", ""); rec.Code != http.StatusNoContent {
		t.Errorf("test = %d %s", rec.Code, rec.Body)
	}
	f.do("PATCH", "/api/notifications/channels/"+id, `{"target":"down"}`)
	down := f.decode(f.do("POST", "/api/notifications/channels/"+id+"/test", ""), http.StatusBadRequest)
	if !strings.Contains(down["error"].(string), "503 unavailable") {
		t.Errorf("test error = %v", down["error"])
	}

	err := f.st.EnqueueNotification(context.Background(), store.NotificationDelivery{ChannelID: id, Key: "k", Title: "t", Body: "b", NextAttemptAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	rec = f.do("GET", "/api/notifications/deliveries?limit=5", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"channelName":"pager"`) || !strings.Contains(rec.Body.String(), `"status":"pending"`) {
		t.Errorf("deliveries = %d %s", rec.Code, rec.Body)
	}
	if rec := f.do("GET", "/api/notifications/deliveries?limit=0x", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("bad limit = %d", rec.Code)
	}
	if rec := f.do("DELETE", "/api/notifications/channels/"+id, ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete = %d", rec.Code)
	}
	if rec := f.do("DELETE", "/api/notifications/channels/"+id, ""); rec.Code != http.StatusNotFound {
		t.Errorf("second delete = %d, want 404", rec.Code)
	}
}
