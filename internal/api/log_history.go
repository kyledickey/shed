package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/kyledickey/shed/internal/control"
)

type logRecordJSON struct {
	Time         time.Time         `json:"time"`
	Source       string            `json:"source"`
	Stream       string            `json:"stream"`
	Level        string            `json:"level"`
	Message      string            `json:"message"`
	DeploymentID string            `json:"deploymentId"`
	TraceID      string            `json:"traceId"`
	Fields       map[string]string `json:"fields"`
}

type logPageJSON struct {
	Records []logRecordJSON `json:"records"`
	Next    string          `json:"next"`
}

// logHistory searches the stored logs of a service.
func (s *Server) logHistory(w http.ResponseWriter, r *http.Request) error {
	v := r.URL.Query()
	q := control.LogQuery{Search: v.Get("q"), Level: v.Get("level")}
	var err error
	if q.From, err = timeParam(v.Get("from"), "from"); err != nil {
		return err
	}
	if q.To, err = timeParam(v.Get("to"), "to"); err != nil {
		return err
	}
	if q.Before, err = timeParam(v.Get("before"), "before"); err != nil {
		return err
	}
	if raw := v.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return errorf(http.StatusBadRequest, "limit must be a positive integer")
		}
		q.Limit = n
	}
	page, err := s.control.LogHistory(r.Context(), r.PathValue("id"), q)
	if err != nil {
		return err
	}
	out := logPageJSON{Records: make([]logRecordJSON, 0, len(page.Records)), Next: page.Next}
	for _, rec := range page.Records {
		out.Records = append(out.Records, logRecordJSON(rec))
	}
	return writeJSON(w, http.StatusOK, out)
}

// timeParam parses an optional RFC 3339 time.
func timeParam(v, name string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}, errorf(http.StatusBadRequest, "%s must be an RFC 3339 time", name)
	}
	return t, nil
}

// traceRedirect sends the browser to the trace in Grafana Explore.
func (s *Server) traceRedirect(w http.ResponseWriter, r *http.Request) error {
	to, err := s.control.TraceExploreURL(r.Context(), r.PathValue("traceId"))
	if err != nil {
		return err
	}
	http.Redirect(w, r, to, http.StatusFound)
	return nil
}
