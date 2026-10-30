package store

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"modernc.org/sqlite"
)

const scope = "github.com/kyledickey/shed/internal/store"

// sqliteConn is the part of a modernc.org/sqlite connection that
// database/sql uses.
type sqliteConn interface {
	driver.Conn
	driver.ConnBeginTx
	driver.ConnPrepareContext
	driver.ExecerContext
	driver.QueryerContext
	driver.Pinger
	driver.SessionResetter
	driver.Validator
}

// connector opens SQLite connections whose queries are timed, and traced
// under a recording span. Spans carry the statement text, never its
// arguments.
type connector struct {
	dsn      string
	duration metric.Float64Histogram
}

func newConnector(dsn string) (connector, error) {
	duration, err := otel.Meter(scope).Float64Histogram("shed_store_query_duration_seconds",
		metric.WithDescription("Duration of store queries, including reading their rows."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 1))
	if err != nil {
		return connector{}, fmt.Errorf("store: query duration histogram: %w", err)
	}
	return connector{dsn: dsn, duration: duration}, nil
}

func (c connector) Connect(context.Context) (driver.Conn, error) {
	dc, err := c.Driver().Open(c.dsn)
	if err != nil {
		return nil, err
	}
	sc, ok := dc.(sqliteConn)
	if !ok {
		dc.Close()
		return nil, fmt.Errorf("store: sqlite connection %T lacks a method database/sql needs", dc)
	}
	return tracedConn{sc, c.duration}, nil
}

func (c connector) Driver() driver.Driver { return &sqlite.Driver{} }

type tracedConn struct {
	sqliteConn
	duration metric.Float64Histogram
}

func (c tracedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	end := c.observe(ctx, query)
	res, err := c.sqliteConn.ExecContext(ctx, query, args)
	end(err)
	return res, err
}

func (c tracedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	end := c.observe(ctx, query)
	rows, err := c.sqliteConn.QueryContext(ctx, query, args)
	if err != nil {
		end(err)
		return nil, err
	}
	return &tracedRows{Rows: rows, end: end}, nil
}

// observe starts timing query and, under a recording span, a child span for
// it. The returned function ends both.
func (c tracedConn) observe(ctx context.Context, query string) func(error) {
	start := time.Now()
	var span trace.Span
	if trace.SpanFromContext(ctx).IsRecording() {
		_, span = otel.Tracer(scope).Start(ctx, operation(query),
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("db.system.name", "sqlite"),
				attribute.String("db.query.text", query),
			))
	}
	return func(err error) {
		c.duration.Record(ctx, time.Since(start).Seconds())
		if span == nil {
			return
		}
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}

// tracedRows ends its query's observation when closed, since SQLite does
// most of a query's work while its rows are read.
type tracedRows struct {
	driver.Rows
	end func(error)
	err error // the first error reading a row
}

func (r *tracedRows) Next(dest []driver.Value) error {
	err := r.Rows.Next(dest)
	if err != nil && err != io.EOF && r.err == nil {
		r.err = err
	}
	return err
}

func (r *tracedRows) Close() error {
	err := r.Rows.Close()
	r.end(errors.Join(r.err, err))
	return err
}

// operation returns the first keyword of query, such as SELECT, as its span
// name.
func operation(query string) string {
	q := strings.TrimLeftFunc(query, unicode.IsSpace)
	if i := strings.IndexFunc(q, unicode.IsSpace); i >= 0 {
		q = q[:i]
	}
	return strings.ToUpper(q)
}
