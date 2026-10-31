package docker

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/moby/moby/client"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const scope = "github.com/kyledickey/shed/internal/docker"

// operations names the Engine API requests shed makes, by method and
// normalized path. Others are named by method and normalized path.
var operations = map[string]string{
	"GET /_ping":                        "ping",
	"HEAD /_ping":                       "ping",
	"GET /info":                         "info",
	"GET /networks":                     "network_list",
	"POST /networks/create":             "network_create",
	"GET /networks/{id}":                "network_inspect",
	"DELETE /networks/{id}":             "network_remove",
	"POST /networks/{id}/connect":       "network_connect",
	"POST /networks/{id}/disconnect":    "network_disconnect",
	"POST /volumes/create":              "volume_create",
	"GET /volumes/{id}":                 "volume_inspect",
	"DELETE /volumes/{id}":              "volume_remove",
	"POST /images/create":               "image_pull",
	"GET /images/json":                  "image_list",
	"GET /images/{id}/json":             "image_inspect",
	"DELETE /images/{id}":               "image_remove",
	"POST /containers/create":           "container_create",
	"GET /containers/json":              "container_list",
	"GET /containers/{id}/json":         "container_inspect",
	"POST /containers/{id}/start":       "container_start",
	"POST /containers/{id}/stop":        "container_stop",
	"POST /containers/{id}/restart":     "container_restart",
	"DELETE /containers/{id}":           "container_remove",
	"GET /containers/{id}/logs":         "container_logs",
	"GET /containers/{id}/stats":        "container_stats",
	"PUT /containers/{id}/archive":      "container_copy_to",
	"GET /containers/{id}/archive":      "container_copy_from",
	"HEAD /containers/{id}/archive":     "container_stat_path",
	"POST /containers/{id}/exec":        "exec_create",
	"POST /exec/{id}/start":             "exec_start",
	"GET /exec/{id}/json":               "exec_inspect",
	"POST /containers/{id}/attach":      "container_attach",
	"POST /containers/{id}/wait":        "container_wait",
	"POST /containers/{id}/kill":        "container_kill",
	"POST /containers/{id}/exec/resize": "exec_resize",
}

// collectionActions follow a collection directly, as in /containers/create.
var collectionActions = map[string]bool{"create": true, "json": true, "prune": true, "load": true, "search": true}

// objectActions end the path of a request about one object, as in
// /containers/{id}/start.
var objectActions = map[string]bool{
	"json": true, "start": true, "stop": true, "restart": true, "kill": true, "wait": true,
	"logs": true, "stats": true, "exec": true, "archive": true, "connect": true,
	"disconnect": true, "push": true, "tag": true, "history": true, "get": true,
	"resize": true, "attach": true, "top": true, "changes": true, "export": true,
	"update": true, "rename": true, "pause": true, "unpause": true,
}

// operation names a request to the Engine API without the object IDs and
// image names in its path, which would make too many distinct names.
func operation(method, path string) string {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	if len(segs) > 0 && isVersion(segs[0]) {
		segs = segs[1:]
	}
	norm := "/" + segs[0]
	switch rest := segs[1:]; {
	case len(rest) == 0:
	case len(rest) == 1 && collectionActions[rest[0]]:
		norm += "/" + rest[0]
	case len(rest) > 1 && objectActions[rest[len(rest)-1]]:
		norm += "/{id}/" + rest[len(rest)-1]
	default:
		norm += "/{id}"
	}
	key := method + " " + norm
	if op, ok := operations[key]; ok {
		return op
	}
	return key
}

// isVersion reports whether s is an API version path segment such as v1.47.
func isVersion(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	return strings.Trim(s[1:], "0123456789.") == ""
}

type startKey struct{}

type started struct {
	op string
	at time.Time
}

// instrument returns client options that name the client's spans, which it
// sends to the global tracer, by operation, and that record
// shed_docker_request_duration_seconds up to the response headers.
// Requests that fail without a response are not timed. The spans' url.full
// attribute keeps the query string, which for the Engine API holds only
// filters, image references, and flags; registry credentials travel in a
// header, which is not recorded.
func instrument() ([]client.Opt, error) {
	duration, err := otel.Meter(scope).Float64Histogram("shed_docker_request_duration_seconds",
		metric.WithDescription("Duration of Docker Engine API requests until their response headers."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 300))
	if err != nil {
		return nil, fmt.Errorf("docker: request duration histogram: %w", err)
	}
	return []client.Opt{
		client.WithTraceOptions(otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return operation(r.Method, r.URL.Path)
		})),
		// The request hook's request is the one sent, so the start it
		// carries reaches the response.
		client.WithHTTPRequestHook(func(r *http.Request) error {
			s := started{operation(r.Method, r.URL.Path), time.Now()}
			*r = *r.WithContext(context.WithValue(r.Context(), startKey{}, s))
			return nil
		}),
		client.WithHTTPResponseHook(func(resp *http.Response) {
			if resp.Request == nil {
				return
			}
			ctx := resp.Request.Context()
			if s, ok := ctx.Value(startKey{}).(started); ok {
				duration.Record(ctx, time.Since(s.at).Seconds(), metric.WithAttributes(attribute.String("op", s.op)))
			}
		}),
	}, nil
}
