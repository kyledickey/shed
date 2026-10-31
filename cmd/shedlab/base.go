package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	cloudImageDir  = "https://cloud-images.ubuntu.com/releases/noble/release/"
	cloudImageName = "ubuntu-24.04-server-cloudimg-amd64.img"
)

func basePath() string { return filepath.Join(dataDir(), "images", "base.qcow2") }

// bakeScript runs after the real installer: it turns the installed VM back
// into a machine that has never started shed, while keeping its pulled images.
const bakeScript = `set -e
sudo apt-get install -y -qq sqlite3 >/dev/null
sudo systemctl disable --now shed
sudo docker ps -aq | xargs -r sudo docker rm -f >/dev/null
sudo docker network prune -f >/dev/null
sudo docker volume prune -af >/dev/null
sudo rm -rf /var/lib/shed /etc/shed/shed.toml /etc/shed/shed.key /etc/shed/shed.log
sudo cloud-init clean --logs --machine-id
`

// bakeBase builds the image every lab clones: an Ubuntu cloud image with
// shed's real installer run on it. It writes to a temporary directory and
// renames at the end, so an interrupted bake leaves nothing that looks done.
func bakeBase(ctx context.Context, p *progress, root string) error {
	images := filepath.Dir(basePath())
	if err := disableCoW(images); err != nil {
		return err
	}
	cloud := filepath.Join(images, cloudImageName)
	if err := downloadVerified(ctx, p, cloud); err != nil {
		return err
	}
	publicKey, err := ensureSSHKey(ctx)
	if err != nil {
		return err
	}

	dir := filepath.Join(images, "bake")
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	v := vm{name: "bake", dir: dir, cpus: 4, memoryGB: 4, sshPort: 18022}
	if err := v.clone(ctx, cloud, 20); err != nil {
		return err
	}
	if err := v.writeSeed(publicKey); err != nil {
		return err
	}
	defer v.kill(context.WithoutCancel(ctx))

	p.set("booting Ubuntu")
	if err := v.start(ctx); err != nil {
		return err
	}
	if err := v.waitSSH(ctx); err != nil {
		return err
	}

	p.set("running deploy/install.sh")
	if err := v.upload(ctx, filepath.Join(root, "deploy", "install.sh"), "/tmp/install.sh"); err != nil {
		return err
	}
	if _, err := v.ssh(ctx, "sudo SHED_YES=1 SHED_DOMAIN=lab."+publicDomain+" SHED_ALLOWED_USERS=lab bash /tmp/install.sh"); err != nil {
		return err
	}
	p.set("cleaning up")
	if err := v.sshInput(ctx, bakeScript, "bash -s"); err != nil {
		return err
	}
	if err := v.stop(ctx); err != nil {
		return err
	}
	p.set("compacting")
	tmp := basePath() + ".tmp"
	if _, err := run(ctx, "qemu-img", "convert", "-O", "qcow2", v.disk(), tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, basePath()); err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// downloadVerified fetches the Ubuntu cloud image to path unless a copy with
// the published checksum is already there.
func downloadVerified(ctx context.Context, p *progress, path string) error {
	want, err := publishedChecksum(ctx)
	if err != nil {
		return err
	}
	if have, err := fileChecksum(path); err == nil && have == want {
		return nil
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cloudImageDir+cloudImageName, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download cloud image: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("download cloud image: %s", res.Status)
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	defer f.Close()

	sum := sha256.New()
	counter := &progressWriter{p: p, total: res.ContentLength}
	if _, err := io.Copy(io.MultiWriter(f, sum, counter), res.Body); err != nil {
		return fmt.Errorf("download cloud image: %w", err)
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != want {
		return fmt.Errorf("cloud image checksum mismatch: got %s, want %s", got, want)
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func publishedChecksum(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cloudImageDir+"SHA256SUMS", nil)
	if err != nil {
		return "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch image checksums: %w", err)
	}
	defer res.Body.Close()
	scanner := bufio.NewScanner(res.Body)
	for scanner.Scan() {
		sum, name, ok := strings.Cut(scanner.Text(), " ")
		if ok && strings.TrimLeft(name, " *") == cloudImageName {
			return sum, nil
		}
	}
	return "", fmt.Errorf("no checksum published for %s", cloudImageName)
}

func fileChecksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

type progressWriter struct {
	p     *progress
	total int64
	seen  int64
	last  int64
}

func (w *progressWriter) Write(b []byte) (int, error) {
	w.seen += int64(len(b))
	if w.total > 0 && w.seen*100/w.total != w.last {
		w.last = w.seen * 100 / w.total
		w.p.set(fmt.Sprintf("downloading Ubuntu %d%%", w.last))
	}
	return len(b), nil
}
