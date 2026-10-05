//go:build integration

package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

const testImage = "alpine:3"

// countWriter counts the bytes written to it.
type countWriter struct{ n int64 }

func (w *countWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}

func newTestClient(t *testing.T) *Client {
	t.Helper()
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ctx := context.Background()
	if err := c.Ping(ctx); err != nil {
		t.Skipf("no Docker daemon: %v", err)
	}
	if err := c.PullImage(ctx, testImage, io.Discard); err != nil {
		t.Fatal(err)
	}
	return c
}

func suffix(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", b)
}

// create creates a container and registers its removal.
func create(t *testing.T, c *Client, spec RunSpec) string {
	t.Helper()
	id, err := c.Create(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Remove(context.Background(), id) })
	return id
}

func TestCopyRoundTrip(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	sfx := suffix(t)

	vol := "shed-test-vol-" + sfx
	if err := c.EnsureVolume(ctx, vol); err != nil {
		t.Fatal(err)
	}
	// Registered before the container cleanup so that it runs after it.
	t.Cleanup(func() {
		if err := c.RemoveVolume(ctx, vol); err != nil {
			t.Errorf("remove volume: %v", err)
		}
	})

	id := create(t, c, RunSpec{
		Name:   "shed-test-helper-" + sfx,
		Image:  testImage,
		Mounts: []Mount{{Volume: vol, Target: "/data"}},
	})

	// A volume in use by a container cannot be removed.
	if err := c.RemoveVolume(ctx, vol); !IsConflict(err) {
		t.Errorf("RemoveVolume of a used volume = %v, want a conflict", err)
	}

	const content = "secret\n"
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "file.txt", Mode: 0o600, Uid: 999, Gid: 998, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.CopyTo(ctx, id, "/data", &buf); err != nil {
		t.Fatal(err)
	}

	rc, err := c.CopyFrom(ctx, id, "/data/.")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	tr := tar.NewReader(rc)
	found := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimPrefix(h.Name, "./") != "file.txt" {
			continue
		}
		found = true
		got, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != content {
			t.Errorf("content = %q, want %q", got, content)
		}
		if h.Uid != 999 || h.Gid != 998 || h.FileInfo().Mode().Perm() != 0o600 {
			t.Errorf("uid/gid/mode = %d/%d/%o, want 999/998/600", h.Uid, h.Gid, h.FileInfo().Mode().Perm())
		}
	}
	if !found {
		t.Error("file.txt not in archive")
	}
}

