package victoria

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Logs is a VictoriaLogs client.
type Logs struct {
	b base
}

// NewLogs returns a client for the VictoriaLogs at baseURL, such as
// "http://127.0.0.1:9428". A nil hc gets a client with a 30 s timeout.
func NewLogs(baseURL string, hc *http.Client, opts ...Option) *Logs {
	return &Logs{b: newBase(baseURL, hc, "vl_data_size_bytes", opts)}
}

// Record is one log entry: field names to values. A query result carries
// _time, _msg, and _stream along with the entry's other fields.
type Record map[string]string

// InsertOptions tell the store how to read the records of a JSON lines write.
type InsertOptions struct {
	// StreamFields are the fields that identify a log stream.
	StreamFields []string
	// MsgField names the field holding the message. Empty means _msg.
	MsgField string
	// TimeField names the field holding the timestamp. Empty means _time. A
	// record without it is stamped on arrival.
	TimeField string
}

// InsertJSONLines writes records as newline-delimited JSON.
func (l *Logs) InsertJSONLines(ctx context.Context, records []Record, opts InsertOptions) error {
	if len(records) == 0 {
		return nil
	}
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false)
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			return fmt.Errorf("victoria: logs insert: encode record: %w", err)
		}
	}
	q := url.Values{}
	if len(opts.StreamFields) > 0 {
		q.Set("_stream_fields", strings.Join(opts.StreamFields, ","))
	}
	if opts.MsgField != "" {
		q.Set("_msg_field", opts.MsgField)
	}
	if opts.TimeField != "" {
		q.Set("_time_field", opts.TimeField)
	}
	return l.b.send(ctx, request{
		op:          "logs insert",
		method:      http.MethodPost,
		path:        "/insert/jsonline",
		query:       q,
		body:        body.Bytes(),
		contentType: "application/stream+json",
	})
}

// ForwardOTLP sends an OTLP/HTTP logs request, protobuf-encoded and
// uncompressed, to the store. streamFields become the VL-Stream-Fields header;
// without them the store puts every record in one default stream.
func (l *Logs) ForwardOTLP(ctx context.Context, body []byte, streamFields []string) error {
	var h map[string]string
	if len(streamFields) > 0 {
		h = map[string]string{"VL-Stream-Fields": strings.Join(streamFields, ",")}
	}
	return l.b.send(ctx, request{
		op:          "logs otlp",
		method:      http.MethodPost,
		path:        "/insert/opentelemetry/v1/logs",
		body:        body,
		contentType: "application/x-protobuf",
		header:      h,
	})
}

// Query runs a LogsQL query over [start, end). A zero start or end leaves that
// side open, and a limit of zero or less returns every match. The store sorts
// by time, newest first, when a limit applies.
func (l *Logs) Query(ctx context.Context, logsql string, start, end time.Time, limit int) ([]Record, error) {
	form := url.Values{"query": {logsql}}
	if !start.IsZero() {
		form.Set("start", start.UTC().Format(time.RFC3339Nano))
	}
	if !end.IsZero() {
		form.Set("end", end.UTC().Format(time.RFC3339Nano))
	}
	if limit > 0 {
		form.Set("limit", strconv.Itoa(limit))
	}
	data, err := l.b.get(ctx, request{
		op:     "logs query",
		method: http.MethodPost,
		path:   "/select/logsql/query",
		form:   form,
	})
	if err != nil {
		return nil, err
	}
	records := []Record{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64<<10), len(data)+1)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("victoria: logs query: decode record: %w", err)
		}
		records = append(records, r)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("victoria: logs query: %w", err)
	}
	return records, nil
}

// Health reports whether the store answers /health.
func (l *Logs) Health(ctx context.Context) error {
	return l.b.health(ctx, "logs health")
}

// DataSize returns the bytes of data the store holds on disk, from its
// /metrics page.
func (l *Logs) DataSize(ctx context.Context) (int64, error) {
	return l.b.dataSize(ctx, "logs stats")
}
