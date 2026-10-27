package firewall

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// fakeIPTables keeps an INPUT chain in memory, in `iptables -S` form.
type fakeIPTables struct {
	chain []string
	calls []string
	fail  string // fail calls starting with this
}

func (f *fakeIPTables) run(_ context.Context, args ...string) ([]byte, error) {
	call := strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if f.fail != "" && strings.HasPrefix(call, f.fail) {
		return nil, errors.New("permission denied")
	}
	switch args[0] {
	case "-S":
		return []byte("-P INPUT DROP\n" + strings.Join(f.chain, "")), nil
	case "-I":
		f.chain = append([]string{"-A INPUT " + strings.Join(args[2:], " ") + "\n"}, f.chain...)
	case "-D":
		want := "-A INPUT " + strings.Join(args[2:], " ") + "\n"
		f.chain = slices.DeleteFunc(f.chain, func(l string) bool { return l == want })
	}
	return nil, nil
}

func rule(iface string) string {
	return "-A INPUT -i " + iface + " -p tcp --dport 4318 -m comment --comment shed-telemetry -j ACCEPT\n"
}

func TestSync(t *testing.T) {
	ufw := "-A INPUT -j ufw-before-input\n"
	tests := []struct {
		name  string
		chain []string
		want  []Rule
		after []string
	}{
		{
			name:  "inserts missing rules ahead of the rest",
			chain: []string{ufw},
			want:  []Rule{{"br-aaa", 4318}, {"br-bbb", 4318}},
			after: []string{rule("br-bbb"), rule("br-aaa"), ufw},
		},
		{
			name:  "keeps wanted rules and deletes stale ones",
			chain: []string{rule("br-aaa"), rule("br-old"), ufw},
			want:  []Rule{{"br-aaa", 4318}},
			after: []string{rule("br-aaa"), ufw},
		},
		{
			name:  "nil removes every owned rule and nothing else",
			chain: []string{rule("br-aaa"), "-A INPUT -i br-aaa -p tcp --dport 4318 -j ACCEPT\n", ufw},
			after: []string{"-A INPUT -i br-aaa -p tcp --dport 4318 -j ACCEPT\n", ufw},
		},
		{
			name:  "reads rules as iptables prints them",
			chain: []string{"-A INPUT -i br-aaa -p tcp -m tcp --dport 4318 -m comment --comment shed-telemetry -j ACCEPT\n"},
			want:  []Rule{{"br-aaa", 4318}},
			after: []string{"-A INPUT -i br-aaa -p tcp -m tcp --dport 4318 -m comment --comment shed-telemetry -j ACCEPT\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ipt := &fakeIPTables{chain: tt.chain}
			f := &Firewall{run: ipt.run}
			if err := f.Sync(context.Background(), tt.want); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(ipt.chain, tt.after) {
				t.Errorf("chain = %q, want %q", ipt.chain, tt.after)
			}
			calls := len(ipt.calls)
			if err := f.Sync(context.Background(), tt.want); err != nil {
				t.Fatal(err)
			}
			if len(ipt.calls) != calls+1 {
				t.Errorf("second Sync ran %q, want only a listing", ipt.calls[calls:])
			}
		})
	}
}

func TestSyncReportsFailures(t *testing.T) {
	ipt := &fakeIPTables{fail: "-I"}
	f := &Firewall{run: ipt.run}
	if err := f.Sync(context.Background(), []Rule{{"br-aaa", 4318}}); err == nil {
		t.Fatal("Sync succeeded although inserting failed")
	}
	ipt = &fakeIPTables{fail: "-S"}
	f = &Firewall{run: ipt.run}
	if err := f.Sync(context.Background(), nil); err == nil {
		t.Fatal("Sync succeeded although listing failed")
	}
}
