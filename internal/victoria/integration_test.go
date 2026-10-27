//go:build integration

package victoria

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// These tests start the three stores in Docker on loopback ports and round-trip
// a write and a query through each client. They need a Docker daemon and the
// images, and run with: go test -tags integration ./internal/victoria/

const (
	imageMetrics = "victoriametrics/victoria-metrics:latest"
	imageLogs    = "victoriametrics/victoria-logs:latest"
	imageTraces  = "victoriametrics/victoria-traces:latest"
)

// startStore runs image on a free loopback port with the extra flags and
// returns its base URL once /health answers.
func startStore(t *testing.T, image string, flags ...string) string {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()

	args := append([]string{"run", "-d", "--rm", "--network", "host", image, "-httpListenAddr=" + addr}, flags...)
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Skipf("docker run %s: %v: %s", image, err, out)
	}
	id := strings.TrimSpace(string(out))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", id).Run() })

	base := "http://" + addr
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := NewMetrics(base, nil).Health(context.Background())
		if err == nil {
			return base
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s not healthy: %v", image, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// eventually retries f until it returns true: the stores buffer writes for up
// to a few seconds before they are searchable.
func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func TestIntegrationMetrics(t *testing.T) {
	ctx := context.Background()
	m := NewMetrics(startStore(t, imageMetrics, "-opentelemetry.usePrometheusNaming"), nil)

	// Samples sit a few minutes back: the store hides the newest 30 s of
	// data from queries (-search.latencyOffset).
	now := time.Now().Truncate(time.Second).Add(-time.Minute)
	lines := []string{
		fmt.Sprintf(`shed_test_gauge{shed_service_id="s1"} 12.5 %d`, now.Add(-time.Minute).UnixMilli()),
		fmt.Sprintf(`shed_test_gauge{shed_service_id="s1"} 13.5 %d`, now.UnixMilli()),
	}
	if err := m.ImportPrometheus(ctx, lines); err != nil {
		t.Fatal(err)
	}
	eventually(t, "imported series", func() bool {
		res, err := m.Query(ctx, `shed_test_gauge`, now)
		return err == nil && len(res.Series) == 1
	})

	res, err := m.Query(ctx, `shed_test_gauge`, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Type != "vector" || res.Series[0].Labels["shed_service_id"] != "s1" || res.Series[0].Values[0].Value != 13.5 {
		t.Fatalf("instant result = %+v", res)
	}

	rng, err := m.QueryRange(ctx, `shed_test_gauge`, now.Add(-2*time.Minute), now, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if rng.Type != "matrix" || len(rng.Series) != 1 || len(rng.Series[0].Values) < 3 {
		t.Fatalf("range result = %+v", rng)
	}

	two, err := m.Query(ctx, `1+1`, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(two.Series) != 1 || two.Series[0].Values[0].Value != 2 {
		t.Fatalf("1+1 = %+v", two)
	}

	if _, err := m.Query(ctx, `sum(`, now); err == nil {
		t.Fatal("bad query succeeded")
	}

	if err := m.ForwardOTLP(ctx, otlpMetrics("shed_test_otlp", 7, now)); err != nil {
		t.Fatal(err)
	}
	eventually(t, "otlp metric", func() bool {
		res, err := m.Query(ctx, `shed_test_otlp`, now)
		return err == nil && len(res.Series) == 1 && res.Series[0].Values[0].Value == 7
	})

	eventually(t, "data size", func() bool {
		n, err := m.DataSize(ctx)
		return err == nil && n > 0
	})
}

func TestIntegrationLogs(t *testing.T) {
	ctx := context.Background()
	l := NewLogs(startStore(t, imageLogs), nil)

	now := time.Now().UTC()
	err := l.InsertJSONLines(ctx, []Record{
		{"ts": now.Format(time.RFC3339Nano), "message": "hello world", "shed.service.id": "s1", "level": "info"},
		{"ts": now.Add(time.Second).Format(time.RFC3339Nano), "message": "boom", "shed.service.id": "s1", "level": "error"},
	}, InsertOptions{StreamFields: []string{"shed.service.id"}, MsgField: "message", TimeField: "ts"})
	if err != nil {
		t.Fatal(err)
	}
	var got []Record
	eventually(t, "inserted records", func() bool {
		got, err = l.Query(ctx, `"shed.service.id":s1`, now.Add(-time.Hour), now.Add(time.Hour), 10)
		return err == nil && len(got) == 2
	})
	if got[0]["_msg"] != "boom" || got[0]["level"] != "error" || got[1]["_msg"] != "hello world" {
		t.Fatalf("records = %+v", got)
	}
	if got[0]["_stream"] != `{shed.service.id="s1"}` || got[0]["_time"] == "" {
		t.Fatalf("stream/time = %+v", got[0])
	}

	if err := l.ForwardOTLP(ctx, otlpLogs("otlp line", "s2"), []string{"shed.service.id"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "otlp log", func() bool {
		got, err = l.Query(ctx, `"otlp line"`, time.Time{}, time.Time{}, 10)
		return err == nil && len(got) == 1
	})
	if got[0]["_stream"] != `{shed.service.id="s2"}` {
		t.Fatalf("otlp stream = %+v", got[0])
	}

	eventually(t, "data size", func() bool {
		n, err := l.DataSize(ctx)
		return err == nil && n > 0
	})
}

func TestIntegrationTraces(t *testing.T) {
	ctx := context.Background()
	tr := NewTraces(startStore(t, imageTraces), nil)

	traceID := "0af7651916cd43dd8448eb211c80319c"
	start := time.Now().Add(-time.Minute).Truncate(time.Millisecond)
	if err := tr.ForwardOTLP(ctx, otlpTrace(traceID, "checkout", start)); err != nil {
		t.Fatal(err)
	}

	var got *Trace
	eventually(t, "trace", func() bool {
		var err error
		got, err = tr.Trace(ctx, traceID)
		return err == nil
	})
	if len(got.Spans) != 2 {
		t.Fatalf("spans = %+v", got.Spans)
	}
	root, child := got.Spans[0], got.Spans[1]
	if root.Name != "GET /" || root.Service != "checkout" || root.Kind != "server" || root.ParentID != "" ||
		root.Duration != 50*time.Millisecond || root.Status != "unset" || root.Attributes["http.method"] != "GET" {
		t.Fatalf("root = %+v", root)
	}
	if child.Name != "db query" || child.ParentID != root.SpanID || child.Status != "error" || child.Kind != "client" {
		t.Fatalf("child = %+v", child)
	}

	var hits []TraceSummary
	eventually(t, "search", func() bool {
		var err error
		hits, err = tr.SearchTraces(ctx, SearchParams{
			Service:     "checkout",
			MinDuration: time.Millisecond,
			Start:       start.Add(-time.Hour),
			End:         start.Add(time.Hour),
			Limit:       5,
		})
		return err == nil && len(hits) == 1
	})
	if h := hits[0]; h.TraceID != traceID || h.Name != "GET /" || h.Spans != 2 || h.Errors != 1 || h.Duration != 50*time.Millisecond {
		t.Fatalf("hit = %+v", h)
	}

	if _, err := tr.Trace(ctx, "ffffffffffffffffffffffffffffffff"); err != ErrTraceNotFound {
		t.Fatalf("missing trace err = %v", err)
	}

	eventually(t, "data size", func() bool {
		n, err := tr.DataSize(ctx)
		return err == nil && n > 0
	})
}

// A minimal protobuf writer, enough to build OTLP requests without the
// OpenTelemetry proto packages.

func pbVarint(v uint64) []byte { return binary.AppendUvarint(nil, v) }

func pbBytes(field int, b []byte) []byte {
	out := pbVarint(uint64(field<<3 | 2))
	out = append(out, pbVarint(uint64(len(b)))...)
	return append(out, b...)
}

func pbString(field int, s string) []byte { return pbBytes(field, []byte(s)) }

func pbUint(field int, v uint64) []byte {
	return append(pbVarint(uint64(field<<3)), pbVarint(v)...)
}

func pbFixed64(field int, v uint64) []byte {
	return binary.LittleEndian.AppendUint64(pbVarint(uint64(field<<3|1)), v)
}

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// pbAttr encodes a KeyValue with a string value.
func pbAttr(field int, key, val string) []byte {
	return pbBytes(field, cat(pbString(1, key), pbBytes(2, pbString(1, val))))
}

func pbResource(attrs ...[]byte) []byte { return pbBytes(1, pbBytes(1, cat(attrs...))) }

func otlpMetrics(name string, v float64, at time.Time) []byte {
	point := cat(pbFixed64(3, uint64(at.UnixNano())), pbFixed64(4, math.Float64bits(v)))
	metric := cat(pbString(1, name), pbBytes(5, pbBytes(1, point)))
	scope := pbBytes(2, pbBytes(2, metric))
	return pbBytes(1, scope)
}

func otlpLogs(body, serviceID string) []byte {
	rec := cat(pbFixed64(1, uint64(time.Now().UnixNano())), pbBytes(5, pbString(1, body)))
	res := pbBytes(1, pbAttr(1, "shed.service.id", serviceID))
	return pbBytes(1, cat(res, pbBytes(2, pbBytes(2, rec))))
}

func otlpTrace(traceID, service string, start time.Time) []byte {
	tid := make([]byte, 16)
	for i := range tid {
		fmt.Sscanf(traceID[i*2:i*2+2], "%02x", &tid[i])
	}
	rootID := []byte{1, 1, 1, 1, 1, 1, 1, 1}
	childID := []byte{2, 2, 2, 2, 2, 2, 2, 2}
	s := uint64(start.UnixNano())
	root := cat(pbBytes(1, tid), pbBytes(2, rootID), pbString(5, "GET /"), pbUint(6, 2),
		pbFixed64(7, s), pbFixed64(8, s+uint64(50*time.Millisecond)), pbAttr(9, "http.method", "GET"))
	child := cat(pbBytes(1, tid), pbBytes(2, childID), pbBytes(4, rootID), pbString(5, "db query"), pbUint(6, 3),
		pbFixed64(7, s+uint64(5*time.Millisecond)), pbFixed64(8, s+uint64(20*time.Millisecond)),
		pbBytes(15, pbUint(3, 2)))
	scope := pbBytes(2, cat(pbBytes(2, root), pbBytes(2, child)))
	res := pbBytes(1, pbAttr(1, "service.name", service))
	return pbBytes(1, cat(res, scope))
}
