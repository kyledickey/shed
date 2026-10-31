package host

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/ti-mo/conntrack"
)

func TestFlowFrom(t *testing.T) {
	in := conntrack.Flow{
		ID:           7,
		TupleOrig:    conntrack.Tuple{IP: conntrack.IPTuple{SourceAddress: netip.MustParseAddr("172.18.0.2"), DestinationAddress: netip.MustParseAddr("1.1.1.1")}, Proto: conntrack.ProtoTuple{Protocol: 6, SourcePort: 40000, DestinationPort: 443}},
		TupleReply:   conntrack.Tuple{IP: conntrack.IPTuple{SourceAddress: netip.MustParseAddr("1.1.1.1"), DestinationAddress: netip.MustParseAddr("203.0.113.9")}, Proto: conntrack.ProtoTuple{Protocol: 6, SourcePort: 443, DestinationPort: 40000}},
		CountersOrig: conntrack.Counter{Packets: 3, Bytes: 300}, CountersReply: conntrack.Counter{Direction: true, Packets: 4, Bytes: 4000},
	}
	want := Flow{
		ID: 7, Proto: 6,
		Orig:          Tuple{Src: Endpoint{netip.MustParseAddr("172.18.0.2"), 40000}, Dst: Endpoint{netip.MustParseAddr("1.1.1.1"), 443}},
		Reply:         Tuple{Src: Endpoint{netip.MustParseAddr("1.1.1.1"), 443}, Dst: Endpoint{netip.MustParseAddr("203.0.113.9"), 40000}},
		OrigCounters:  Counters{3, 300},
		ReplyCounters: Counters{4, 4000},
	}
	if got := flowFrom(in); got != want {
		t.Errorf("flowFrom = %+v, want %+v", got, want)
	}
}

func TestConntrackError(t *testing.T) {
	tests := []struct {
		err  error
		want error
	}{
		{syscall.EPERM, ErrNotPermitted},
		{syscall.EACCES, ErrNotPermitted},
		{syscall.ENOENT, ErrConntrackUnavailable},
		{syscall.EPROTONOSUPPORT, ErrConntrackUnavailable},
		{os.ErrNotExist, ErrConntrackUnavailable},
	}
	for _, tc := range tests {
		got := conntrackError(&os.PathError{Op: "x", Err: tc.err})
		if !errors.Is(got, tc.want) {
			t.Errorf("conntrackError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
	other := conntrackError(syscall.EIO)
	if errors.Is(other, ErrNotPermitted) || errors.Is(other, ErrConntrackUnavailable) {
		t.Errorf("conntrackError(EIO) = %v, want neither sentinel", other)
	}
}

func TestEnableConntrackAccounting(t *testing.T) {
	proc := t.TempDir()
	file := filepath.Join(proc, "sys/net/netfilter/nf_conntrack_acct")

	if err := EnableConntrackAccounting(proc); !errors.Is(err, ErrConntrackUnavailable) {
		t.Fatalf("without the file: err = %v, want ErrConntrackUnavailable", err)
	}

	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnableConntrackAccounting(proc); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(file); string(got) != "1\n" {
		t.Errorf("file = %q, want 1", got)
	}
	if err := EnableConntrackAccounting(proc); err != nil {
		t.Errorf("second call: %v", err)
	}
}
