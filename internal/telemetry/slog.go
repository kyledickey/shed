package telemetry

import (
	"context"
	"errors"
	"log/slog"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/log"
)

// slogHandler fans records out to next and to an OpenTelemetry bridge.
type slogHandler struct {
	next   slog.Handler
	bridge slog.Handler
}

// NewSlogHandler returns a handler that passes every record to next unchanged
// and also emits it through lp, carrying the trace and span IDs of the
// record's context. It is enabled when next is, so next's level applies to
// both.
func NewSlogHandler(next slog.Handler, lp log.LoggerProvider) slog.Handler {
	return slogHandler{
		next:   next,
		bridge: otelslog.NewHandler("shed", otelslog.WithLoggerProvider(lp)),
	}
}

func (h slogHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h slogHandler) Handle(ctx context.Context, r slog.Record) error {
	err := h.next.Handle(ctx, r)
	if !h.next.Enabled(ctx, r.Level) {
		return err
	}
	return errors.Join(err, h.bridge.Handle(ctx, r))
}

func (h slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return slogHandler{next: h.next.WithAttrs(attrs), bridge: h.bridge.WithAttrs(attrs)}
}

func (h slogHandler) WithGroup(name string) slog.Handler {
	return slogHandler{next: h.next.WithGroup(name), bridge: h.bridge.WithGroup(name)}
}
