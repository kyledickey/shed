package mcp

import (
	"net/http"
	"testing"

	"go.opentelemetry.io/otel/attribute"

	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/telemetry/telemetrytest"
)

func TestToolCallsAreTracedAndCounted(t *testing.T) {
	rec := telemetrytest.Install(t)
	agent := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := audit.WithActor(r.Context(), audit.Actor{Kind: audit.Agent, ClientID: "c1", ClientName: "Claude"})
			h.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	cs := connectWith(t, &fakeBackend{}, agent)
	call(t, cs, "get_service", map[string]any{"service_id": "secret-looking-id"})

	s := rec.Span("tools/call get_service")
	if s == nil {
		t.Fatal("no span for the tool call")
	}
	for _, kv := range s.Attributes() {
		if kv.Value.String() == "secret-looking-id" {
			t.Errorf("tool span records the argument %s", kv.Key)
		}
	}
	if n := rec.Sum(t, "shed_mcp_tool_calls_total",
		attribute.String("tool", "get_service"), attribute.String("client", "Claude")); n != 1 {
		t.Errorf("shed_mcp_tool_calls_total{get_service, Claude} = %d, want 1", n)
	}
}
