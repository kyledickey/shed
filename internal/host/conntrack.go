package host

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ti-mo/conntrack"
)

// Errors of the conntrack functions, which callers branch on.
var (
	// ErrConntrackUnavailable reports that the kernel does not track
	// connections: the nf_conntrack module is not loaded, or the kernel was
	// built without it.
	ErrConntrackUnavailable = errors.New("host: connection tracking is not available")
	// ErrNotPermitted reports that the process may not read or configure
	// connection tracking, which takes CAP_NET_ADMIN.
	ErrNotPermitted = errors.New("host: not permitted to use connection tracking")
)

// Endpoint is one side of a connection.
type Endpoint struct {
	Addr netip.Addr
	// Port is zero for protocols without ports.
	Port uint16
}

// Tuple is one direction of a connection.
type Tuple struct {
	Src, Dst Endpoint
}

// Counters are the packets and bytes seen in one direction of a connection.
// They are zero unless accounting was on when the connection began.
type Counters struct {
	Packets, Bytes uint64
}

// Flow is an entry of the kernel's connection tracking table.
type Flow struct {
	// ID identifies the entry while it lives.
	ID uint32
	// Proto is the IP protocol number, such as 6 for TCP and 17 for UDP.
	Proto uint8
	// Orig is the connection as its initiator sent it; Reply is how it
	// answers, after any address translation.
	Orig, Reply Tuple
	// OrigCounters count the initiator's packets, ReplyCounters the answers'.
	OrigCounters, ReplyCounters Counters
}

// ConntrackFlows returns the entries of the kernel's connection tracking
// table, via netlink. It returns [ErrConntrackUnavailable] or
// [ErrNotPermitted] when it cannot.
func ConntrackFlows(ctx context.Context) ([]Flow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := conntrack.Dial(nil)
	if err != nil {
		return nil, conntrackError(err)
	}
	defer c.Close()
	flows, err := c.Dump(nil)
	if err != nil {
		return nil, conntrackError(err)
	}
	out := make([]Flow, len(flows))
	for i, f := range flows {
		out[i] = flowFrom(f)
	}
	return out, nil
}

// flowFrom converts f.
func flowFrom(f conntrack.Flow) Flow {
	tuple := func(t conntrack.Tuple) Tuple {
		return Tuple{
			Src: Endpoint{t.IP.SourceAddress, t.Proto.SourcePort},
			Dst: Endpoint{t.IP.DestinationAddress, t.Proto.DestinationPort},
		}
	}
	return Flow{
		ID:            f.ID,
		Proto:         f.TupleOrig.Proto.Protocol,
		Orig:          tuple(f.TupleOrig),
		Reply:         tuple(f.TupleReply),
		OrigCounters:  Counters{f.CountersOrig.Packets, f.CountersOrig.Bytes},
		ReplyCounters: Counters{f.CountersReply.Packets, f.CountersReply.Bytes},
	}
}

// conntrackError maps the errors of the netlink socket and of the sysctl
// files to the package's sentinels.
func conntrackError(err error) error {
	switch {
	case errors.Is(err, syscall.EPERM), errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EROFS):
		return fmt.Errorf("%w: %w", ErrNotPermitted, err)
	case errors.Is(err, os.ErrNotExist), errors.Is(err, syscall.ENOENT),
		errors.Is(err, syscall.EPROTONOSUPPORT), errors.Is(err, syscall.EOPNOTSUPP),
		errors.Is(err, syscall.ENOSYS), errors.Is(err, syscall.EAFNOSUPPORT):
		return fmt.Errorf("%w: %w", ErrConntrackUnavailable, err)
	}
	return fmt.Errorf("host: conntrack: %w", err)
}

// EnableConntrackAccounting sets net.netfilter.nf_conntrack_acct to 1 under
// the procfs mounted at proc (empty means /proc), so that new connections
// count their packets and bytes. Connections that began before keep zero
// counters. It returns [ErrConntrackUnavailable] or [ErrNotPermitted] when
// it cannot.
func EnableConntrackAccounting(proc string) error {
	path := filepath.Join(or(proc, "/proc"), "sys/net/netfilter/nf_conntrack_acct")
	cur, err := os.ReadFile(path)
	if err != nil {
		return conntrackError(err)
	}
	if strings.TrimSpace(string(cur)) == "1" {
		return nil
	}
	if err := os.WriteFile(path, []byte("1\n"), 0); err != nil {
		return conntrackError(err)
	}
	return nil
}
