package api

import (
	"net/http"
	"time"

	"github.com/kyledickey/shed/internal/control"
	"github.com/kyledickey/shed/internal/store"
)

type notificationChannelJSON struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Provider  string    `json:"provider"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
}

type notificationDeliveryJSON struct {
	ID          string     `json:"id"`
	ChannelID   string     `json:"channelId"`
	ChannelName string     `json:"channelName"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	LastError   string     `json:"lastError"`
	CreatedAt   time.Time  `json:"createdAt"`
	SentAt      *time.Time `json:"sentAt"`
}

// The target is a secret, so no response carries it.
func toNotificationChannel(c store.NotificationChannel) notificationChannelJSON {
	return notificationChannelJSON{ID: c.ID, Name: c.Name, Provider: c.Provider, Enabled: c.Enabled, CreatedAt: c.CreatedAt}
}

func (s *Server) listNotificationProviders(w http.ResponseWriter, r *http.Request) error {
	names := s.control.NotificationProviders()
	if names == nil {
		names = []string{}
	}
	return writeJSON(w, http.StatusOK, names)
}

func (s *Server) listNotificationChannels(w http.ResponseWriter, r *http.Request) error {
	cs, err := s.control.NotificationChannels(r.Context())
	if err != nil {
		return err
	}
	out := make([]notificationChannelJSON, 0, len(cs))
	for _, c := range cs {
		out = append(out, toNotificationChannel(c))
	}
	return writeJSON(w, http.StatusOK, out)
}

type newNotificationChannelRequest struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Target   string `json:"target"`
	Enabled  bool   `json:"enabled"`
}

func (s *Server) createNotificationChannel(w http.ResponseWriter, r *http.Request) error {
	var req newNotificationChannelRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	c, err := s.control.CreateNotificationChannel(r.Context(), control.NotificationChannelInput(req))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, toNotificationChannel(c))
}

// notificationChannelPatch is a partial update. An empty target keeps the
// stored one.
type notificationChannelPatch struct {
	Name    *string `json:"name"`
	Target  string  `json:"target"`
	Enabled *bool   `json:"enabled"`
}

func (s *Server) patchNotificationChannel(w http.ResponseWriter, r *http.Request) error {
	var req notificationChannelPatch
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	c, err := s.control.UpdateNotificationChannel(r.Context(), r.PathValue("id"), control.NotificationChannelPatch(req))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toNotificationChannel(c))
}

func (s *Server) deleteNotificationChannel(w http.ResponseWriter, r *http.Request) error {
	if err := s.control.DeleteNotificationChannel(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) testNotificationChannel(w http.ResponseWriter, r *http.Request) error {
	if err := s.control.TestNotificationChannel(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

const (
	defaultDeliveries = 50
	maxDeliveries     = 200
)

func (s *Server) listNotificationDeliveries(w http.ResponseWriter, r *http.Request) error {
	limit, err := positiveInt(r.URL.Query().Get("limit"), "limit")
	if err != nil {
		return err
	}
	if limit == 0 {
		limit = defaultDeliveries
	}
	ds, err := s.control.NotificationDeliveries(r.Context(), int(min(limit, maxDeliveries)))
	if err != nil {
		return err
	}
	out := make([]notificationDeliveryJSON, 0, len(ds))
	for _, d := range ds {
		out = append(out, notificationDeliveryJSON{
			ID: d.ID, ChannelID: d.ChannelID, ChannelName: d.ChannelName, Title: d.Title, Body: d.Body,
			Status: string(d.Status), Attempts: d.Attempts, LastError: d.LastError,
			CreatedAt: d.CreatedAt, SentAt: d.SentAt,
		})
	}
	return writeJSON(w, http.StatusOK, out)
}
