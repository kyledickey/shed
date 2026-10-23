package api

import (
	"net/http"

	"github.com/kyledickey/shed/internal/control"
)

type onePasswordJSON struct {
	Connected bool `json:"connected"`
}

func toOnePassword(st control.OnePasswordStatus) onePasswordJSON {
	return onePasswordJSON{Connected: st.Connected}
}

func (s *Server) getOnePassword(w http.ResponseWriter, r *http.Request) error {
	st, err := s.control.OnePassword(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toOnePassword(st))
}

// putOnePassword connects 1Password with a service account token, which is
// saved only if 1Password accepts it.
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

func (s *Server) deleteOnePassword(w http.ResponseWriter, r *http.Request) error {
	if err := s.control.DisconnectOnePassword(r.Context()); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) getOnePasswordVariables(w http.ResponseWriter, r *http.Request) error {
	vars, err := s.control.OnePasswordVariables(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, vars)
}
