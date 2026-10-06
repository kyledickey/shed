package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyledickey/shed/internal/store"
)

func TestLoadKeyGeneratesThenReuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shed.key")
	var logs bytes.Buffer
	key, from, err := loadKey(path, "", slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != store.KeySize || from != path {
		t.Fatalf("loadKey() = %d bytes from %s", len(key), from)
	}
	if !strings.Contains(logs.String(), "back it up") || !strings.Contains(logs.String(), path) {
		t.Errorf("log = %q, want a warning naming %s", logs.String(), path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("key file mode = %v, want 0600", info.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if want := base64.StdEncoding.EncodeToString(key) + "\n"; string(data) != want {
		t.Errorf("key file = %q, want %q", data, want)
	}

	logs.Reset()
	again, _, err := loadKey(path, "", slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil || !bytes.Equal(again, key) {
		t.Errorf("loadKey() again = %x, %v; want %x", again, err, key)
	}
	if logs.Len() != 0 {
		t.Errorf("loading an existing key logged %q", logs.String())
	}
}

func TestLoadKeyContents(t *testing.T) {
	key := bytes.Repeat([]byte{7}, store.KeySize)
	enc := base64.StdEncoding.EncodeToString(key)
	tests := []struct {
		name    string
		data    string
		wantErr bool
	}{
		{"plain", enc, false},
		{"whitespace", "  \n" + enc + "\r\n\n", false},
		{"empty", "", true},
		{"not base64", "not a key!", true},
		{"short", base64.StdEncoding.EncodeToString(key[:16]), true},
		{"long", base64.StdEncoding.EncodeToString(append(key, 0)), true},
		{"raw bytes", string(key), true},
		{"unpadded", base64.RawStdEncoding.EncodeToString(key), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "shed.key")
			if err := os.WriteFile(path, []byte(tt.data), 0o600); err != nil {
				t.Fatal(err)
			}
			got, _, err := loadKey(path, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), path) {
					t.Errorf("loadKey() error = %v, want one naming %s", err, path)
				}
				return
			}
			if err != nil || !bytes.Equal(got, key) {
				t.Errorf("loadKey() = %x, %v; want %x", got, err, key)
			}
		})
	}
}

func TestLoadKeyCredential(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shed.key")
	creds := t.TempDir()
	t.Setenv("CREDENTIALS_DIRECTORY", creds)
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))

	key := bytes.Repeat([]byte{9}, store.KeySize)
	cred := filepath.Join(creds, "shed.key")
	if err := os.WriteFile(cred, []byte(base64.StdEncoding.EncodeToString(key)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, from, err := loadKey(path, os.Getenv("CREDENTIALS_DIRECTORY"), discard)
	if err != nil || !bytes.Equal(got, key) || from != cred {
		t.Errorf("loadKey() = %x from %s, %v; want the credential", got, from, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("loadKey() with a credential touched %s: %v", path, err)
	}

	// A bad credential is an error, not a reason to fall back.
	if err := os.WriteFile(cred, []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadKey(path, creds, discard); err == nil {
		t.Error("loadKey() with a bad credential succeeded")
	}

	// A credentials directory without the key falls back to the key file.
	if _, from, err := loadKey(path, t.TempDir(), discard); err != nil || from != path {
		t.Errorf("loadKey() without a credential = from %s, %v; want %s", from, err, path)
	}
}
