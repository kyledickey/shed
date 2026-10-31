package otlp

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// signal describes one OTLP endpoint: its request type and how the handler
// processes, forwards, and answers it.
type signal[R proto.Message] struct {
	name     string
	new      func() R
	count    func(R) int64
	process  func(h *Handler, ctx context.Context, req R, a Attribution) outcome
	forward  func(f Forwarder, ctx context.Context, body []byte) error
	response func(o outcome) proto.Message
}

// outcome is what processing a request decided.
type outcome struct {
	accepted int64
	rejected int64
	// spans are the accepted server and consumer spans, recorded as span
	// metrics once the request is forwarded.
	spans []spanSample
}

func serve[R proto.Message](h *Handler, w http.ResponseWriter, r *http.Request, s signal[R]) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	c, ok := codecFor(r.Header.Get("Content-Type"))
	if !ok {
		http.Error(w, "content type must be application/x-protobuf or application/json", http.StatusUnsupportedMediaType)
		return
	}
	body, status, err := readBody(w, r)
	if err != nil {
		c.writeStatus(w, status, err.Error())
		return
	}
	req := s.new()
	if err := c.unmarshal(body, req); err != nil {
		c.writeStatus(w, http.StatusBadRequest, fmt.Sprintf("decode %s: %v", s.name, err))
		return
	}

	ctx := r.Context()
	a, err := h.cfg.Attribute(r)
	if err != nil {
		h.log.Warn("otlp: attribute request", "remote", r.RemoteAddr, "err", err)
		w.Header().Set("Retry-After", "5")
		c.writeStatus(w, http.StatusServiceUnavailable, "attribution unavailable")
		return
	}

	o := s.process(h, ctx, req, a)
	if o.accepted > 0 {
		out, err := proto.Marshal(req)
		if err == nil {
			err = s.forward(h.cfg.Forwarder, ctx, out)
		}
		if err != nil {
			h.log.Warn("otlp: forward", "signal", s.name, "err", err)
			w.Header().Set("Retry-After", "5")
			c.writeStatus(w, http.StatusServiceUnavailable, "store unavailable")
			return
		}
	}
	h.ingested.Add(ctx, o.accepted, metric.WithAttributes(attribute.String("signal", s.name)))
	h.recordSpans(ctx, o.spans)
	c.write(w, http.StatusOK, s.response(o))
}

// readBody reads the request body, decompressing it if needed, and returns
// the HTTP status to answer with when it fails.
func readBody(w http.ResponseWriter, r *http.Request) ([]byte, int, error) {
	var rd io.Reader = http.MaxBytesReader(w, r.Body, MaxBodySize)
	switch enc := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))); enc {
	case "", "identity":
	case "gzip":
		zr, err := gzip.NewReader(rd)
		if err != nil {
			return nil, statusForRead(err), fmt.Errorf("gzip: %w", err)
		}
		defer zr.Close()
		rd = zr
	default:
		return nil, http.StatusUnsupportedMediaType, fmt.Errorf("unsupported content encoding %q", enc)
	}
	b, err := io.ReadAll(io.LimitReader(rd, MaxBodySize+1))
	if err != nil {
		return nil, statusForRead(err), fmt.Errorf("read body: %w", err)
	}
	if len(b) > MaxBodySize {
		return nil, http.StatusRequestEntityTooLarge, errors.New("decompressed body over 8 MiB")
	}
	return b, 0, nil
}

func statusForRead(err error) int {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

// codec is a request's encoding; responses use the same one.
type codec bool

const (
	protobufCodec codec = false
	jsonCodec     codec = true
)

func codecFor(contentType string) (codec, bool) {
	mt, _, err := mime.ParseMediaType(contentType)
	switch {
	case err != nil:
		return protobufCodec, false
	case mt == "application/x-protobuf":
		return protobufCodec, true
	case mt == "application/json":
		return jsonCodec, true
	}
	return protobufCodec, false
}

func (c codec) unmarshal(b []byte, m proto.Message) error {
	if c == protobufCodec {
		return proto.Unmarshal(b, m)
	}
	b, err := hexIDsToBase64(b)
	if err != nil {
		return err
	}
	return protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(b, m)
}

func (c codec) write(w http.ResponseWriter, status int, m proto.Message) {
	var b []byte
	var err error
	if c == jsonCodec {
		w.Header().Set("Content-Type", "application/json")
		b, err = protojson.Marshal(m)
	} else {
		w.Header().Set("Content-Type", "application/x-protobuf")
		b, err = proto.Marshal(m)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	w.Write(b)
}

// writeStatus answers with a google.rpc.Status, as OTLP/HTTP prescribes for
// errors.
func (c codec) writeStatus(w http.ResponseWriter, status int, msg string) {
	c.write(w, status, &statuspb.Status{Message: msg})
}

// idKeys are the JSON fields that OTLP/JSON encodes as hex strings, where
// protojson expects base64 for bytes.
var idKeys = map[string]bool{
	"traceId": true, "spanId": true, "parentSpanId": true,
	"trace_id": true, "span_id": true, "parent_span_id": true,
}

// hexIDsToBase64 rewrites the trace and span IDs of an OTLP/JSON body from
// hex to base64 so protojson can decode it. Numbers are kept verbatim, since
// 64-bit timestamps do not survive a float64.
func hexIDsToBase64(b []byte) ([]byte, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	if err := rewriteIDs(v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func rewriteIDs(v any) error {
	switch v := v.(type) {
	case map[string]any:
		for k, x := range v {
			if s, ok := x.(string); ok && idKeys[k] {
				raw, err := hex.DecodeString(s)
				if err != nil {
					return fmt.Errorf("%s: %w", k, err)
				}
				v[k] = base64.StdEncoding.EncodeToString(raw)
				continue
			}
			if err := rewriteIDs(x); err != nil {
				return err
			}
		}
	case []any:
		for _, x := range v {
			if err := rewriteIDs(x); err != nil {
				return err
			}
		}
	}
	return nil
}