func TestExec(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	id, err := c.Run(ctx, RunSpec{Name: "shed-test-exec-" + suffix(t), Image: testImage, Cmd: []string{"sleep", "300"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Remove(context.Background(), id) })

	t.Run("large stdin", func(t *testing.T) {
		const size = 50 << 20
		var out, errOut countWriter
		err := c.Exec(ctx, id, []string{"cat"}, io.LimitReader(rand.Reader, size), &out, &errOut)
		if err != nil {
			t.Fatal(err)
		}
		if out.n != size || errOut.n != 0 {
			t.Errorf("stdout/stderr bytes = %d/%d, want %d/0", out.n, errOut.n, size)
		}
	})

	t.Run("exit status and stderr", func(t *testing.T) {
		var out, errOut bytes.Buffer
		err := c.Exec(ctx, id, []string{"sh", "-c", "echo out; echo err >&2; exit 3"}, nil, &out, &errOut)
		var ee *ExitError
		if !errors.As(err, &ee) || ee.Code != 3 {
			t.Fatalf("err = %v, want ExitError{3}", err)
		}
		if out.String() != "out\n" || errOut.String() != "err\n" {
			t.Errorf("stdout/stderr = %q/%q, want %q/%q", out.String(), errOut.String(), "out\n", "err\n")
		}
	})

	t.Run("early exit without reading stdin", func(t *testing.T) {
		err := c.Exec(ctx, id, []string{"true"}, io.LimitReader(rand.Reader, 10<<20), io.Discard, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("stdin read error", func(t *testing.T) {
		boom := errors.New("boom")
		r := io.MultiReader(strings.NewReader("partial"), failingReader{boom})
		err := c.Exec(ctx, id, []string{"cat"}, r, io.Discard, io.Discard)
		if !errors.Is(err, boom) {
			t.Errorf("err = %v, want %v", err, boom)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		cctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		start := time.Now()
		err := c.Exec(cctx, id, []string{"sleep", "60"}, nil, io.Discard, io.Discard)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("err = %v, want deadline exceeded", err)
		}
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("Exec took %v after cancel", d)
		}
	})
}

// volumeExists reports whether the named volume exists.
func volumeExists(t *testing.T, c *Client, name string) bool {
	t.Helper()
	_, err := c.api.VolumeInspect(context.Background(), name, client.VolumeInspectOptions{})
	if err != nil && !IsNotFound(err) {
		t.Fatal(err)
	}
	return err == nil
}

func TestRemoveDeletesAnonymousVolumes(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	// mongo declares VOLUME /data/db and /data/configdb. The named volume
	// takes the place of the first, as in a deployed database.
	const image = "mongo:8"
	if err := c.PullImage(ctx, image, io.Discard); err != nil {
		t.Fatal(err)
	}
	named := "shed-test-named-" + suffix(t)
	if err := c.EnsureVolume(ctx, named); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.RemoveVolume(context.Background(), named) })

	id := create(t, c, RunSpec{
		Name:   "shed-test-volumes-" + suffix(t),
		Image:  image,
		Mounts: []Mount{{Volume: named, Target: "/data/db"}},
	})
	info, err := c.Inspect(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	var anonymous string
	for _, v := range info.Volumes {
		if v != named {
			anonymous = v
		}
	}
	if anonymous == "" || len(info.Volumes) != 2 {
		t.Fatalf("container volumes = %v, want %s and an anonymous one", info.Volumes, named)
	}
	t.Cleanup(func() { _ = c.RemoveVolume(context.Background(), anonymous) })
	if !volumeExists(t, c, anonymous) {
		t.Fatalf("anonymous volume %s was not created", anonymous)
	}

	if err := c.Remove(ctx, id); err != nil {
		t.Fatal(err)
	}
	if volumeExists(t, c, anonymous) {
		t.Errorf("anonymous volume %s survived Remove", anonymous)
	}
	if !volumeExists(t, c, named) {
		t.Errorf("named volume %s was removed by Remove", named)
	}
}

func TestNetworkAliases(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	sfx := suffix(t)
	net, alias := "shed-test-net-"+sfx, "shed-test-alias-"+sfx
	if err := c.EnsureNetwork(ctx, net); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.RemoveNetwork(context.Background(), net) })
	run := func(name string) string {
		t.Helper()
		id, err := c.Run(ctx, RunSpec{Name: name + "-" + sfx, Image: testImage, Cmd: []string{"sleep", "300"}, Network: net})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Remove(context.Background(), id) })
		return id
	}
	target, peer := run("shed-test-target"), run("shed-test-client")
	lookup := func() (string, bool) {
		var out bytes.Buffer
		err := c.Exec(ctx, peer, []string{"getent", "hosts", alias}, nil, &out, io.Discard)
		ip, _, _ := strings.Cut(strings.TrimSpace(out.String()), " ")
		return ip, err == nil
	}
	resolves := func() bool {
		_, ok := lookup()
		return ok
	}
	ipOf := func(id string) string {
		t.Helper()
		ctr, err := c.Inspect(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return ctr.IPs[net]
	}

	if resolves() {
		t.Fatal("alias resolves before it was added")
	}
	before := ipOf(target)
	if err := c.ReconnectNetwork(ctx, net, target, []string{alias}); err != nil {
		t.Fatal(err)
	}
	if after := ipOf(target); after != before {
		t.Errorf("address changed from %s to %s on reconnect", before, after)
	}
	// The address must be requested, not just happen to be free again.
	res, err := c.api.ContainerInspect(ctx, target, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ep := res.Container.NetworkSettings.Networks[net]; ep == nil || ep.IPAMConfig == nil || ep.IPAMConfig.IPv4Address.String() != before {
		t.Errorf("endpoint does not request address %s: %+v", before, ep)
	}
	if ip, ok := lookup(); !ok || ip != before {
		t.Errorf("alias resolves to %q (%v), want %s", ip, ok, before)
	}
	if err := c.DisconnectNetwork(ctx, net, target); err != nil {
		t.Fatal(err)
	}
	if resolves() {
		t.Error("alias still resolves after disconnecting")
	}
}

func TestRunRemovesContainerThatFailsToStart(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	name := "shed-test-nostart-" + suffix(t)
	id, err := c.Run(ctx, RunSpec{Name: name, Image: testImage, Cmd: []string{"/no/such/binary"}})
	if err == nil {
		_ = c.Remove(ctx, id)
		t.Fatal("Run succeeded with a missing command")
	}
	if id != "" {
		t.Errorf("Run returned ID %q although the container was removed", id)
	}
	if _, err := c.Inspect(ctx, name); !IsNotFound(err) {
		t.Errorf("inspect after failed start: %v, want not found", err)
	}
}

func TestCreateIsolated(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()
	sfx := suffix(t)
	net := "shed-test-net-" + sfx
	if err := c.EnsureNetwork(ctx, net); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.RemoveNetwork(context.Background(), net) })
	vol := "shed-test-vol-" + sfx
	if err := c.EnsureVolume(ctx, vol); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.RemoveVolume(context.Background(), vol) })

	orig := create(t, c, RunSpec{
		Name: "shed-test-orig-" + sfx, Image: testImage, Cmd: []string{"sleep", "infinity"},
		Env: []string{"SECRET=s3cret"}, Labels: map[string]string{"shed.test": sfx},
		Network: net, Aliases: []string{"db-" + sfx}, Mounts: []Mount{{Volume: vol, Target: "/data"}},
		Publish: []PortBinding{{HostPort: 0, ContainerPort: 5432}},
	})
	iso, err := c.CreateIsolated(ctx, orig, "shed-test-iso-"+sfx, map[string]string{"shed.test.iso": sfx})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Remove(context.Background(), iso) })
	if err := c.Start(ctx, iso); err != nil {
		t.Fatal(err)
	}

	res, err := c.api.ContainerInspect(ctx, iso, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	info := res.Container
	if info.HostConfig.RestartPolicy.Name != "no" {
		t.Errorf("restart policy %q, want no", info.HostConfig.RestartPolicy.Name)
	}
	if _, ok := info.Config.Labels["shed.test"]; ok {
		t.Errorf("labels %v include the original's", info.Config.Labels)
	}
	var nets []string
	for name := range info.NetworkSettings.Networks {
		nets = append(nets, name)
	}
	if len(nets) != 1 || nets[0] != "none" {
		t.Errorf("networks %v, want only none", nets)
	}
	for port, bindings := range info.NetworkSettings.Ports {
		if len(bindings) > 0 {
			t.Errorf("port %v published as %v", port, bindings)
		}
	}
	// The same environment and volume, and no route out of the container.
	var out, errOut bytes.Buffer
	script := `echo "$SECRET"; touch /data/written; sed 1d /proc/net/route | wc -l`
	if err := c.Exec(ctx, iso, []string{"sh", "-c", script}, nil, &out, &errOut); err != nil {
		t.Fatalf("%v: %s", err, errOut.String())
	}
	if got := strings.Fields(out.String()); len(got) != 2 || got[0] != "s3cret" || got[1] != "0" {
		t.Errorf("isolated container printed %q, want the secret and no routes", out.String())
	}
	rc, err := c.CopyFrom(ctx, orig, "/data/written")
	if err != nil {
		t.Fatalf("file written by the copy is not in the original's volume: %v", err)
	}
	rc.Close()
}
