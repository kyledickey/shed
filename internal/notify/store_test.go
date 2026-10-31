package notify

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/store"
)

func TestStoreOutbox(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "shed.db"), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	channel, err := db.CreateNotificationChannel(ctx, store.NotificationChannel{
		Name: "phone", Provider: "fake", Target: "target", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	outbox := NewStore(db)
	provider := &fakeProvider{}
	n := New(outbox, "", slog.New(slog.NewTextHandler(io.Discard, nil)), provider.provider("fake"))
	message := Message{Title: "Title", Body: "Body", URL: "https://example.com", Project: "Project"}
	for range 2 {
		if err := n.Enqueue(ctx, message); err != nil {
			t.Fatal(err)
		}
	}
	if err := n.deliverDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := provider.got[channel.Target]; len(got) != 2 {
		t.Fatalf("sent %d messages, want 2", len(got))
	} else {
		for _, m := range got {
			if m.Key == "" {
				t.Error("message has no outbox key")
			}
			m.Key = ""
			if m != message {
				t.Errorf("message = %+v, want %+v", m, message)
			}
		}
	}
	if due, err := outbox.DueDeliveries(ctx, time.Now(), 10); err != nil || len(due) != 0 {
		t.Errorf("due after sending = %+v, %v, want none", due, err)
	}
}
