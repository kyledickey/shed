package onepassword

import (
	"context"
	"slices"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestCallsAreTracedWithoutTokenOrValues(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	ctx := context.Background()
	c, fake := newClient()
	const token = "ops_serviceaccounttoken"
	fake.tokens[token] = true
	id, err := c.CreateItem(ctx, token, "v", Item{Title: "web", Variables: map[string]string{"API_KEY": "supersecretvalue"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Variables(ctx, token, "v", []string{id}); err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, s := range spans.Ended() {
		names = append(names, s.Name())
		texts := []string{s.Name(), s.Status().Description}
		for _, kv := range s.Attributes() {
			texts = append(texts, kv.Value.String())
		}
		for _, text := range texts {
			if strings.Contains(text, token) || strings.Contains(text, "supersecretvalue") {
				t.Errorf("span %q carries a secret: %q", s.Name(), text)
			}
		}
	}
	for _, want := range []string{"1password sign in", "1password create item", "1password get items"} {
		if !slices.Contains(names, want) {
			t.Errorf("spans %q lack %q", names, want)
		}
	}
}

func TestCallsIgnoreCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err, _ := traced(ctx, "test", func(ctx context.Context) (error, error) { return ctx.Err(), nil })
	if err != nil {
		t.Errorf("call saw %v", err)
	}
}
