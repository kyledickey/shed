package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/kyledickey/shed/internal/update"
)

// Updates checks for, downloads, and installs new versions of shed.
// *update.Updater implements it.
type Updates interface {
	Status(ctx context.Context) (update.Status, error)
	Check(ctx context.Context) (update.Status, error)
	Download(ctx context.Context) (update.Status, error)
	SetAutoDownload(ctx context.Context, on bool) (update.Status, error)
	Install(ctx context.Context) (update.Status, error)
}

var _ Updates = (*update.Updater)(nil)

type releaseJSON struct {
	Version     string    `json:"version"`
	URL         string    `json:"url"`
	Notes       string    `json:"notes"`
	PublishedAt time.Time `json:"publishedAt"`
}

type updateStatusJSON struct {
	Current      string       `json:"current"`
	Latest       *releaseJSON `json:"latest"`
	Available    bool         `json:"available"`
	CheckedAt    *time.Time   `json:"checkedAt"`
	State        update.State `json:"state"`
	Staged       string       `json:"staged"`
	Error        string       `json:"error"`
	AutoDownload bool         `json:"autoDownload"`
	Unsupported  string       `json:"unsupported"`
}

func toUpdateStatus(st update.Status) updateStatusJSON {
	out := updateStatusJSON{
		Current:      st.Current,
		Available:    st.Available,
		State:        st.State,
		Staged:       st.Staged,
		Error:        st.Err,
		AutoDownload: st.AutoDownload,
		Unsupported:  st.Unsupported,
	}
	if r := st.Latest; r != nil {
		out.Latest = &releaseJSON{Version: r.Version, URL: r.URL, Notes: r.Notes, PublishedAt: r.PublishedAt}
	}
	if !st.CheckedAt.IsZero() {
		out.CheckedAt = &st.CheckedAt
	}
	return out
}

func (s *Server) getUpdate(w http.ResponseWriter, r *http.Request) error {
	st, err := s.updates.Status(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toUpdateStatus(st))
}

// checkUpdate checks GitHub now. A failed check is reported in the status
// rather than as an error, like a failed background check.
func (s *Server) checkUpdate(w http.ResponseWriter, r *http.Request) error {
	_, err := s.updates.Check(r.Context())
	switch {
	case errors.Is(err, update.ErrUnsupported), errors.Is(err, update.ErrBusy):
		return err
	case err != nil:
		s.log.Warn("update check failed", "err", err)
	}
	return s.getUpdate(w, r)
}

func (s *Server) downloadUpdate(w http.ResponseWriter, r *http.Request) error {
	st, err := s.updates.Download(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusAccepted, toUpdateStatus(st))
}

func (s *Server) putUpdateSettings(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		AutoDownload *bool `json:"autoDownload"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if in.AutoDownload == nil {
		return errorf(http.StatusBadRequest, "autoDownload is required")
	}
	st, err := s.updates.SetAutoDownload(r.Context(), *in.AutoDownload)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toUpdateStatus(st))
}

// installUpdate swaps in the downloaded binary, responds, and then restarts
// shed. Shutting down waits for this response to be written.
func (s *Server) installUpdate(w http.ResponseWriter, r *http.Request) error {
	st, err := s.updates.Install(r.Context())
	if errors.Is(err, update.ErrUnsupported) || errors.Is(err, update.ErrBusy) || errors.Is(err, update.ErrNotStaged) {
		return err
	}
	if err != nil {
		s.log.Error("install update", "err", err)
		return errorf(http.StatusInternalServerError, "%v", err)
	}
	s.log.Info("restarting into update", "version", st.Staged)
	if err := writeJSON(w, http.StatusAccepted, toUpdateStatus(st)); err != nil {
		return err
	}
	s.restart()
	return nil
}
