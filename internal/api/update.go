package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/kyledickey/shed/internal/update"
)

type releaseJSON struct {
	Version     string    `json:"version"`
	URL         string    `json:"url"`
	Notes       string    `json:"notes"`
	PublishedAt time.Time `json:"publishedAt"`
}

type updateStatusJSON struct {
	Current      string         `json:"current"`
	Latest       *releaseJSON   `json:"latest"`
	Available    bool           `json:"available"`
	CheckedAt    *time.Time     `json:"checkedAt"`
	State        update.State   `json:"state"`
	Staged       string         `json:"staged"`
	Error        string         `json:"error"`
	AutoDownload bool           `json:"autoDownload"`
	Channel      update.Channel `json:"channel"`
	Unsupported  string         `json:"unsupported"`
}

func toUpdateStatus(st update.Status) updateStatusJSON {
	out := updateStatusJSON{
		Current:      st.Current,
		Available:    st.Available,
		State:        st.State,
		Staged:       st.Staged,
		Error:        st.Err,
		AutoDownload: st.AutoDownload,
		Channel:      st.Channel,
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
	st, err := s.control.UpdateStatus(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toUpdateStatus(st))
}

// checkUpdate checks GitHub now. A failed check is reported in the status
// rather than as an error, like a failed background check.
func (s *Server) checkUpdate(w http.ResponseWriter, r *http.Request) error {
	st, err := s.control.CheckUpdate(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toUpdateStatus(st))
}

func (s *Server) downloadUpdate(w http.ResponseWriter, r *http.Request) error {
	st, err := s.control.DownloadUpdate(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusAccepted, toUpdateStatus(st))
}

func (s *Server) putUpdateSettings(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		AutoDownload *bool           `json:"autoDownload"`
		Channel      *update.Channel `json:"channel"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if in.AutoDownload == nil && in.Channel == nil {
		return errorf(http.StatusBadRequest, "autoDownload or channel is required")
	}
	var st update.Status
	var err error
	// The channel goes first: it is the one that can be invalid.
	if in.Channel != nil {
		if st, err = s.control.SetUpdateChannel(r.Context(), *in.Channel); err != nil {
			return err
		}
	}
	if in.AutoDownload != nil {
		if st, err = s.control.SetAutoDownload(r.Context(), *in.AutoDownload); err != nil {
			return err
		}
	}
	return writeJSON(w, http.StatusOK, toUpdateStatus(st))
}

// installUpdate swaps in the downloaded binary, responds, and then restarts
// shed. Shutting down waits for this response to be written.
func (s *Server) installUpdate(w http.ResponseWriter, r *http.Request) error {
	st, err := s.control.InstallUpdate(r.Context())
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
