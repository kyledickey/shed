package observability

import (
	"context"

	"github.com/kyledickey/shed/internal/otlp"
	"github.com/kyledickey/shed/internal/victoria"
)

// StreamFields are the VictoriaLogs stream fields of every record shed
// writes.
var StreamFields = []string{"shed.project.id", "shed.service.id", "shed.source"}

// OTLPStore accepts a forwarded OTLP request. *victoria.Metrics and
// *victoria.Traces implement it.
type OTLPStore interface {
	ForwardOTLP(ctx context.Context, body []byte) error
}

// OTLPLogStore accepts a forwarded OTLP logs request. *victoria.Logs
// implements it.
type OTLPLogStore interface {
	ForwardOTLP(ctx context.Context, body []byte, streamFields []string) error
}

var (
	_ OTLPStore    = (*victoria.Metrics)(nil)
	_ OTLPStore    = (*victoria.Traces)(nil)
	_ OTLPLogStore = (*victoria.Logs)(nil)
)

// NewForwarder returns the gateway's forwarder: traces to VictoriaTraces,
// metrics to VictoriaMetrics, and logs to VictoriaLogs with shed's stream
// fields.
func NewForwarder(metrics, traces OTLPStore, logs OTLPLogStore) otlp.Forwarder {
	return forwarder{metrics: metrics, traces: traces, logs: logs}
}

type forwarder struct {
	metrics, traces OTLPStore
	logs            OTLPLogStore
}

func (f forwarder) Traces(ctx context.Context, body []byte) error {
	return f.traces.ForwardOTLP(ctx, body)
}

func (f forwarder) Metrics(ctx context.Context, body []byte) error {
	return f.metrics.ForwardOTLP(ctx, body)
}

func (f forwarder) Logs(ctx context.Context, body []byte) error {
	return f.logs.ForwardOTLP(ctx, body, StreamFields)
}
