package docker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moby/moby/client"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestOperation(t *testing.T) {
	tests := []struct{ method, path, want string }{
		{"GET", "/v1.47/containers/0123456789ab/json", "container_inspect"},
		{"POST", "/v1.47/containers/create", "container_create"},
		{"GET", "/containers/json", "container_list"},
		{"DELETE", "/v1.47/containers/0123456789ab", "container_remove"},
		{"POST", "/v1.47/images/create", "image_pull"},
		{"GET", "/v1.47/images/ghcr.io/owner/app:1.2/json", "image_inspect"},
		{"DELETE", "/v1.47/images/ghcr.io/owner/app:1.2", "image_remove"},
		{"POST", "/v1.47/exec/abc/start", "exec_start"},
		{"GET", "/_ping", "ping"},
		{"POST", "/v1.47/plugins/some-plugin/enable", "POST /plugins/{id}"},
		{"GET", "/v1.47/swarm", "GET /swarm"},
	}
	for _, tt := range tests {
		if got := operation(tt.method, tt.path); got != tt.want {
			t.Errorf("operation(%s %s) = %q, want %q", tt.method, tt.path, got, tt.want)
		}
	}
}

func TestRequestsAreTracedAndTimedByOperation(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	prevTP, prevMP := otel.GetTracerProvider(), otel.GetMeterProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetMeterProvider(prevMP)
	})

	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/json") {
			w.Write([]byte(`{"Id":"0123456789ab","State":{"Status":"running"}}`))
		}
	}))
	defer engine.Close()
	opts, err := instrument()
	if err != nil {
		t.Fatal(err)
	}
	api, err := client.New(append([]client.Opt{client.WithHost("tcp://" + engine.Listener.Addr().String())}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	c := &Client{api: api}
	if _, err := c.Inspect(context.Background(), "0123456789ab"); err != nil {
		t.Fatal(err)
	}

	var named bool
	for _, s := range spans.Ended() {
		if s.Name() == "container_inspect" {
			named = true
		}
		if strings.Contains(s.Name(), "0123456789ab") {
			t.Errorf("span name %q carries the container ID", s.Name())
		}
	}
	if !named {
		t.Errorf("no container_inspect span among %d spans", len(spans.Ended()))
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	var count uint64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if h, ok := m.Data.(metricdata.Histogram[float64]); ok && m.Name == "shed_docker_request_duration_seconds" {
				for _, dp := range h.DataPoints {
					if op, _ := dp.Attributes.Value("op"); op.AsString() == "container_inspect" {
						count += dp.Count
					}
				}
			}
		}
	}
	if count != 1 {
		t.Errorf("shed_docker_request_duration_seconds{op=container_inspect} count = %d, want 1", count)
	}
}
