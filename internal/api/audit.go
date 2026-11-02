package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/store"
)

type auditEventJSON struct {
	ID        int64           `json:"id"`
	Time      time.Time       `json:"ts"`
	ActorKind string          `json:"actorKind"`
	Actor     string          `json:"actor"`
	Action    string          `json:"action"`
	Outcome   string          `json:"outcome"`
	ProjectID string          `json:"projectId"`
	ServiceID string          `json:"serviceId"`
	Target    string          `json:"target"`
	Detail    json.RawMessage `json:"detail"`
}

type auditPageJSON struct {
	Events []auditEventJSON `json:"events"`
	Next   *int64           `json:"next"`
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	f := store.AuditFilter{
		Actor:     q.Get("actor"),
		Action:    q.Get("action"),
		ServiceID: q.Get("service"),
		Outcome:   q.Get("outcome"),
	}
	var err error
	if f.Before, err = positiveInt(q.Get("before"), "before"); err != nil {
		return err
	}
	limit, err := positiveInt(q.Get("limit"), "limit")
	if err != nil {
		return err
	}
	f.Limit = int(min(limit, audit.MaxLimit))
	page, err := s.control.AuditEvents(r.Context(), f)
	if err != nil {
		return err
	}
	out := auditPageJSON{Events: make([]auditEventJSON, 0, len(page.Events))}
	for _, e := range page.Events {
		out.Events = append(out.Events, auditEventJSON{
			ID: e.ID, Time: e.Time, ActorKind: e.ActorKind, Actor: e.Actor,
			Action: e.Action, Outcome: e.Outcome, ProjectID: e.ProjectID, ServiceID: e.ServiceID,
			Target: e.Target, Detail: json.RawMessage(e.Detail),
		})
	}
	if page.Next != 0 {
		out.Next = &page.Next
	}
	return writeJSON(w, http.StatusOK, out)
}

func (s *Server) getAuditSettings(w http.ResponseWriter, r *http.Request) error {
	st, err := s.control.AuditSettings(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, st)
}

func (s *Server) putAuditSettings(w http.ResponseWriter, r *http.Request) error {
	var in audit.Settings
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	st, err := s.control.SetAuditSettings(r.Context(), in)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, st)
}

// positiveInt parses an optional positive integer parameter; empty is 0.
func positiveInt(v, name string) (int64, error) {
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 1 {
		return 0, errorf(http.StatusBadRequest, "%s must be a positive integer", name)
	}
	return n, nil
}

// withAnonymous makes every request's audit actor anonymous until
// authentication names someone.
func withAnonymous(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := audit.WithActor(r.Context(), audit.Actor{Kind: audit.Anonymous})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// deny records a denied anonymous request.
func (s *Server) deny(r *http.Request, action string) {
	s.control.Record(r.Context(), audit.Event{Action: action, Outcome: audit.Denied})
}
