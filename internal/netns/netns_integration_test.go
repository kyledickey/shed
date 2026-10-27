//go:build integration

// This test needs root and util-linux (unshare, nsenter):
//
//	sudo go test -tags integration ./internal/netns/
package netns_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/kyledickey/shed/internal/netns"
)

// namespace starts a process in a new network namespace whose loopback is
// up, with IPv6 disabled if ipv6 is false, and returns its PID.
func namespace(t *testing.T, ipv6 bool) int {
	t.Helper()
	script := "ip link set lo up"
	if !ipv6 {
		script += " && sysctl -qw net.ipv6.conf.lo.disable_ipv6=1"
	}
	cmd := exec.Command("unshare", "--net", "sh", "-c", script+" && echo ready && exec sleep 600")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	buf := make([]byte, 6)
	if _, err := out.Read(buf); err != nil || string(buf) != "ready\n" {
		t.Fatalf("namespace not ready: %q, %v", buf, err)
	}
	return cmd.Process.Pid
}

// dialIn reports whether a TCP connection to addr succeeds from inside the
// network namespace of pid.
func dialIn(pid int, addr string) error {
	host, port, _ := net.SplitHostPort(addr)
	out, err := exec.Command("nsenter", "-t", strconv.Itoa(pid), "-n", "bash", "-c",
		fmt.Sprintf("exec 3<>/dev/tcp/%s/%s", host, port)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

func TestListen(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root: entering a network namespace needs CAP_SYS_ADMIN; run with sudo")
	}
	ctx := context.Background()
	own, err := netns.ID(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	pid := namespace(t, true)
	if id, err := netns.ID(pid); err != nil || id == own {
		t.Fatalf("ID of the new namespace = %d, %v; own is %d", id, err, own)
	}

	for _, addr := range []string{"127.0.0.1:4318", "[::1]:4318"} {
		ln, err := netns.Listen(ctx, pid, addr)
		if err != nil {
			t.Fatal(err)
		}
		accepted := make(chan error, 1)
		go func() {
			c, err := ln.Accept()
			if err == nil {
				c.Close()
			}
			accepted <- err
		}()
		if err := dialIn(pid, addr); err != nil {
			t.Errorf("dial %s inside the namespace: %v", addr, err)
		}
		select {
		case err := <-accepted:
			if err != nil {
				t.Errorf("accept on %s: %v", addr, err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("accept on %s: nothing arrived", addr)
		}
		if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			c.Close()
			t.Errorf("%s answers on the host too", addr)
		}
		defer ln.Close()
	}

	if _, err := netns.Listen(ctx, pid, "127.0.0.1:4318"); !errors.Is(err, unix.EADDRINUSE) {
		t.Errorf("listen on a taken port: %v, want EADDRINUSE", err)
	}
	if _, err := netns.Listen(ctx, namespace(t, false), "[::1]:4318"); !errors.Is(err, unix.EADDRNOTAVAIL) {
		t.Errorf("listen on ::1 without IPv6: %v, want EADDRNOTAVAIL", err)
	}
	if _, err := netns.Listen(ctx, 1<<30, "127.0.0.1:4318"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("listen in a missing process: %v, want ErrNotExist", err)
	}

	// Every thread the listens ran on is back in shed's namespace.
	var wg sync.WaitGroup
	for range 4 * runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			var st unix.Stat_t
			if err := unix.Stat("/proc/thread-self/ns/net", &st); err != nil {
				t.Error(err)
			} else if st.Ino != own {
				t.Errorf("thread %d is in namespace %d, want %d", unix.Gettid(), st.Ino, own)
			}
		})
	}
	wg.Wait()
}
