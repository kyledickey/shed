package telemetry

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/trace"
)

type recorder struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (r *recorder) Export(_ context.Context, rs []sdklog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rec := range rs {
		r.records = append(r.records, rec.Clone())
	}
	return nil
}
func (r *recorder) Shutdown(context.Context) error   { return nil }
func (r *recorder) ForceFlush(context.Context) error { return nil }

func TestSlogHandlerForwardsToNextAndBridge(t *testing.T) {
	rec := &recorder{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(rec)))
	var buf bytes.Buffer
	next := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	log := slog.New(NewSlogHandler(next, lp)).With("app", "shed").WithGroup("req")

	tp := trace.NewTracerProvider()
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	defer span.End()

	log.DebugContext(ctx, "below level")
	log.InfoContext(ctx, "hello", "id", 7)

	if got := buf.String(); !strings.Contains(got, `msg=hello app=shed req.id=7`) || strings.Contains(got, "below level") {
		t.Errorf("next output = %q", got)
	}
	if len(rec.records) != 1 {
		t.Fatalf("bridge got %d records, want 1 (next's level applies)", len(rec.records))
	}
	got := rec.records[0]
	if got.Body().AsString() != "hello" {
		t.Errorf("body = %q", got.Body().AsString())
	}
	if got.TraceID() != span.SpanContext().TraceID() || got.SpanID() != span.SpanContext().SpanID() {
		t.Errorf("trace/span = %s/%s, want %s/%s", got.TraceID(), got.SpanID(), span.SpanContext().TraceID(), span.SpanContext().SpanID())
	}
}

func TestSlogHandlerWithoutSpan(t *testing.T) {
	rec := &recorder{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(rec)))
	h := NewSlogHandler(slog.NewTextHandler(&bytes.Buffer{}, nil), lp)
	slog.New(h).Info("plain")
	if len(rec.records) != 1 || rec.records[0].TraceID().IsValid() {
		t.Errorf("records = %v, want one without a trace ID", rec.records)
	}
}
