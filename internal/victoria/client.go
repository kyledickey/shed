// Package victoria holds small HTTP clients for the three stores shed runs
// when telemetry is on: VictoriaMetrics, VictoriaLogs, and VictoriaTraces.
//
// Each client wraps one base URL and an [http.Client]. It writes (Prometheus
// text, JSON lines, forwarded OTLP), runs queries (MetricsQL, LogsQL, the
// Jaeger JSON API), checks /health, and reads the size of the store's data
// from its /metrics page. The package imports nothing from internal/.
package victoria

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultMaxBytes is the default cap on a query response body.
	DefaultMaxBytes int64 = 32 << 20

	// maxErrorBody is how much of an error response is kept in an error.
	maxErrorBody = 512

	// maxDrain is how much of a successful write response is read so the
	// connection can be reused.
	maxDrain = 4 << 10

	// maxStatsBytes caps a /metrics page.
	maxStatsBytes = 64 << 20

	defaultTimeout = 30 * time.Second
)

// ErrTooLarge is returned when a response body exceeds the client's cap.
var ErrTooLarge = errors.New("victoria: response too large")

// StatusError is returned when a store answers with a non-2xx status. Callers
// branch on Code, for example to report a store that is down or full.
type StatusError struct {
	// Op names the operation, such as "metrics query".
	Op string
	// Code is the HTTP status code.
	Code int
	// Body is the start of the response body, at most 512 bytes.
	Body string
}

// Error implements error.
func (e *StatusError) Error() string {
	return fmt.Sprintf("victoria: %s: status %d: %s", e.Op, e.Code, e.Body)
}

// Option configures a client.
type Option func(*base)

// WithMaxBytes caps the response body a query may read. Larger responses fail
// with [ErrTooLarge]. The default is [DefaultMaxBytes].
func WithMaxBytes(n int64) Option {
	return func(b *base) { b.maxBytes = n }
}

// base is the state and plumbing shared by the three clients.
type base struct {
	url      string
	hc       *http.Client
	maxBytes int64
	// sizeMetric is the data-size gauge the store exposes on /metrics.
	sizeMetric string
}

func newBase(baseURL string, hc *http.Client, sizeMetric string, opts []Option) base {
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	b := base{
		url:        strings.TrimRight(baseURL, "/"),
		hc:         hc,
		maxBytes:   DefaultMaxBytes,
		sizeMetric: sizeMetric,
	}
	for _, o := range opts {
		o(&b)
	}
	return b
}

// request describes one HTTP call.
type request struct {
	op          string
	method      string
	path        string
	query       url.Values
	form        url.Values
	body        []byte
	contentType string
	header      map[string]string
}

// do sends r and returns the response for a 2xx status. The caller closes the
// body. Any other status becomes a *StatusError.
func (b *base) do(ctx context.Context, r request) (*http.Response, error) {
	u := b.url + r.path
	if len(r.query) > 0 {
		u += "?" + r.query.Encode()
	}
	body := r.body
	contentType := r.contentType
	if r.form != nil {
		body = []byte(r.form.Encode())
		contentType = "application/x-www-form-urlencoded"
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, u, rd)
	if err != nil {
		return nil, fmt.Errorf("victoria: %s: %w", r.op, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range r.header {
		req.Header.Set(k, v)
	}
	resp, err := b.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("victoria: %s: %w", r.op, err)
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody+1))
		return nil, &StatusError{Op: r.op, Code: resp.StatusCode, Body: truncate(snippet)}
	}
	return resp, nil
}

// send performs a write and discards the response.
func (b *base) send(ctx context.Context, r request) error {
	resp, err := b.do(ctx, r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrain))
	return nil
}

// get performs a query and returns its body, read up to the client's cap.
func (b *base) get(ctx context.Context, r request) ([]byte, error) {
	resp, err := b.do(ctx, r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, b.maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("victoria: %s: read body: %w", r.op, err)
	}
	if int64(len(data)) > b.maxBytes {
		return nil, fmt.Errorf("%s: more than %d bytes: %w", r.op, b.maxBytes, ErrTooLarge)
	}
	return data, nil
}

// health checks the store's /health endpoint.
func (b *base) health(ctx context.Context, op string) error {
	return b.send(ctx, request{op: op, method: http.MethodGet, path: "/health"})
}

// dataSize sums every series of the store's data-size gauge on /metrics.
func (b *base) dataSize(ctx context.Context, op string) (int64, error) {
	resp, err := b.do(ctx, request{op: op, method: http.MethodGet, path: "/metrics"})
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	n, err := sumMetric(io.LimitReader(resp.Body, maxStatsBytes), b.sizeMetric)
	if err != nil {
		return 0, fmt.Errorf("victoria: %s: %w", op, err)
	}
	return n, nil
}

// sumMetric adds up the values of every sample named name in a Prometheus
// text exposition. It fails if no sample has that name.
func sumMetric(r io.Reader, name string) (int64, error) {
	var sum float64
	var found bool
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		rest, ok := strings.CutPrefix(line, name)
		if !ok || rest == "" {
			continue
		}
		switch rest[0] {
		case ' ', '\t':
		case '{':
			end := strings.LastIndexByte(rest, '}')
			if end < 0 {
				continue
			}
			rest = rest[end+1:]
		default:
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			return 0, fmt.Errorf("metric %s: bad value %q", name, fields[0])
		}
		sum += v
		found = true
	}
	if err := sc.Err(); err != nil {
		return 0, fmt.Errorf("read metrics: %w", err)
	}
	if !found {
		return 0, fmt.Errorf("metric %s not found", name)
	}
	return int64(math.Round(sum)), nil
}

// truncate shortens an error body to at most maxErrorBody bytes.
func truncate(b []byte) string {
	cut := len(b) > maxErrorBody
	if cut {
		b = b[:maxErrorBody-3]
	}
	s := strings.TrimSpace(strings.ToValidUTF8(string(b), "?"))
	if cut {
		s += "..."
	}
	return s
}
