//go:build integration

package notify

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestIntegrationHark sends a real notification to the Hark service named by
// SHED_HARK_TEST_URL. It is skipped when the variable is unset.
func TestIntegrationHark(t *testing.T) {
	webhook := os.Getenv("SHED_HARK_TEST_URL")
	if webhook == "" {
		t.Skip("SHED_HARK_TEST_URL is not set")
	}
	s, err := Hark(nil).Open(webhook)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Send(context.Background(), Message{
		Title:   "shed",
		Project: "shed tests",
		Body:    "Test notification from the shed integration tests.",
		Key:     "shed-test-" + time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
}
