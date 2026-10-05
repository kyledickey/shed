package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"aead.dev/minisign"
)

type memSettings struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *memSettings) Setting(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	return v, ok, nil
}

func (s *memSettings) SetSetting(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = make(map[string]string)
	}
	s.m[key] = value
	return nil
}

// release is a fake GitHub release served by a test server.
type release struct {
	tag     string
	archive []byte // tar.gz; nil means a working one for tag
	sums    []byte // nil means the archive's correct checksums
	sig     []byte // nil means a valid signature of sums
	status  int    // of the latest-release endpoint; 0 means 200
}

type fixture struct {
	t      *testing.T
	pub    string
	priv   minisign.PrivateKey
	srv    *httptest.Server
	binary string
	dir    string

	mu  sync.Mutex
	rel release
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	text, err := pub.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, pub: string(text), priv: priv}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	bin := t.TempDir()
	f.binary = filepath.Join(bin, "shed")
	if err := os.WriteFile(f.binary, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.dir = filepath.Join(t.TempDir(), "updates")
	return f
}

func (f *fixture) set(r release) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rel = r
}

// assets returns the archive, checksums, and signature of the release.
func (f *fixture) assets() (archive, sums, sig []byte) {
	f.mu.Lock()
	r := f.rel
	f.mu.Unlock()
	archive = r.archive
	if archive == nil {
		archive = tarball(f.t, "#!/bin/sh\necho "+r.tag+"\n")
	}
	sums = r.sums
	if sums == nil {
		sum := sha256.Sum256(archive)
		sums = fmt.Appendf(nil, "%x  %s\n%x  other.tar.gz\n", sum, archiveName(r.tag), sum)
	}
	sig = r.sig
	if sig == nil {
		sig = minisign.Sign(f.priv, sums)
	}
	return archive, sums, sig
}

func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	rel := f.rel
	f.mu.Unlock()
	archive, sums, sig := f.assets()
	switch r.URL.Path {
	case "/repos/o/shed/releases/latest":
		if rel.status != 0 {
			w.WriteHeader(rel.status)
			return
		}
		asset := func(name string) map[string]string {
			return map[string]string{"name": name, "browser_download_url": f.srv.URL + "/dl/" + name}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name":     rel.tag,
			"html_url":     "https://github.com/o/shed/releases/tag/" + rel.tag,
			"body":         " notes \n",
			"published_at": "2026-10-01T12:00:00Z",
			"assets":       []any{asset(archiveName(rel.tag)), asset(checksumsAsset), asset(signatureAsset)},
		})
	case "/dl/" + archiveName(rel.tag):
		w.Write(archive)
	case "/dl/" + checksumsAsset:
		w.Write(sums)
	case "/dl/" + signatureAsset:
		w.Write(sig)
	default:
		http.NotFound(w, r)
	}
}

