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

// postOnePasswordVaults lists the vaults a service account token can
// access, so the operator can choose one.
func (s *Server) postOnePasswordVaults(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	vaults, err := s.control.OnePasswordVaults(r.Context(), in.Token)
	if err != nil {
		return err
	}
	out := make([]onePasswordVaultJSON, 0, len(vaults))
	for _, v := range vaults {
		out = append(out, onePasswordVaultJSON{ID: v.ID, Name: v.Name})
	}
	return writeJSON(w, http.StatusOK, out)
}

// putOnePassword connects 1Password with a service account token, moving
// every service's variables into a vault it can access.
func (s *Server) putOnePassword(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Token string `json:"token"`
		Vault string `json:"vault"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	st, err := s.control.ConnectOnePassword(r.Context(), in.Token, in.Vault)
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
