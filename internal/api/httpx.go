package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"

	"github.com/kyledickey/shed/internal/backup"
	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/store"
)

// securityHeaders sets headers that forbid framing the dashboard
// (clickjacking), MIME sniffing, and cross-origin referrers on every
// response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

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

// msgFenced explains why a fenced service cannot be started, deployed, or
// restored into.
const msgFenced = "a restore of this service failed and its data may be incomplete; " +
	"restart shed to retry recovering it, or clear the restore fence to keep the data as it is"

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
	case errors.Is(err, deploy.ErrImageUnavailable):
		status, msg = http.StatusConflict, "the deployment's image is no longer on the server; deploy again to build or pull it"
	case errors.Is(err, deploy.ErrNoImage):
		status, msg = http.StatusConflict, "deployment has no image to redeploy"
	case errors.Is(err, deploy.ErrNoContainer):
		status, msg = http.StatusConflict, "nothing to run; deploy the service first"
	case errors.Is(err, deploy.ErrServiceStopped):
		status, msg = http.StatusConflict, "service is stopped; start it instead"
	case errors.Is(err, deploy.ErrServiceBusy):
		status, msg = http.StatusConflict, "service is busy with a backup or restore; try again when it finishes"
	case errors.Is(err, deploy.ErrFenced), errors.Is(err, backup.ErrFenced):
		status, msg = http.StatusConflict, msgFenced
	case errors.Is(err, deploy.ErrDeleting):
		status, msg = http.StatusConflict, "service is being deleted"
	case errors.Is(err, deploy.ErrStopped), errors.Is(err, backup.ErrStopped):
		status, msg = http.StatusServiceUnavailable, "shutting down"
	case errors.Is(err, backup.ErrInvalid):
		status, msg = http.StatusBadRequest, err.Error()
	case errors.Is(err, backup.ErrNoVolumes):
		status, msg = http.StatusBadRequest, "service has no volumes to back up"
	case errors.Is(err, backup.ErrBusy):
		status, msg = http.StatusConflict, "a backup or restore is already in progress"
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
