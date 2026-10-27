package build

import (
	"bytes"
	"context"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const scope = "github.com/kyledickey/shed/internal/build"

// vertexLine matches a line of BuildKit's plain progress output: the vertex
// number, then a name, status, or log line of the vertex.
var vertexLine = regexp.MustCompile(`^#(\d+) (.+)$`)

// steps is an io.Writer that reads BuildKit's plain progress output and
// records each build step (vertex) as a span with whether it was cached, and
// in shed_build_steps_total. Call close when the build ends.
type steps struct {
	ctx    context.Context // parent of the step spans
	count  metric.Int64Counter
	labels []attribute.KeyValue

	buf  []byte
	open map[string]trace.Span // by vertex number
	done map[string]bool
}

func newSteps(ctx context.Context, labels []attribute.KeyValue) *steps {
	// Creating an instrument fails only for an invalid name, and the
	// instrument returned works regardless.
	count, _ := otel.Meter(scope).Int64Counter("shed_build_steps_total",
		metric.WithDescription("BuildKit build steps that ended, by whether they were cached."))
	return &steps{ctx: ctx, count: count, labels: slices.Clip(labels), open: map[string]trace.Span{}, done: map[string]bool{}}
}

func (s *steps) Write(p []byte) (int, error) {
	s.buf = append(s.buf, p...)
	for {
		i := bytes.IndexByte(s.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		s.line(strings.TrimRight(string(s.buf[:i]), "\r"))
		s.buf = s.buf[i+1:]
	}
}

// line handles one line of output. The first line of a vertex names it, and
// DONE, CACHED, ERROR, or CANCELED ends it; BuildKit repeats the name when it
// switches back to a vertex.
func (s *steps) line(l string) {
	m := vertexLine.FindStringSubmatch(l)
	if m == nil || s.done[m[1]] {
		return
	}
	id, text := m[1], m[2]
	span, ok := s.open[id]
	if !ok {
		_, s.open[id] = otel.Tracer(scope).Start(s.ctx, text)
		return
	}
	switch {
	case strings.HasPrefix(text, "DONE"):
		s.end(id, span, false, "")
	case text == "CACHED":
		s.end(id, span, true, "")
	case strings.HasPrefix(text, "ERROR"):
		s.end(id, span, false, text)
	case text == "CANCELED":
		s.end(id, span, false, text)
	}
}

func (s *steps) end(id string, span trace.Span, cached bool, failure string) {
	span.SetAttributes(attribute.Bool("cached", cached))
	if failure != "" {
		span.SetStatus(codes.Error, failure)
	}
	span.End()
	s.count.Add(s.ctx, 1, metric.WithAttributes(append(s.labels, attribute.Bool("cached", cached))...))
	delete(s.open, id)
	s.done[id] = true
}

// close ends the spans of steps that never finished.
func (s *steps) close() {
	for id, span := range s.open {
		s.end(id, span, false, "unfinished")
	}
}

// publicURL returns u without user info, query, or fragment, so that it can
// be recorded. A clone URL carries the installation token as user info.
func publicURL(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	p.User, p.RawQuery, p.ForceQuery, p.Fragment = nil, "", false, ""
	return p.String()
}

// span runs fn in a span named name.
func span(ctx context.Context, name string, fn func(context.Context) error, attrs ...attribute.KeyValue) error {
	ctx, sp := otel.Tracer(scope).Start(ctx, name, trace.WithAttributes(attrs...))
	defer sp.End()
	err := fn(ctx)
	if err != nil {
		sp.SetStatus(codes.Error, err.Error())
	}
	return err
}
