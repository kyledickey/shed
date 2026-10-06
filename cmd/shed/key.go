package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/kyledickey/shed/internal/store"
)

// credentialName is the name of the key in systemd's credentials directory.
const credentialName = "shed.key"

// loadKey returns the key that encrypts secrets in the database, and the
// path it was read from. A key passed as a systemd credential, in
// credentialsDir, takes precedence. Otherwise the key is read from path, and
// generated there if the file does not exist.
func loadKey(path, credentialsDir string, log *slog.Logger) ([]byte, string, error) {
	if credentialsDir != "" {
		cred := filepath.Join(credentialsDir, credentialName)
		data, err := os.ReadFile(cred)
		if err == nil {
			key, err := parseKey(data)
			if err != nil {
				return nil, "", fmt.Errorf("key %s: %w", cred, err)
			}
			return key, cred, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, "", fmt.Errorf("read key: %w", err)
		}
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		key, err := generateKey(path)
		if err != nil {
			return nil, "", err
		}
		log.Warn("generated the key that encrypts secrets in shed.db; back it up off this server, "+
			"since without it variables, GitHub App credentials, and backup settings in shed.db can't be read",
			"path", path)
		return key, path, nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("read key: %w", err)
	}
	key, err := parseKey(data)
	if err != nil {
		return nil, "", fmt.Errorf("key %s: %w", path, err)
	}
	return key, path, nil
}

// parseKey decodes a key file: the standard base64 of the key, with
// surrounding whitespace.
func parseKey(data []byte) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(key) != store.KeySize {
		return nil, fmt.Errorf("want the base64 of %d bytes", store.KeySize)
	}
	return key, nil
}

// generateKey writes a new random key to path, which must not exist.
func generateKey(path string) ([]byte, error) {
	key := make([]byte, store.KeySize)
	rand.Read(key)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	_, err = f.WriteString(base64.StdEncoding.EncodeToString(key) + "\n")
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		// The key must be durable before the database is encrypted with it.
		err = syncDir(filepath.Dir(path))
	}
	if err != nil {
		os.Remove(path)
		return nil, fmt.Errorf("generate key: write %s: %w", path, err)
	}
	return key, nil
}

// syncDir flushes a directory's entries to stable storage.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}
