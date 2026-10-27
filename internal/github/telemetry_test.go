package github

import (
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRequestSpansCarryNoSecrets(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	_, c := newFakeGitHub(t)
	if _, err := c.ExchangeUser(t.Context(), "good", "http://x/cb"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Repos(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CloneURL(t.Context(), "octo/app"); err != nil {
		t.Fatal(err)
	}

	if len(spans.Ended()) < 3 {
		t.Fatalf("got %d request spans, want at least 3", len(spans.Ended()))
	}
	for _, s := range spans.Ended() {
		for _, kv := range s.Attributes() {
			v := kv.Value.String()
			for _, secret := range []string{"good", "csecret", "user-token", "inst-token"} {
				if strings.Contains(v, secret) {
					t.Errorf("span %q attribute %s = %q carries %q", s.Name(), kv.Key, v, secret)
				}
			}
			if kv.Key == "url.full" && strings.Contains(v, "?") {
				t.Errorf("span %q url.full = %q keeps its query", s.Name(), v)
			}
		}
	}
}
