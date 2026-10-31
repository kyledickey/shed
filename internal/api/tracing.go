package api

import (
	"errors"
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"

	"github.com/kyledickey/shed/internal/control"
)

const scope = "github.com/kyledickey/shed/internal/api"

// traced serves h, a mux, with a server span per request named by the
// matched route pattern, such as "GET /api/services/{id}". Incoming trace
// context starts a new trace linked to it, since clients are untrusted.
// Event streams and protocol upgrades get no span and no HTTP metrics: they
// last as long as the client stays connected, which would swamp request
// durations.
func traced(h http.Handler) http.Handler {
	return otelhttp.NewHandler(h, "shed",
		otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true }),
		otelhttp.WithFilter(func(r *http.Request) bool { return !streams(r) }))
}

// streams reports whether r asks for a response that stays open.
func streams(r *http.Request) bool {
	return r.Header.Get("Upgrade") != "" ||
		r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

// delivery is what the webhook learns about a delivery as it handles it.
type delivery struct {
	// event is the X-GitHub-Event of a delivery whose signature is valid,
	// so that unverified requests cannot add label values.
	event   string
	ignored bool // a valid delivery shed does not act on
}

// webhook handles a GitHub webhook delivery in its own span and counts it
// in shed_webhook_deliveries_total by event and outcome.
func (s *Server) webhook(w http.ResponseWriter, r *http.Request) error {
	ctx, span := otel.Tracer(scope).Start(r.Context(), "github webhook")
	defer span.End()
	d := delivery{event: "unverified"}
	err := s.receiveWebhook(w, r.WithContext(ctx), &d)
	outcome := webhookOutcome(err, d.ignored)
	span.SetAttributes(attribute.String("github.event", d.event), attribute.String("outcome", outcome))
	if d.event != "unverified" {
		span.SetAttributes(attribute.String("github.delivery", r.Header.Get("X-GitHub-Delivery")))
	}
	if outcome == "failed" {
		span.SetStatus(codes.Error, err.Error())
	}
	s.webhookDeliveries.Add(ctx, 1, metric.WithAttributes(
		attribute.String("event", d.event), attribute.String("outcome", outcome)))
	return err
}

// webhookOutcome names the outcome of a delivery the webhook answered with
// err.
func webhookOutcome(err error, ignored bool) string {
	if err == nil {
		if ignored {
			return "ignored"
		}
		return "accepted"
	}
	status := http.StatusInternalServerError
	var he *httpError
	if errors.As(err, &he) {
		status = he.status
	} else if e, ok := control.Explain(err); ok {
		status = statusOf(e.Kind)
	}
	switch {
	case status >= 500:
		return "failed"
	case status == http.StatusUnauthorized:
		return "unauthorized"
	case status == http.StatusTooManyRequests:
		return "throttled"
	default:
		return "rejected"
	}
}
