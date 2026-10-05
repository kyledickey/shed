package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"aead.dev/minisign"
)

const (
	checksumsAsset = "checksums.txt"
	signatureAsset = checksumsAsset + ".minisig"
	// maxSmall bounds checksums.txt and its signature.
	maxSmall = 1 << 20
	// maxArchive bounds a release archive.
	maxArchive = 512 << 20
	// maxBinary bounds the extracted shed binary.
	maxBinary = 1 << 30
)

// archiveName returns the name of the release archive for this platform.
func archiveName(version string) string {
	return fmt.Sprintf("shed_%s_linux_%s.tar.gz", strings.TrimPrefix(version, "v"), runtime.GOARCH)
}

// fetch downloads rel, verifies it, and returns the path of its shed binary
// in the download directory.
func (u *Updater) fetch(ctx context.Context, rel *Release) (string, error) {
	archive := archiveName(rel.Version)
	urls := make(map[string]string, 3)
	for _, name := range []string{checksumsAsset, signatureAsset, archive} {
		url, ok := rel.assets[name]
		if !ok {
			return "", fmt.Errorf("update: release %s has no %s", rel.Version, name)
		}
		urls[name] = url
	}

	sums, err := u.get(ctx, urls[checksumsAsset], maxSmall)
	if err != nil {
		return "", err
	}
	sig, err := u.get(ctx, urls[signatureAsset], maxSmall)
	if err != nil {
		return "", err
	}
	if !minisign.Verify(u.key, sums, sig) {
		return "", fmt.Errorf("update: %s of %s is not signed by the shed release key", checksumsAsset, rel.Version)
	}
	want, err := checksum(sums, archive)
	if err != nil {
		return "", err
	}

	if err := os.RemoveAll(u.cfg.Dir); err != nil {
		return "", fmt.Errorf("update: clear downloads: %w", err)
	}
	if err := os.MkdirAll(u.cfg.Dir, 0o700); err != nil {
		return "", fmt.Errorf("update: create download directory: %w", err)
	}
	archivePath := filepath.Join(u.cfg.Dir, archive)
	defer os.Remove(archivePath)
	got, err := u.save(ctx, urls[archive], archivePath)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(got, want) {
		return "", fmt.Errorf("update: %s does not match its checksum", archive)
	}

	bin := filepath.Join(u.cfg.Dir, "shed-"+rel.Version)
	if err := extract(archivePath, bin); err != nil {
		return "", err
	}
	if err := checkVersion(ctx, bin, rel.Version); err != nil {
		os.Remove(bin)
		return "", err
	}
	return bin, nil
}

// get returns the body of url, which must be at most limit bytes.
func (u *Updater) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	body, err := u.open(ctx, url)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	b, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("update: download %s: %w", url, err)
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("update: download %s: larger than %d bytes", url, limit)
	}
	return b, nil
}

// save writes the body of url to path and returns its SHA-256.
func (u *Updater) save(ctx context.Context, url, path string) ([]byte, error) {
	body, err := u.open(ctx, url)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(body, maxArchive+1))
	if err != nil {
		return nil, fmt.Errorf("update: download %s: %w", url, err)
	}
	if n > maxArchive {
		return nil, fmt.Errorf("update: download %s: larger than %d bytes", url, maxArchive)
	}
	return h.Sum(nil), f.Close()
}

func (u *Updater) open(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	req.Header.Set("User-Agent", "shed/"+u.cfg.Version)
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("update: download %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("update: download %s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

// checksum returns the SHA-256 that sums, in sha256sum's format, lists for
// name.
func checksum(sums []byte, name string) ([]byte, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		hash, file, ok := strings.Cut(sc.Text(), "  ")
		if !ok || strings.TrimPrefix(file, "*") != name {
			continue
		}
		sum, err := hex.DecodeString(hash)
		if err != nil || len(sum) != sha256.Size {
			return nil, fmt.Errorf("update: invalid checksum for %s", name)
		}
		return sum, nil
	}
	return nil, fmt.Errorf("update: %s lists no checksum for %s", checksumsAsset, name)
}

// extract writes the shed binary at the root of the gzipped tar archive to
// dst.
func extract(archive, dst string) error {
	f, err := os.Open(archive)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("update: read %s: %w", filepath.Base(archive), err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("update: %s has no shed binary", filepath.Base(archive))
		}
		if err != nil {
			return fmt.Errorf("update: read %s: %w", filepath.Base(archive), err)
		}
		if hdr.Typeflag != tar.TypeReg || strings.TrimPrefix(hdr.Name, "./") != "shed" {
			continue
		}
		tmp := dst + ".tmp"
		if err := writeFile(tmp, io.LimitReader(tr, maxBinary), 0o700); err != nil {
			os.Remove(tmp)
			return err
		}
		if err := os.Rename(tmp, dst); err != nil {
			os.Remove(tmp)
			return fmt.Errorf("update: %w", err)
		}
		return nil
	}
}

// checkVersion runs bin -version and checks that it prints version.
func checkVersion(ctx context.Context, bin, version string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-version").Output()
	if err != nil {
		return fmt.Errorf("update: run downloaded shed: %w", err)
	}
	if got := strings.TrimSpace(string(out)); got != version {
		return fmt.Errorf("update: downloaded shed reports version %q, not %s", got, version)
	}
	return nil
}

// replace installs the binary at src as dst, keeping dst as dst.prev. The
// new binary is copied next to dst first so the final rename is atomic.
func replace(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	defer in.Close()
	next := dst + ".new"
	if err := writeFile(next, in, 0o755); err != nil {
		os.Remove(next)
		return err
	}
	prev := dst + ".prev"
	if err := os.Remove(prev); err != nil && !errors.Is(err, os.ErrNotExist) {
		os.Remove(next)
		return fmt.Errorf("update: %w", err)
	}
	if err := os.Link(dst, prev); err != nil {
		os.Remove(next)
		return fmt.Errorf("update: keep previous binary: %w", err)
	}
	if err := os.Rename(next, dst); err != nil {
		os.Remove(next)
		return fmt.Errorf("update: %w", err)
	}
	return syncDir(filepath.Dir(dst))
}

// writeFile writes r to a new file at path and syncs it.
func writeFile(path string, r io.Reader, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("update: write %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("update: sync %s: %w", path, err)
	}
	return f.Close()
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("update: sync %s: %w", dir, err)
	}
	return nil
}
