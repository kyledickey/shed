// Package netns opens TCP listeners inside the network namespaces of other
// processes, such as a container's init process. It needs CAP_SYS_ADMIN
// and only works on Linux.
package netns

import (
	"context"
	"fmt"
	"net"
	"runtime"

	"golang.org/x/sys/unix"
)

func path(pid int) string { return fmt.Sprintf("/proc/%d/ns/net", pid) }

// ID identifies the network namespace of the process pid: the inode of
// /proc/<pid>/ns/net. Two processes share a namespace when their IDs are
// equal.
func ID(pid int) (uint64, error) {
	var st unix.Stat_t
	if err := unix.Stat(path(pid), &st); err != nil {
		return 0, fmt.Errorf("netns: stat %s: %w", path(pid), err)
	}
	return st.Ino, nil
}

// Listen listens on the TCP address addr inside the network namespace of
// the process pid. The listener stays in that namespace after Listen
// returns, and accepted connections come from it. An address the namespace
// does not have, such as [::1] with IPv6 disabled, fails with an error
// wrapping unix.EADDRNOTAVAIL, and a port already taken there with one
// wrapping unix.EADDRINUSE.
func Listen(ctx context.Context, pid int, addr string) (net.Listener, error) {
	target, err := unix.Open(path(pid), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("netns: open %s: %w", path(pid), err)
	}
	defer unix.Close(target)

	type result struct {
		ln  net.Listener
		err error
	}
	done := make(chan result, 1)
	go func() {
		// The thread switches namespaces, so no other goroutine may run on
		// it until it is back in its own; if it cannot get back, it stays
		// locked and the runtime discards it when this goroutine exits.
		runtime.LockOSThread()
		ln, restored, err := listenIn(ctx, target, addr)
		if restored {
			runtime.UnlockOSThread()
		}
		done <- result{ln, err}
	}()
	r := <-done
	if r.err != nil {
		return nil, fmt.Errorf("netns: listen on %s in %s: %w", addr, path(pid), r.err)
	}
	return r.ln, nil
}

// listenIn listens on addr from inside the namespace target, on the current
// thread, which must be locked, and reports whether the thread is back in
// its own namespace.
func listenIn(ctx context.Context, target int, addr string) (net.Listener, bool, error) {
	own, err := unix.Open("/proc/thread-self/ns/net", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, true, err
	}
	defer unix.Close(own)
	if err := unix.Setns(target, unix.CLONE_NEWNET); err != nil {
		return nil, true, fmt.Errorf("setns: %w", err)
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if rerr := unix.Setns(own, unix.CLONE_NEWNET); rerr != nil {
		if ln != nil {
			ln.Close()
		}
		return nil, false, fmt.Errorf("restore namespace: %w", rerr)
	}
	return ln, true, err
}
