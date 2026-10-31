package build

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const progress = `#1 [internal] load build definition from Dockerfile
#1 transferring dockerfile: 52B done
#1 DONE 0.0s

#2 [1/2] FROM docker.io/library/alpine
#2 CACHED

#3 [2/2] RUN echo supersecretvalue
#3 0.123 supersecretvalue
#1 [internal] load build definition from Dockerfile
#3 [2/2] RUN echo supersecretvalue
#3 DONE 0.2s
`

func TestBuildRecordsStepsWithoutSecrets(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	prevTP, prevMP := otel.GetTracerProvider(), otel.GetMeterProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetMeterProvider(prevMP)
	})

	bin := t.TempDir()
	scripts := map[string]string{
		"git":    "#!/bin/sh\necho \"git $*\"\ntouch Dockerfile\n",
		"docker": "#!/bin/sh\ncat \"$PROGRESS\"\n",
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	progressFile := filepath.Join(t.TempDir(), "progress")
	if err := os.WriteFile(progressFile, []byte(progress), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROGRESS", progressFile)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	const token = "ghs_installationtoken123"
	req := Request{
		RepoURL: "https://x-access-token:" + token + "@github.com/o/r.git?ref=" + token,
		Commit:  "abc",
		Image:   "x",
		Env:     map[string]string{"API_KEY": "supersecretvalue"},
		Labels:  []attribute.KeyValue{attribute.String("shed_service_id", "svc1")},
	}
	b := &Builder{WorkDir: t.TempDir()}
	if err := b.Build(context.Background(), "a", req, io.Discard); err != nil {
		t.Fatal(err)
	}

	cached := map[string]string{}
	for _, sp := range spans.Ended() {
		texts := []string{sp.Name(), sp.Status().Description}
		for _, kv := range sp.Attributes() {
			texts = append(texts, kv.Value.String())
			if kv.Key == "cached" {
				cached[sp.Name()] = kv.Value.String()
			}
		}
		for _, s := range texts {
			if strings.Contains(s, token) || strings.Contains(s, "supersecretvalue") {
				t.Errorf("span %q carries a secret: %q", sp.Name(), s)
			}
		}
		if sp.Name() == "git clone" {
			for _, kv := range sp.Attributes() {
				if kv.Key == "vcs.repository.url.full" && kv.Value.String() != "https://github.com/o/r.git" {
					t.Errorf("clone URL attribute = %q, want https://github.com/o/r.git", kv.Value.String())
				}
			}
		}
	}
	want := map[string]string{
		"[internal] load build definition from Dockerfile": "false",
		"[1/2] FROM docker.io/library/alpine":              "true",
		"[2/2] RUN echo ***":                               "false",
	}
	for name, c := range want {
		if cached[name] != c {
			t.Errorf("step %q cached = %q, want %q (steps: %v)", name, cached[name], c, cached)
		}
	}
	if len(cached) != len(want) {
		t.Errorf("got steps %v, want %v", cached, want)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	counts := map[bool]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok || m.Name != "shed_build_steps_total" {
				continue
			}
			for _, dp := range sum.DataPoints {
				if v, _ := dp.Attributes.Value("shed_service_id"); v.AsString() != "svc1" {
					t.Errorf("data point lacks the service label: %v", dp.Attributes)
				}
				c, _ := dp.Attributes.Value("cached")
				counts[c.AsBool()] += dp.Value
			}
		}
	}
	if counts[true] != 1 || counts[false] != 2 {
		t.Errorf("shed_build_steps_total = %v, want 1 cached and 2 not", counts)
	}
}
