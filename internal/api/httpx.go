package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"

	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/store"
)

// handlerFunc is an HTTP handler that reports failure by returning an error.
type handlerFunc func(w http.ResponseWriter, r *http.Request) error

// httpError is an error with the status and message to send to the client.
type httpError struct {
	status int
	msg    string
}

func (e *httpError) Error() string { return e.msg }

func errorf(status int, format string, args ...any) error {
	return &httpError{status: status, msg: fmt.Sprintf(format, args...)}
}

// handle adapts h to http.Handler, sending its error as JSON.
func (s *Server) handle(h handlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			s.writeError(w, r, err)
		}
	})
}

// writeError maps err to a status and sends it as {"error": message}.
// Unexpected errors are logged and reported generically.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var he *httpError
	status, msg := http.StatusInternalServerError, "internal error"
	switch {
	case errors.As(err, &he):
		status, msg = he.status, he.msg
	case errors.Is(err, store.ErrNotFound):
		status, msg = http.StatusNotFound, "not found"
	case errors.Is(err, store.ErrConflict):
		status, msg = http.StatusConflict, "already exists"
	case errors.Is(err, deploy.ErrNotInProgress):
		status, msg = http.StatusConflict, "deployment is not in progress"
	case errors.Is(err, deploy.ErrNoImage):
		status, msg = http.StatusConflict, "deployment has no image to redeploy"
	case errors.Is(err, deploy.ErrNoContainer):
		status, msg = http.StatusConflict, "nothing to run; deploy the service first"
	case errors.Is(err, deploy.ErrServiceStopped):
		status, msg = http.StatusConflict, "service is stopped; start it instead"
	case errors.Is(err, deploy.ErrServiceBusy):
		status, msg = http.StatusConflict, "service is busy with a backup or restore; try again when it finishes"
	case errors.Is(err, deploy.ErrStopped):
		status, msg = http.StatusServiceUnavailable, "shutting down"
	default:
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeJSON sends v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

// maxBody bounds JSON request bodies.
const maxBody = 1 << 20

// decodeJSON decodes the request body into v. An empty body leaves v
// unchanged.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(v)
	if err != nil && !errors.Is(err, io.EOF) {
		return errorf(http.StatusBadRequest, "invalid JSON body: %v", err)
	}
	return nil
}

// requireJSON rejects mutating requests whose body is not declared as JSON.
// Browsers cannot send that content type cross-site without a CORS
// preflight, which complements the SameSite=Lax session cookie.
func requireJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if mt != "application/json" {
				writeJSON(w, http.StatusUnsupportedMediaType,
					map[string]string{"error": "Content-Type must be application/json"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// protectMutations checks the configured dashboard origin, including for empty
// requests. Clients without browser headers still need JSON for POST/PUT/PATCH.
func protectMutations(baseURL string, next http.Handler) http.Handler {
	u, _ := url.Parse(baseURL)
	origin := u.Scheme + "://" + u.Host
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			got, site := r.Header.Get("Origin"), r.Header.Get("Sec-Fetch-Site")
			if got != "" && got != origin || got == "" && site != "" && site != "same-origin" {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "untrusted request origin"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
