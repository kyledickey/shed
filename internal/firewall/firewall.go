// Package firewall keeps the host firewall rules shed needs: one iptables
// INPUT rule per Docker bridge accepting TCP to a single port, so containers
// can reach shed's telemetry gateway on hosts whose firewall (ufw, for
// example) drops input from bridges. Every rule carries the comment
// "shed-telemetry", and [Firewall.Sync] touches only those.
package firewall

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

// comment marks the rules shed owns.
const comment = "shed-telemetry"

// ErrUnavailable reports that iptables cannot be run on this host.
var ErrUnavailable = errors.New("firewall: iptables is not available")

// Rule accepts TCP to Port arriving on the network interface Iface.
type Rule struct {
	Iface string
	Port  int
}

func (r Rule) args() []string {
	return []string{"-i", r.Iface, "-p", "tcp", "--dport", strconv.Itoa(r.Port),
		"-m", "comment", "--comment", comment, "-j", "ACCEPT"}
}

// Firewall changes the host's iptables INPUT chain.
type Firewall struct {
	// run runs iptables with args and returns its standard output.
	run func(ctx context.Context, args ...string) ([]byte, error)
}

// New returns a Firewall that runs the iptables on PATH.
func New() *Firewall {
	return &Firewall{run: func(ctx context.Context, args ...string) ([]byte, error) {
		path, err := exec.LookPath("iptables")
		if err != nil {
			return nil, ErrUnavailable
		}
		var stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, path, append([]string{"-w"}, args...)...)
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("iptables %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
		}
		return out, nil
	}}
}

// Sync makes shed's rules in the INPUT chain exactly rules: it inserts the
// missing ones at the top of the chain, ahead of any rule that would drop
// the traffic, and deletes the ones no longer wanted. Sync(ctx, nil)
// removes them all.
func (f *Firewall) Sync(ctx context.Context, rules []Rule) error {
	out, err := f.run(ctx, "-S", "INPUT")
	if err != nil {
		return fmt.Errorf("firewall: list rules: %w", err)
	}
	have := owned(out)
	var errs []error
	for _, r := range have {
		if !slices.Contains(rules, r) {
			if _, err := f.run(ctx, append([]string{"-D", "INPUT"}, r.args()...)...); err != nil {
				errs = append(errs, fmt.Errorf("firewall: delete rule: %w", err))
			}
		}
	}
	for _, r := range rules {
		if !slices.Contains(have, r) {
			if _, err := f.run(ctx, append([]string{"-I", "INPUT"}, r.args()...)...); err != nil {
				errs = append(errs, fmt.Errorf("firewall: insert rule: %w", err))
			}
		}
	}
	return errors.Join(errs...)
}

// owned returns the rules shed owns in `iptables -S INPUT` output.
func owned(out []byte) []Rule {
	var rules []Rule
	for line := range strings.Lines(string(out)) {
		fields := strings.Fields(line)
		if !slices.Contains(fields, comment) {
			continue
		}
		var r Rule
		for i := 0; i+1 < len(fields); i++ {
			switch fields[i] {
			case "-i":
				r.Iface = fields[i+1]
			case "--dport":
				r.Port, _ = strconv.Atoi(fields[i+1])
			}
		}
		if r.Iface != "" && r.Port > 0 && !slices.Contains(rules, r) {
			rules = append(rules, r)
		}
	}
	return rules
}
