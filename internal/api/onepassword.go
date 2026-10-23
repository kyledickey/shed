package api

import (
	"net/http"
)

// putOnePassword links a service to a 1Password Environment, which is saved
// only if the service account token can read it.
func (s *Server) putOnePassword(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Environment string `json:"environment"`
		Token       string `json:"token"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	svc, err := s.control.LinkOnePassword(r.Context(), r.PathValue("id"), in.Environment, in.Token)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toService(svc))
}

func (s *Server) deleteOnePassword(w http.ResponseWriter, r *http.Request) error {
	svc, err := s.control.UnlinkOnePassword(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toService(svc))
}

func (s *Server) getOnePasswordVariables(w http.ResponseWriter, r *http.Request) error {
	vars, err := s.control.OnePasswordVariables(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, vars)
}
