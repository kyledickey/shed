package proxy

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"math"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/caddyserver/caddy/v2"
)

// AccessEntry is one request the proxy served. It never holds the client's
// address, a request header, the query string, or a body.
type AccessEntry struct {
	// Time is when the response finished.
	Time   time.Time
	Method string
	// Host is the requested host name, lowercase and without a port.
	Host string
	// Path is the request path as sent, still percent-encoded.
	Path     string
	Status   int
	Duration time.Duration
	// BytesIn is the size of the request body read.
	BytesIn int64
	// BytesOut is the size of the response body written.
	BytesOut int64
	// TLSVersion is the TLS version, such as "TLS 1.3", or empty for plain
	// HTTP.
	TLSVersion string
	// TraceID is the request's trace ID while tracing is on, else empty.
	TraceID string
}

// accessLogger is the name of Caddy's access logger for the shed server,
// "http.log.access.<default_logger_name>".
const (
	accessLoggerName = "edge"
	accessLogger     = "http.log.access." + accessLoggerName
)

// accessSink receives access entries. Caddy opens log writers by module ID,
// not by Proxy, so the callback is process-global like the rest of Caddy.
var accessSink atomic.Pointer[func(AccessEntry)]

func init() {
	caddy.RegisterModule(accessWriter{})
}

// accessWriter is a Caddy log writer module that parses each JSON access log
// entry and hands it to accessSink.
type accessWriter struct{}

// CaddyModule implements caddy.Module.
func (accessWriter) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "caddy.logging.writers.shed_access",
		New: func() caddy.Module { return new(accessWriter) },
	}
}

func (accessWriter) String() string    { return "shed access entries" }
func (accessWriter) WriterKey() string { return "shed_access" }

func (accessWriter) OpenWriter() (io.WriteCloser, error) { return accessWriter{}, nil }

// Write receives exactly one encoded entry per call from zap.
func (accessWriter) Write(p []byte) (int, error) {
	if fn := accessSink.Load(); fn != nil {
		if e, ok := parseAccess(p); ok {
			(*fn)(e)
		}
	}
	return len(p), nil
}

func (accessWriter) Close() error { return nil }

// accessJSON is the part of Caddy's JSON access log entry shed keeps.
type accessJSON struct {
	TS      float64 `json:"ts"`
	Request struct {
		Method string `json:"method"`
		Host   string `json:"host"`
		URI    string `json:"uri"`
		TLS    *struct {
			Version uint16 `json:"version"`
		} `json:"tls"`
	} `json:"request"`
	BytesRead int64   `json:"bytes_read"`
	Duration  float64 `json:"duration"`
	Size      int64   `json:"size"`
	Status    int     `json:"status"`
	TraceID   string  `json:"traceID"`
}

// parseAccess decodes one access log entry.
func parseAccess(b []byte) (AccessEntry, bool) {
	var j accessJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return AccessEntry{}, false
	}
	r := j.Request
	sec, frac := math.Modf(j.TS)
	e := AccessEntry{
		Time:     time.Unix(int64(sec), int64(frac*1e9)),
		Method:   r.Method,
		Host:     hostOnly(r.Host),
		Status:   j.Status,
		Duration: time.Duration(j.Duration * float64(time.Second)),
		BytesIn:  j.BytesRead,
		BytesOut: j.Size,
		TraceID:  j.TraceID,
	}
	e.Path, _, _ = strings.Cut(r.URI, "?")
	if r.TLS != nil {
		e.TLSVersion = tls.VersionName(r.TLS.Version)
	}
	// Caddy logs 0 when a handler wrote no header; Go then sends 200.
	if e.Status == 0 {
		e.Status = 200
	}
	return e, true
}

// hostOnly returns host lowercase, without a port or trailing dot.
func hostOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.TrimSuffix(strings.ToLower(host), ".")
}

var _ caddy.WriterOpener = accessWriter{}
