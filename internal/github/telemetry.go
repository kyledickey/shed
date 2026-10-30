package github

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// tracedTransport sends requests through base, each in a client span. It
// sends no trace context to GitHub.
func tracedTransport(base http.RoundTripper) http.RoundTripper {
	return otelhttp.NewTransport(publicURL{base},
		otelhttp.WithPropagators(propagation.NewCompositeTextMapPropagator()))
}

// publicURL overwrites the url.full attribute of the request's span with the
// URL stripped of user info, query, and fragment, before any of it is
// exported.
type publicURL struct{ next http.RoundTripper }

func (t publicURL) RoundTrip(r *http.Request) (*http.Response, error) {
	u := *r.URL
	u.User, u.RawQuery, u.ForceQuery, u.Fragment = nil, "", false, ""
	trace.SpanFromContext(r.Context()).SetAttributes(attribute.String("url.full", u.String()))
	return t.next.RoundTrip(r)
}
