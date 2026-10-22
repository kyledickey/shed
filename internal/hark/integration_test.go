//go:build integration

package hark

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestIntegrationSend sends a real notification to the Hark service named
// by SHED_HARK_TEST_URL. It is skipped when the variable is unset.
func TestIntegrationSend(t *testing.T) {
	webhook := os.Getenv("SHED_HARK_TEST_URL")
	if webhook == "" {
		t.Skip("SHED_HARK_TEST_URL is not set")
	}
	c, err := New(webhook, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Send(context.Background(), Notification{
		Body:    "Test notification from the shed integration tests.",
		Title:   "shed",
		Project: "shed tests",
	}, "shed-test-"+time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if res.EventID == "" {
		t.Error("EventID is empty")
	}
	t.Logf("event %s delivered to %d devices", res.EventID, res.Delivered)
}
