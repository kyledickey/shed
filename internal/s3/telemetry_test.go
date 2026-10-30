package s3

import (
	"context"
	"io"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRequestSpansCarryNoQueryOrCredentials(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	_, cfg := newFakeS3(t, "bucket", "AKIDTEST")
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := c.Put(ctx, "k", strings.NewReader("hello"), -1); err != nil {
		t.Fatal(err)
	}
	rc, err := c.Get(ctx, "k")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, rc)
	rc.Close()

	var urls int
	for _, s := range spans.Ended() {
		for _, kv := range s.Attributes() {
			v := kv.Value.String()
			if strings.Contains(v, "AKIDTEST") || strings.Contains(v, cfg.SecretAccessKey) {
				t.Errorf("span %q attribute %s = %q carries a credential", s.Name(), kv.Key, v)
			}
			if kv.Key == "url.full" {
				urls++
				if strings.Contains(v, "?") {
					t.Errorf("span %q url.full = %q keeps its query", s.Name(), v)
				}
			}
		}
	}
	if urls == 0 {
		t.Fatal("no request spans with url.full")
	}
}
