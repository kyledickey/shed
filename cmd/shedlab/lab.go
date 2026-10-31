package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// slotCount is how many labs can exist at once. Each slot has fixed ports and
// hostnames, which is what lets one GitHub App's callback list cover them all.
const slotCount = 5

const (
	publicDomain = "shed.land"
	sshUser      = "ubuntu"
	guestShedDB  = "/var/lib/shed/shed.db"
)

// slot is a lab's identity, from 1 to slotCount.
type slot int

func (s slot) name() string      { return "lab" + strconv.Itoa(int(s)) }
func (s slot) port(base int) int { return 18000 + int(s)*100 + base }
func (s slot) dashPort() int     { return s.port(0) }
func (s slot) sshPort() int      { return s.port(22) }
func (s slot) httpsPort() int    { return s.port(43) }
func (s slot) httpPort() int     { return s.port(80) }
func (s slot) localHost() string { return s.name() + ".localhost" }
func (s slot) localURL() string  { return fmt.Sprintf("http://%s:%d", s.localHost(), s.dashPort()) }
func (s slot) publicURL() string { return "https://" + s.name() + "." + publicDomain }

// parseSlot accepts "lab3" or "3".
func parseSlot(name string) (slot, bool) {
	n, err := strconv.Atoi(strings.TrimPrefix(name, "lab"))
	return slot(n), err == nil && n >= 1 && n <= slotCount
}

// lab is what shedlab remembers about a lab. Whether it is running is never
// stored: it comes from the VM process, so a crash can't leave it out of date.
type lab struct {
	Slot     slot      `json:"slot"`
	Scenario string    `json:"scenario"`
	Source   string    `json:"source"` // "dev", a release tag, or "snapshot <name>".
	Auth     authMode  `json:"auth"`
	Public   bool      `json:"public"`
	URL      string    `json:"url"`
	Session  string    `json:"session"`
	Login    string    `json:"login"`
	CPUs     int       `json:"cpus"`
	MemoryGB int       `json:"memoryGb"`
	Created  time.Time `json:"created"`
}

// authMode says how a lab signs in.
type authMode string

const (
	authNone   authMode = "none"
	authGitHub authMode = "github"
)

func dataDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "shedlab")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "shedlab")
}

func labDir(s slot) string { return filepath.Join(dataDir(), "labs", s.name()) }

func (l lab) vm() vm {
	s := l.Slot
	return vm{
		name: s.name(), dir: labDir(s), cpus: l.CPUs, memoryGB: l.MemoryGB, sshPort: s.sshPort(),
		// Guest and host ports match so Caddy's HTTPS redirects land on the
		// forwarded port.
		forward: []int{s.dashPort(), s.httpPort(), s.httpsPort()},
	}
}

func (l lab) save() error {
	raw, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(labDir(l.Slot), "lab.json"), raw, 0o600)
}

func loadLab(s slot) (lab, error) {
	var l lab
	raw, err := os.ReadFile(filepath.Join(labDir(s), "lab.json"))
	if err != nil {
		return l, err
	}
	return l, json.Unmarshal(raw, &l)
}

// loadLabs returns the labs that exist, in slot order.
func loadLabs() ([]lab, error) {
	var labs []lab
	for s := slot(1); s <= slotCount; s++ {
		l, err := loadLab(s)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", s.name(), err)
		}
		labs = append(labs, l)
	}
	return labs, nil
}

