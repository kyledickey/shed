package api

import (
	"io"
	"mime"
	"net/http"

	"github.com/kyledickey/shed/internal/backup"
	"github.com/kyledickey/shed/internal/control"
)

func toBackupList(l control.BackupList) (backupPolicyJSON, []backupJSON) {
	backups := make([]backupJSON, 0, len(l.Backups))
	for _, b := range l.Backups {
		backups = append(backups, toBackup(b))
	}
	return toBackupPolicy(l.Policy), backups
}

func (s *Server) serviceBackups(w http.ResponseWriter, r *http.Request) error {
	l, err := s.control.ServiceBackups(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	var out serviceBackupsJSON
	out.Policy, out.Backups = toBackupList(l)
	if l.Restore != nil {
		v := toRestore(*l.Restore)
		out.Restore = &v
	}
	return writeJSON(w, http.StatusOK, out)
}

func (s *Server) systemBackups(w http.ResponseWriter, r *http.Request) error {
	l, err := s.control.SystemBackups(r.Context())
	if err != nil {
		return err
	}
	var out systemBackupsJSON
	out.Policy, out.Backups = toBackupList(l)
	return writeJSON(w, http.StatusOK, out)
}

func (s *Server) putServiceBackupPolicy(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.control.ServiceRecord(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	var in backupPolicyInput
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	policy, err := s.control.SetServiceBackupPolicy(r.Context(), r.PathValue("id"), backup.PolicyInput(in))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toBackupPolicy(policy))
}

func (s *Server) putSystemBackupPolicy(w http.ResponseWriter, r *http.Request) error {
	var in backupPolicyInput
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	policy, err := s.control.SetSystemBackupPolicy(r.Context(), backup.PolicyInput(in))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toBackupPolicy(policy))
}

func (s *Server) runServiceBackup(w http.ResponseWriter, r *http.Request) error {
	b, err := s.control.BackUpService(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusAccepted, toBackup(b))
}

func (s *Server) runSystemBackup(w http.ResponseWriter, r *http.Request) error {
	b, err := s.control.BackUpSystem(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusAccepted, toBackup(b))
}

// downloadBackup streams the decrypted, still compressed archive.
func (s *Server) downloadBackup(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := r.PathValue("id")
	rc, name, err := s.control.OpenBackup(ctx, id)
	if err != nil {
		return err
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/zstd")
	w.Header().Set("Content-Disposition",
		mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := io.Copy(w, rc); err != nil && ctx.Err() == nil {
		// The status is sent; all that is left is to cut the stream short.
		s.log.Error("stream backup", "backup", id, "err", err)
	}
	return nil
}

func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request) error {
	rs, err := s.control.RestoreBackup(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusAccepted, toRestore(rs))
}

func (s *Server) deleteBackup(w http.ResponseWriter, r *http.Request) error {
	if err := s.control.DeleteBackup(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) getBackupSettings(w http.ResponseWriter, r *http.Request) error {
	st, err := s.control.BackupSettings(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toBackupSettings(st))
}

func (s *Server) putBackupSettings(w http.ResponseWriter, r *http.Request) error {
	var in backupSettingsInput
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if in.Encryption == nil {
		return errorf(http.StatusBadRequest, "encryption is required")
	}
	input := backup.SettingsInput{Encrypt: in.Encryption.Enabled}
	if in.S3 != nil {
		v := backup.S3Input(*in.S3)
		input.S3 = &v
	}
	st, err := s.control.SetBackupSettings(r.Context(), input)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toBackupSettings(st))
}

// testBackupSettings checks the S3 destination of a settings body without
// saving it.
func (s *Server) testBackupSettings(w http.ResponseWriter, r *http.Request) error {
	var in backupSettingsInput
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if in.S3 == nil {
		return errorf(http.StatusBadRequest, "there is no S3 destination to test")
	}
	if err := s.control.TestBackupS3(r.Context(), backup.S3Input(*in.S3)); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) getBackupKey(w http.ResponseWriter, r *http.Request) error {
	id, err := s.control.BackupIdentity(r.Context())
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	return writeJSON(w, http.StatusOK, map[string]string{"identity": id})
}

func toBackupSettings(st backup.Settings) backupSettingsJSON {
	var out backupSettingsJSON
	if st.S3 != nil {
		v := s3SettingsJSON(*st.S3)
		out.S3 = &v
	}
	out.Encryption.Enabled = st.Encrypt
	out.Encryption.Recipient = st.Recipient
	return out
}
