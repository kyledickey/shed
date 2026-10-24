package api

import (
	"net/http"
)

func (s *Server) getOnePassword(w http.ResponseWriter, r *http.Request) error {
	st, err := s.control.OnePassword(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toOnePassword(st))
}

// putOnePassword connects 1Password with a service account token, moving
// every service's variables into its vault.
func (s *Server) putOnePassword(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	st, err := s.control.ConnectOnePassword(r.Context(), in.Token)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toOnePassword(st))
}

// deleteOnePassword disconnects 1Password, copying the variables back into
// shed.db.
func (s *Server) deleteOnePassword(w http.ResponseWriter, r *http.Request) error {
	st, err := s.control.DisconnectOnePassword(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toOnePassword(st))
}