func (f *fixture) updater(version string, settings *memSettings) *Updater {
	f.t.Helper()
	if settings == nil {
		settings = &memSettings{}
	}
	u, err := New(Config{
		Repo:      "o/shed",
		Version:   version,
		PublicKey: f.pub,
		Binary:    f.binary,
		Dir:       f.dir,
		Settings:  settings,
		APIURL:    f.srv.URL,
		Log:       slog.New(slog.DiscardHandler),
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return u
}

func tarball(t *testing.T, shed string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, file := range []struct{ name, body string }{
		{"shed.service", "[Unit]\n"},
		{"shed", shed},
	} {
		if err := tw.WriteHeader(&tar.Header{Name: file.name, Mode: 0o755, Size: int64(len(file.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		io.WriteString(tw, file.body)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// waitIdle runs u's pending download and returns the status after it.
func waitIdle(t *testing.T, u *Updater) Status {
	t.Helper()
	select {
	case <-u.kick:
		u.download(t.Context())
	default:
	}
	st, err := u.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		rel       release
		available bool
		latest    string
		wantErr   bool
	}{
		{name: "newer", version: "v1.0.0", rel: release{tag: "v1.1.0"}, available: true, latest: "v1.1.0"},
		{name: "same", version: "v1.1.0", rel: release{tag: "v1.1.0"}, latest: "v1.1.0"},
		{name: "older", version: "v1.2.0", rel: release{tag: "v1.1.0"}, latest: "v1.1.0"},
		{name: "running a newer pre-release", version: "v1.2.0-rc.1", rel: release{tag: "v1.1.0"}, latest: "v1.1.0"},
		{name: "running an older pre-release", version: "v1.1.0-rc.1", rel: release{tag: "v1.1.0"}, available: true, latest: "v1.1.0"},
		{name: "no releases", version: "v1.0.0", rel: release{status: http.StatusNotFound}},
		{name: "GitHub error", version: "v1.0.0", rel: release{status: http.StatusForbidden}, wantErr: true},
		{name: "tag not a version", version: "v1.0.0", rel: release{tag: "latest"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.set(tt.rel)
			u := f.updater(tt.version, nil)
			_, err := u.Check(t.Context())
			if (err != nil) != tt.wantErr {
				t.Fatalf("Check() error = %v, want error %v", err, tt.wantErr)
			}
			st := waitIdle(t, u)
			if st.Available != tt.available {
				t.Errorf("Available = %v, want %v", st.Available, tt.available)
			}
			if got := latestVersion(st); got != tt.latest {
				t.Errorf("Latest = %q, want %q", got, tt.latest)
			}
			if tt.wantErr == (st.Err == "") {
				t.Errorf("Err = %q, want error %v", st.Err, tt.wantErr)
			}
			if st.State != Idle || st.Staged != "" {
				t.Errorf("State = %q, Staged = %q; want idle with nothing staged", st.State, st.Staged)
			}
		})
	}
}

func latestVersion(st Status) string {
	if st.Latest == nil {
		return ""
	}
	return st.Latest.Version
}

func TestDevelopmentBuild(t *testing.T) {
	f := newFixture(t)
	f.set(release{tag: "v1.1.0"})
	u := f.updater("dev", nil)
	if _, err := u.Check(t.Context()); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Check() error = %v, want ErrUnsupported", err)
	}
	if _, err := u.Download(t.Context()); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Download() error = %v, want ErrUnsupported", err)
	}
	if _, err := u.Install(t.Context()); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Install() error = %v, want ErrUnsupported", err)
	}
	if st, _ := u.Status(t.Context()); st.Unsupported == "" {
		t.Error("Unsupported is empty")
	}
}

func TestDownloadAndInstall(t *testing.T) {
	f := newFixture(t)
	f.set(release{tag: "v1.1.0"})
	u := f.updater("v1.0.0", nil)
	if _, err := u.Install(t.Context()); !errors.Is(err, ErrNotStaged) {
		t.Fatalf("Install() before download: error = %v, want ErrNotStaged", err)
	}
	if _, err := u.Download(t.Context()); !errors.Is(err, ErrNoUpdate) {
		t.Fatalf("Download() before check: error = %v, want ErrNoUpdate", err)
	}
	if _, err := u.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	st, err := u.Download(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if st.State != Downloading {
		t.Fatalf("State = %q, want downloading", st.State)
	}
	if _, err := u.Check(t.Context()); !errors.Is(err, ErrBusy) {
		t.Errorf("Check() while downloading: error = %v, want ErrBusy", err)
	}
	st = waitIdle(t, u)
	if st.Staged != "v1.1.0" || st.Err != "" {
		t.Fatalf("after download: Staged = %q, Err = %q", st.Staged, st.Err)
	}

	st, err = u.Install(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if st.State != Restarting {
		t.Errorf("State = %q, want restarting", st.State)
	}
	if got := read(t, f.binary); !strings.Contains(got, "echo v1.1.0") {
		t.Errorf("binary = %q, want the new release", got)
	}
	if got := read(t, f.binary+".prev"); got != "old" {
		t.Errorf("previous binary = %q, want %q", got, "old")
	}
	if _, err := os.Stat(f.binary + ".new"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".new left behind: %v", err)
	}
	if _, err := u.Install(t.Context()); !errors.Is(err, ErrBusy) {
		t.Errorf("second Install() error = %v, want ErrBusy", err)
	}
}

func TestDownloadRejects(t *testing.T) {
	good := tarball(t, "#!/bin/sh\necho v1.1.0\n")
	tests := []struct {
		name string
		rel  func() release
		want string
	}{
		{
			name: "signature by another key",
			rel: func() release {
				_, other, _ := minisign.GenerateKey(rand.Reader)
				sum := sha256.Sum256(good)
				sums := fmt.Appendf(nil, "%x  %s\n", sum, archiveName("v1.1.0"))
				return release{tag: "v1.1.0", archive: good, sums: sums, sig: minisign.Sign(other, sums)}
			},
			want: "not signed",
		},
		{
			name: "tampered archive",
			rel: func() release {
				sum := sha256.Sum256(good)
				sums := fmt.Appendf(nil, "%x  %s\n", sum, archiveName("v1.1.0"))
				return release{tag: "v1.1.0", archive: tarball(t, "#!/bin/sh\necho evil\n"), sums: sums}
			},
			want: "does not match its checksum",
		},
		{
			name: "archive missing from checksums",
			rel: func() release {
				return release{tag: "v1.1.0", sums: []byte("00  other.tar.gz\n")}
			},
			want: "lists no checksum",
		},
		{
			name: "binary reports another version",
			rel: func() release {
				return release{tag: "v1.1.0", archive: tarball(t, "#!/bin/sh\necho v1.0.9\n")}
			},
			want: "reports version",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.set(tt.rel())
			u := f.updater("v1.0.0", nil)
			if _, err := u.Check(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := u.Download(t.Context()); err != nil {
				t.Fatal(err)
			}
			st := waitIdle(t, u)
			if st.Staged != "" || !strings.Contains(st.Err, tt.want) {
				t.Errorf("Staged = %q, Err = %q; want nothing staged and an error containing %q", st.Staged, st.Err, tt.want)
			}
			if got := read(t, f.binary); got != "old" {
				t.Errorf("binary changed to %q", got)
			}
		})
	}
}

func TestAutoDownload(t *testing.T) {
	f := newFixture(t)
	f.set(release{tag: "v1.1.0"})
	settings := &memSettings{}
	u := f.updater("v1.0.0", settings)
	if _, err := u.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if st := waitIdle(t, u); st.Staged != "" || st.AutoDownload {
		t.Fatalf("auto-download off: Staged = %q, AutoDownload = %v", st.Staged, st.AutoDownload)
	}

	st, err := u.SetAutoDownload(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !st.AutoDownload || st.State != Downloading {
		t.Fatalf("after enabling: AutoDownload = %v, State = %q", st.AutoDownload, st.State)
	}
	if st := waitIdle(t, u); st.Staged != "v1.1.0" {
		t.Fatalf("Staged = %q, want v1.1.0", st.Staged)
	}

	// A newer release found by a check downloads on its own.
	f.set(release{tag: "v1.2.0"})
	if _, err := u.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if st := waitIdle(t, u); st.Staged != "v1.2.0" {
		t.Errorf("Staged = %q, want v1.2.0", st.Staged)
	}
}

func TestRunDownloads(t *testing.T) {
	f := newFixture(t)
	f.set(release{tag: "v1.1.0"})
	u := f.updater("v1.0.0", nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		u.Run(ctx)
	}()
	// Run waits a minute before its first check; Download still runs now.
	if _, err := u.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Download(t.Context()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		st, err := u.Status(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if st.Staged == "v1.1.0" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("download did not finish: %+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
}

func TestChecksum(t *testing.T) {
	sum := sha256.Sum256([]byte("x"))
	tests := []struct {
		name, sums string
		wantErr    bool
	}{
		{"text mode", fmt.Sprintf("%x  a.tar.gz\n", sum), false},
		{"binary mode", fmt.Sprintf("%x  *a.tar.gz\n", sum), false},
		{"other file", fmt.Sprintf("%x  b.tar.gz\n", sum), true},
		{"name prefix", fmt.Sprintf("%x  a.tar.gz.sig\n", sum), true},
		{"short hash", "abcd  a.tar.gz\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := checksum([]byte(tt.sums), "a.tar.gz")
			if (err != nil) != tt.wantErr {
				t.Fatalf("checksum() error = %v, want error %v", err, tt.wantErr)
			}
			if err == nil && !bytes.Equal(got, sum[:]) {
				t.Errorf("checksum() = %x, want %x", got, sum)
			}
		})
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