// freeSlot claims the lowest slot without a directory. Mkdir makes the claim
// atomic, so two concurrent `up`s get different slots.
func freeSlot() (slot, error) {
	if err := os.MkdirAll(filepath.Join(dataDir(), "labs"), 0o755); err != nil {
		return 0, err
	}
	for s := slot(1); s <= slotCount; s++ {
		err := os.Mkdir(labDir(s), 0o755)
		if err == nil {
			return s, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return 0, err
		}
	}
	return 0, fmt.Errorf("all %d labs are in use; remove one with `shedlab down`", slotCount)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// vm is one QEMU guest.
type vm struct {
	name     string
	dir      string
	cpus     int
	memoryGB int
	sshPort  int
	forward  []int
}

func (v vm) pidFile() string { return filepath.Join(v.dir, "qemu.pid") }
func (v vm) disk() string    { return filepath.Join(v.dir, "disk.qcow2") }

// pid returns the VM's process ID, or 0 if it isn't running. The command line
// is checked because a stale pid file can name an unrelated process.
func (v vm) pid() int {
	raw, err := os.ReadFile(v.pidFile())
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || !bytes.Contains(cmdline, []byte("shedlab-"+v.name+"\x00")) {
		return 0
	}
	return pid
}

func (v vm) running() bool { return v.pid() != 0 }

// clone creates the VM's disk as a copy-on-write layer over backing.
func (v vm) clone(ctx context.Context, backing string, diskGB int) error {
	if err := disableCoW(v.dir); err != nil {
		return err
	}
	_, err := run(ctx, "qemu-img", "create", "-q", "-f", "qcow2", "-F", "qcow2", "-b", backing,
		v.disk(), strconv.Itoa(diskGB)+"G")
	return err
}

// disableCoW turns off btrfs copy-on-write for new files in dir, because qcow2
// is already copy-on-write. It fails harmlessly on other filesystems.
func disableCoW(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	_ = exec.Command("chattr", "+C", dir).Run()
	return nil
}

// writeSeed writes the cloud-init NoCloud files QEMU presents as a FAT disk.
func (v vm) writeSeed(publicKey string) error {
	seed := filepath.Join(v.dir, "seed")
	if err := os.MkdirAll(seed, 0o755); err != nil {
		return err
	}
	meta := fmt.Sprintf("instance-id: %s-%d\nlocal-hostname: %s\n", v.name, time.Now().Unix(), v.name)
	user := fmt.Sprintf("#cloud-config\nssh_authorized_keys:\n  - %s\n", strings.TrimSpace(publicKey))
	for name, content := range map[string]string{"meta-data": meta, "user-data": user} {
		if err := os.WriteFile(filepath.Join(seed, name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (v vm) qemuArgs() []string {
	netdev := fmt.Sprintf("user,id=net0,hostfwd=tcp:127.0.0.1:%d-:22", v.sshPort)
	for _, p := range v.forward {
		netdev += fmt.Sprintf(",hostfwd=tcp:127.0.0.1:%d-:%d", p, p)
	}
	return []string{
		"-name", "shedlab-" + v.name,
		"-machine", "q35,accel=kvm", "-cpu", "host",
		"-smp", strconv.Itoa(v.cpus), "-m", strconv.Itoa(v.memoryGB) + "G",
		// Labs are disposable, so trade crash safety for write speed.
		"-drive", "file=" + v.disk() + ",if=virtio,format=qcow2,cache=unsafe,discard=unmap",
		"-drive", "if=virtio,format=raw,read-only=on,file.driver=vvfat,file.dir=" + filepath.Join(v.dir, "seed") + ",file.label=cidata",
		"-netdev", netdev, "-device", "virtio-net-pci,netdev=net0",
		"-device", "virtio-rng-pci",
		"-display", "none", "-serial", "file:" + filepath.Join(v.dir, "console.log"),
		"-daemonize", "-pidfile", v.pidFile(),
	}
}

func (v vm) start(ctx context.Context) error {
	if v.running() {
		return nil
	}
	_, err := run(ctx, "qemu-system-x86_64", v.qemuArgs()...)
	return err
}

// waitSSH blocks until the guest accepts commands and cloud-init has finished.
func (v vm) waitSSH(ctx context.Context) error {
	return poll(ctx, 5*time.Minute, "SSH on "+v.name, func() error {
		if !v.running() {
			return permanent(fmt.Errorf("VM exited during boot; see %s", filepath.Join(v.dir, "console.log")))
		}
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", v.sshPort), time.Second)
		if err != nil {
			return err
		}
		c.Close()
		_, err = v.ssh(ctx, "cloud-init status --wait >/dev/null; true")
		return err
	})
}

// stop shuts the guest down cleanly and falls back to killing QEMU.
func (v vm) stop(ctx context.Context) error {
	pid := v.pid()
	if pid == 0 {
		return nil
	}
	_, _ = v.ssh(ctx, "sudo systemctl poweroff")
	err := poll(ctx, time.Minute, v.name+" to power off", func() error {
		if v.running() {
			return errors.New("still running")
		}
		return nil
	})
	if err == nil {
		return nil
	}
	return v.kill(ctx)
}

func (v vm) kill(ctx context.Context) error {
	pid := v.pid()
	if pid == 0 {
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("stop %s: %w", v.name, err)
	}
	return poll(ctx, 15*time.Second, v.name+" to exit", func() error {
		if v.running() {
			return errors.New("still running")
		}
		return nil
	})
}

func sshKey() string { return filepath.Join(dataDir(), "id_ed25519") }

// ensureSSHKey creates the key shared by every VM and returns its public half.
func ensureSSHKey(ctx context.Context) (string, error) {
	if err := os.MkdirAll(dataDir(), 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(sshKey()); errors.Is(err, fs.ErrNotExist) {
		if _, err := run(ctx, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "shedlab", "-f", sshKey()); err != nil {
			return "", err
		}
	}
	raw, err := os.ReadFile(sshKey() + ".pub")
	return string(raw), err
}

func sshOptions() []string {
	return []string{
		"-i", sshKey(), "-o", "IdentitiesOnly=yes", "-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null", "-o", "LogLevel=ERROR", "-o", "ConnectTimeout=5",
	}
}

// sshArgs returns ssh's arguments for running command in the guest, or a login
// shell if command is empty.
func (v vm) sshArgs(command string) []string {
	args := append(sshOptions(), "-p", strconv.Itoa(v.sshPort), sshUser+"@127.0.0.1")
	if command != "" {
		args = append(args, command)
	}
	return args
}

func (v vm) ssh(ctx context.Context, command string) (string, error) {
	return run(ctx, "ssh", v.sshArgs(command)...)
}

// sshInput runs command in the guest with stdin as its input.
func (v vm) sshInput(ctx context.Context, stdin, command string) error {
	cmd := exec.CommandContext(ctx, "ssh", v.sshArgs(command)...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w\n%s", command, err, tail(out))
	}
	return nil
}

// upload copies a local file into the guest.
func (v vm) upload(ctx context.Context, local, remote string) error {
	args := append(sshOptions(), "-P", strconv.Itoa(v.sshPort), local, sshUser+"@127.0.0.1:"+remote)
	_, err := run(ctx, "scp", args...)
	return err
}

func run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w\n%s", name, err, tail(out))
	}
	return string(out), nil
}

// tail keeps the end of a command's output, which is where the error is.
func tail(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 25 {
		lines = lines[len(lines)-25:]
	}
	return strings.Join(lines, "\n")
}

type permanentError struct{ error }

func permanent(err error) error { return permanentError{err} }

// poll calls check until it succeeds, returns a permanent error, or timeout
// passes.
func poll(ctx context.Context, timeout time.Duration, what string, check func() error) error {
	deadline := time.Now().Add(timeout)
	for {
		err := check()
		if err == nil {
			return nil
		}
		var p permanentError
		if errors.As(err, &p) {
			return p.error
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s: %w", what, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
